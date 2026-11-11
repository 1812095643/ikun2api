package service

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type proxyGuardStoreStub struct {
	mu         sync.Mutex
	snapshots  map[int64]*AccountProxySnapshot
	persistErr error
	clearCount int
	resetErr   error
	resetCount int
}

func (s *proxyGuardStoreStub) GetAccountProxySnapshot(_ context.Context, id int64) (*AccountProxySnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.snapshots[id]
	if a == nil {
		return nil, ErrAccountNotFound
	}
	copy := *a
	if a.State != nil {
		state := *a.State
		copy.State = &state
	}
	return &copy, nil
}
func (s *proxyGuardStoreStub) QuarantineAccountProxy(_ context.Context, a *AccountProxySnapshot, state ProxyFailureState) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.persistErr != nil {
		return false, s.persistErr
	}
	current := s.snapshots[a.AccountID]
	if current.ResetEpoch != a.ResetEpoch {
		return false, nil
	}
	if current.State != nil && current.State.Generation != state.Generation {
		return false, nil
	}
	current.State = &state
	return true, nil
}
func (s *proxyGuardStoreStub) ClearAccountProxyQuarantine(_ context.Context, a *AccountProxySnapshot) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.snapshots[a.AccountID]
	if current.State == nil || current.State.Generation != a.State.Generation {
		return false, nil
	}
	current.State = nil
	s.clearCount++
	return true, nil
}
func (s *proxyGuardStoreStub) ListProxyQuarantinedAccounts(_ context.Context, after int64, limit int) ([]int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []int64
	for id, a := range s.snapshots {
		if id > after && a.State != nil {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (s *proxyGuardStoreStub) ResetAccountProxyQuarantine(_ context.Context, a *AccountProxySnapshot) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.resetErr != nil {
		return false, s.resetErr
	}
	current := s.snapshots[a.AccountID]
	if (current.State == nil) != (a.State == nil) ||
		(current.State != nil && current.State.Generation != a.State.Generation) {
		return false, nil
	}
	if current.ResetEpoch != a.ResetEpoch {
		return false, nil
	}
	current.State = nil
	current.ResetEpoch = "epoch-reset-" + time.Now().UTC().Format(time.RFC3339Nano)
	s.resetCount++
	return true, nil
}

func TestProxyFailureGuardManualResetClearsDurableAndLocalBlockBeforeCooldown(t *testing.T) {
	g, store, probe, _ := proxyGuardFixture()
	lease, err := g.Begin(context.Background(), 7, "socks5://127.0.0.1:1080", "https://api.anthropic.com/")
	require.NoError(t, err)
	defer lease.Close()
	require.ErrorIs(t, lease.Failure(&net.OpError{Op: "dial", Err: errors.New("down")}), ErrProxyQuarantined)
	require.NotNil(t, store.snapshots[7].State)
	require.NoError(t, g.ResetAccount(context.Background(), 7))
	require.Nil(t, store.snapshots[7].State)
	require.Empty(t, g.blocks)
	require.Zero(t, probe.calls, "manual reset must not wait for the automatic probe")
	g.CheckRecovery()
	require.Nil(t, store.snapshots[7].State, "recovery must not resurrect the reset marker")
	next, err := g.Begin(context.Background(), 7, "socks5://127.0.0.1:1080", "https://api.anthropic.com/")
	require.NoError(t, err)
	next.Close()
	_, err = g.Begin(context.Background(), 7, "", "https://api.anthropic.com/")
	require.ErrorIs(t, err, ErrProxyQuarantined, "manual recovery must never enable direct fallback")
}

func TestProxyFailureGuardManualResetKeepsBlockOnPersistenceFailure(t *testing.T) {
	g, store, _, _ := proxyGuardFixture()
	lease, err := g.Begin(context.Background(), 7, "socks5://127.0.0.1:1080", "https://api.anthropic.com/")
	require.NoError(t, err)
	defer lease.Close()
	lease.Failure(&net.OpError{Op: "dial", Err: errors.New("down")})
	store.resetErr = errors.New("storage unavailable")
	require.Error(t, g.ResetAccount(context.Background(), 7))
	require.NotNil(t, store.snapshots[7].State)
	require.Contains(t, g.blocks, int64(7))
}

func TestProxyFailureGuardManualResetHandlesUnpersistedFailure(t *testing.T) {
	g, store, _, _ := proxyGuardFixture()
	store.persistErr = errors.New("write unavailable")
	lease, err := g.Begin(context.Background(), 7, "socks5://127.0.0.1:1080", "https://api.anthropic.com/")
	require.NoError(t, err)
	defer lease.Close()
	lease.Failure(&net.OpError{Op: "dial", Err: errors.New("down")})
	require.Nil(t, store.snapshots[7].State)
	require.Contains(t, g.blocks, int64(7))
	require.NoError(t, g.ResetAccount(context.Background(), 7))
	require.Empty(t, g.blocks)
}

func TestProxyFailureGuardManualResetDoesNotRequarantineFromAnOlderLease(t *testing.T) {
	g, store, _, now := proxyGuardFixture()
	lease, err := g.Begin(context.Background(), 7, "socks5://127.0.0.1:1080", "https://api.anthropic.com/")
	require.NoError(t, err)
	defer lease.Close()
	store.snapshots[7].State = &ProxyFailureState{Generation: "manual-reset", NotBefore: now.Add(time.Minute)}
	require.NoError(t, g.ResetAccount(context.Background(), 7))
	require.ErrorIs(t, lease.Failure(&net.OpError{Op: "dial", Err: errors.New("late transport failure")}), ErrProxyQuarantined)
	require.Nil(t, store.snapshots[7].State, "pre-reset requests cannot re-create the reset quarantine")
	require.Empty(t, g.blocks)
}

func TestProxyFailureGuardObservesResetFromAnotherProcess(t *testing.T) {
	g, store, _, _ := proxyGuardFixture()
	lease, err := g.Begin(context.Background(), 7, "socks5://127.0.0.1:1080", "https://api.anthropic.com/")
	require.NoError(t, err)
	defer lease.Close()
	otherProcessSnapshot, err := store.GetAccountProxySnapshot(context.Background(), 7)
	require.NoError(t, err)
	resetOK, err := store.ResetAccountProxyQuarantine(context.Background(), otherProcessSnapshot)
	require.NoError(t, err)
	require.True(t, resetOK)
	lease.Failure(&net.OpError{Op: "dial", Err: errors.New("late failure after reset")})
	require.Nil(t, store.snapshots[7].State, "stale lease must not write after another process reset")
	g.CheckRecovery()
	require.Empty(t, g.blocks)
}

func TestProxyFailureGuardResetEpochChangesEvenBeforeFailureIsPersisted(t *testing.T) {
	g, store, _, _ := proxyGuardFixture()
	lease, err := g.Begin(context.Background(), 7, "socks5://127.0.0.1:1080", "https://api.anthropic.com/")
	require.NoError(t, err)
	defer lease.Close()
	otherProcessSnapshot, err := store.GetAccountProxySnapshot(context.Background(), 7)
	require.NoError(t, err)
	resetOK, err := store.ResetAccountProxyQuarantine(context.Background(), otherProcessSnapshot)
	require.NoError(t, err)
	require.True(t, resetOK, "reset must advance the database epoch even with no persisted quarantine marker")
	lease.Failure(&net.OpError{Op: "dial", Err: errors.New("older request failure")})
	require.Nil(t, store.snapshots[7].State, "old request cannot persist a quarantine across a reset on another process")
	g.CheckRecovery()
	require.Empty(t, g.blocks)
}

type proxyGuardProberStub struct {
	calls  int
	err    error
	during func()
}

func (p *proxyGuardProberStub) ProbeAccountProxy(ctx context.Context, proxy, origin string) error {
	p.calls++
	if p.during != nil {
		p.during()
	}
	return p.err
}
func proxyGuardFixture() (*ProxyFailureGuard, *proxyGuardStoreStub, *proxyGuardProberStub, *time.Time) {
	id := int64(9)
	now := time.Unix(10000, 0)
	store := &proxyGuardStoreStub{snapshots: map[int64]*AccountProxySnapshot{7: {AccountID: 7, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Active: true, ProxyID: &id,
		Proxy: &Proxy{ID: id, Protocol: "socks5", Host: "127.0.0.1", Port: 1080, Status: StatusActive}}}}
	prober := &proxyGuardProberStub{}
	guard := NewProxyFailureGuard(store, prober)
	guard.now = func() time.Time { return now }
	return guard, store, prober, &now
}

func TestProxyFailureGuardCancelsPeersAndRequiresRecoveryAfterThreeMinutes(t *testing.T) {
	g, store, probe, now := proxyGuardFixture()
	a, err := g.Begin(context.Background(), 7, "socks5://127.0.0.1:1080", "https://api.anthropic.com/v1/messages")
	require.NoError(t, err)
	defer a.Close()
	b, err := g.Begin(context.Background(), 7, "socks5://127.0.0.1:1080", "https://api.anthropic.com/v1/messages")
	require.NoError(t, err)
	defer b.Close()
	err = a.Failure(&net.OpError{Op: "dial", Err: proxyTimeoutFixture{}})
	require.ErrorIs(t, err, ErrProxyQuarantined)
	require.ErrorIs(t, context.Cause(b.Context()), ErrProxyQuarantined)
	require.Equal(t, now.Add(3*time.Minute), store.snapshots[7].State.NotBefore)
	require.Equal(t, "https://api.anthropic.com/", store.snapshots[7].State.ProbeURL)
	*now = now.Add(2 * time.Minute)
	g.CheckRecovery()
	require.Zero(t, probe.calls)
	*now = now.Add(time.Minute)
	probe.err = errors.New("still offline")
	g.CheckRecovery()
	require.Equal(t, 1, probe.calls)
	_, err = g.Begin(context.Background(), 7, "socks5://127.0.0.1:1080", "https://api.anthropic.com/")
	require.ErrorIs(t, err, ErrProxyQuarantined)
	probe.err = nil
	g.CheckRecovery()
	require.Nil(t, store.snapshots[7].State)
	c, err := g.Begin(context.Background(), 7, "socks5://127.0.0.1:1080", "https://api.anthropic.com/")
	require.NoError(t, err)
	c.Close()
}

type proxyTimeoutFixture struct{}

func (proxyTimeoutFixture) Error() string   { return "i/o timeout" }
func (proxyTimeoutFixture) Timeout() bool   { return true }
func (proxyTimeoutFixture) Temporary() bool { return true }

func TestProxyFailureGuardNeverSendsMissingWrongOrInactiveProxy(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mutate   func(*AccountProxySnapshot)
		supplied string
	}{
		{"empty", nil, ""}, {"wrong", nil, "socks5://127.0.0.1:9999"},
		{"missing_relation", func(a *AccountProxySnapshot) { a.Proxy = nil }, "socks5://127.0.0.1:1080"},
		{"inactive", func(a *AccountProxySnapshot) { a.Proxy.Status = "inactive" }, "socks5://127.0.0.1:1080"},
		{"expired", func(a *AccountProxySnapshot) { expired := time.Unix(1, 0); a.Proxy.ExpiresAt = &expired }, "socks5://127.0.0.1:1080"},
		{"legacy_direct_fallback", func(a *AccountProxySnapshot) { a.OriginID = a.ProxyID; a.ProxyID = nil; a.Proxy = nil }, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, store, _, _ := proxyGuardFixture()
			if tc.mutate != nil {
				tc.mutate(store.snapshots[7])
			}
			lease, err := g.Begin(context.Background(), 7, tc.supplied, "https://api.anthropic.com/")
			require.Nil(t, lease)
			require.ErrorIs(t, err, ErrProxyQuarantined)
			require.NotNil(t, store.snapshots[7].State)
		})
	}
}

func TestProxyFailureGuardPersistedBlockSurvivesRestartAndStaleProbe(t *testing.T) {
	g, store, _, now := proxyGuardFixture()
	lease, err := g.Begin(context.Background(), 7, "socks5://127.0.0.1:1080", "https://api.anthropic.com/")
	require.NoError(t, err)
	lease.Failure(proxyTimeoutFixture{})
	lease.Close()
	*now = now.Add(4 * time.Minute)
	probe := &proxyGuardProberStub{err: errors.New("offline")}
	restarted := NewProxyFailureGuard(store, probe)
	restarted.now = func() time.Time { return *now }
	_, err = restarted.Begin(context.Background(), 7, "socks5://127.0.0.1:1080", "https://api.anthropic.com/")
	require.ErrorIs(t, err, ErrProxyQuarantined)
	restarted.CheckRecovery()
	require.NotNil(t, store.snapshots[7].State)
	probe.err = nil
	probe.during = func() { store.snapshots[7].State.Generation = "newer-failure" }
	restarted.CheckRecovery()
	require.NotNil(t, store.snapshots[7].State)
	require.Zero(t, store.clearCount)
}

func TestProxyFailureGuardCancellationAndPersistentWriteFailure(t *testing.T) {
	g, store, _, now := proxyGuardFixture()
	ctx, cancel := context.WithCancel(context.Background())
	a, err := g.Begin(ctx, 7, "socks5://127.0.0.1:1080", "https://api.anthropic.com/")
	require.NoError(t, err)
	cancel()
	a.Failure(context.Canceled)
	a.Close()
	require.Nil(t, store.snapshots[7].State)
	store.persistErr = errors.New("database unavailable")
	b, err := g.Begin(context.Background(), 7, "socks5://127.0.0.1:1080", "https://api.anthropic.com/")
	require.NoError(t, err)
	b.Failure(proxyTimeoutFixture{})
	b.Close()
	_, err = g.Begin(context.Background(), 7, "socks5://127.0.0.1:1080", "https://api.anthropic.com/")
	require.ErrorIs(t, err, ErrProxyQuarantined)
	store.persistErr = nil
	*now = now.Add(4 * time.Minute)
	g.CheckRecovery()
	require.Equal(t, 1, store.clearCount)
}

func TestProxyFailureGuardStateIsNotAnExpiringSchedulerHint(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	a := &Account{Status: StatusActive, Schedulable: true, TempUnschedulableUntil: &past, Extra: map[string]any{ProxyFailureGuardExtraKey: map[string]any{"generation": "x"}}}
	require.False(t, a.IsSchedulable())
	require.False(t, a.IsCredentialUsableForShadow())
	current := map[string]any{ProxyFailureGuardExtraKey: "runtime-only", ProxyFailureResetEpochExtraKey: "epoch-1"}
	merged := MergeProxyFailureGuardExtra(map[string]any{ProxyFailureGuardExtraKey: "forged", "name": "keep"}, current)
	require.Equal(t, "runtime-only", merged[ProxyFailureGuardExtraKey])
	require.Equal(t, "epoch-1", merged[ProxyFailureResetEpochExtraKey])
	require.Equal(t, "keep", merged["name"])
	require.NotContains(t, MergeProxyFailureGuardExtra(map[string]any{ProxyFailureGuardExtraKey: "forged"}, nil), ProxyFailureGuardExtraKey)
	require.NotContains(t, MergeProxyFailureGuardExtra(map[string]any{ProxyFailureResetEpochExtraKey: "forged"}, nil), ProxyFailureResetEpochExtraKey)
}
