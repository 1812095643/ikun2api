package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/proxyurl"
	"github.com/google/uuid"
)

const ProxyFailureGuardExtraKey = "proxy_failure_guard"
const ProxyFailureResetEpochExtraKey = "proxy_failure_guard_reset_epoch"
const ProxyFailureCooldown = 3 * time.Minute
const ProxyFailureReason = "proxy_failure_guard: 代理连接异常，冷却后自动探测或由管理员手动恢复"

var ErrProxyQuarantined = errors.New("account proxy is quarantined; direct fallback is forbidden")

// State contains no proxy credentials, user input or provider authorization.
// Presence is a durable scheduling gate: NotBefore expiring never clears it.
type ProxyFailureState struct {
	Generation string    `json:"generation"`
	NotBefore  time.Time `json:"not_before"`
	RetryAfter time.Time `json:"retry_after"`
	ProbeURL   string    `json:"probe_url"`
}

type AccountProxySnapshot struct {
	AccountID  int64
	Platform   string
	Type       string
	Active     bool
	ProxyID    *int64
	OriginID   *int64
	Proxy      *Proxy
	State      *ProxyFailureState
	ResetEpoch string
}

func (s AccountProxySnapshot) RequiresProxy() bool {
	return s.ProxyID != nil || s.OriginID != nil ||
		(s.Platform == PlatformAnthropic && (s.Type == AccountTypeOAuth || s.Type == AccountTypeSetupToken))
}

// This narrow store keeps tests and read-only repositories independent of the
// account management API. Writes must compare the current route and generation.
type ProxyFailureGuardRepository interface {
	GetAccountProxySnapshot(context.Context, int64) (*AccountProxySnapshot, error)
	QuarantineAccountProxy(context.Context, *AccountProxySnapshot, ProxyFailureState) (bool, error)
	ClearAccountProxyQuarantine(context.Context, *AccountProxySnapshot) (bool, error)
	ListProxyQuarantinedAccounts(context.Context, int64, int) ([]int64, error)
}

type ProxyRecoveryProber interface {
	ProbeAccountProxy(context.Context, string, string) error
}

type proxyQuarantineResetter interface {
	ResetAccountProxyQuarantine(context.Context, *AccountProxySnapshot) (bool, error)
}

func (a *Account) IsProxyQuarantined() bool {
	if a == nil {
		return false
	}
	value, exists := a.Extra[ProxyFailureGuardExtraKey]
	return exists && value != nil
}

// Management edits cannot forge, drop or resurrect the runtime quarantine.
func MergeProxyFailureGuardExtra(next, current map[string]any) map[string]any {
	if next == nil {
		next = map[string]any{}
	}
	delete(next, ProxyFailureGuardExtraKey)
	if value, ok := current[ProxyFailureGuardExtraKey]; ok && value != nil {
		next[ProxyFailureGuardExtraKey] = value
	}
	delete(next, ProxyFailureResetEpochExtraKey)
	if value, ok := current[ProxyFailureResetEpochExtraKey]; ok && value != nil {
		next[ProxyFailureResetEpochExtraKey] = value
	}
	return next
}

func DecodeProxyFailureState(raw []byte) (*ProxyFailureState, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var state ProxyFailureState
	if err := json.Unmarshal(raw, &state); err != nil || state.Generation == "" || state.NotBefore.IsZero() {
		return nil, fmt.Errorf("invalid persisted proxy quarantine state")
	}
	return &state, nil
}

type proxyFailureLocal struct {
	snapshot  *AccountProxySnapshot
	state     ProxyFailureState
	persisted bool
}

type ProxyFailureGuard struct {
	repo              ProxyFailureGuardRepository
	prober            ProxyRecoveryProber
	mu                sync.Mutex
	active            map[int64]map[*ProxyRequestLease]context.CancelCauseFunc
	blocks            map[int64]proxyFailureLocal
	epochs            map[int64]uint64
	now               func() time.Time
	stop              chan struct{}
	once              sync.Once
	wg                sync.WaitGroup
	recoverMu         sync.Mutex
	cursor            int64
	accountOperations [64]sync.Mutex
}

func NewProxyFailureGuard(repo ProxyFailureGuardRepository, prober ProxyRecoveryProber) *ProxyFailureGuard {
	return &ProxyFailureGuard{repo: repo, prober: prober, active: make(map[int64]map[*ProxyRequestLease]context.CancelCauseFunc),
		blocks: make(map[int64]proxyFailureLocal), epochs: make(map[int64]uint64), now: time.Now, stop: make(chan struct{})}
}

type ProxyRequestLease struct {
	guard    *ProxyFailureGuard
	snapshot *AccountProxySnapshot
	ctx      context.Context
	cancel   context.CancelCauseFunc
	origin   string
	once     sync.Once
	epoch    uint64
}

func (l *ProxyRequestLease) Context() context.Context { return l.ctx }

func (g *ProxyFailureGuard) Begin(ctx context.Context, accountID int64, suppliedProxy, probeURL string) (*ProxyRequestLease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if g == nil || g.repo == nil || accountID <= 0 {
		return nil, fmt.Errorf("proxy safety gate unavailable")
	}
	snapshot, err := g.repo.GetAccountProxySnapshot(ctx, accountID)
	if err != nil || snapshot == nil {
		return nil, fmt.Errorf("cannot verify current account proxy; request stopped")
	}
	if !snapshot.Active {
		return nil, fmt.Errorf("account unavailable")
	}
	if snapshot.State != nil {
		return nil, ErrProxyQuarantined
	}
	g.mu.Lock()
	if local, blocked := g.blocks[accountID]; blocked &&
		(local.persisted || local.snapshot.ResetEpoch != snapshot.ResetEpoch) {
		delete(g.blocks, accountID)
	}
	g.mu.Unlock()
	child, cancel := context.WithCancelCause(ctx)
	lease := &ProxyRequestLease{guard: g, snapshot: snapshot, ctx: child, cancel: cancel, origin: proxyRecoveryOrigin(probeURL)}
	if snapshot.RequiresProxy() {
		expected, routeErr := snapshotProxyURL(snapshot, g.now())
		supplied, parsed, parseErr := proxyurl.Parse(suppliedProxy)
		if routeErr != nil || parseErr != nil || parsed == nil || supplied != expected {
			g.trip(lease)
			cancel(ErrProxyQuarantined)
			return nil, ErrProxyQuarantined
		}
	}
	g.mu.Lock()
	if _, blocked := g.blocks[accountID]; blocked {
		g.mu.Unlock()
		cancel(ErrProxyQuarantined)
		return nil, ErrProxyQuarantined
	}
	if g.active[accountID] == nil {
		g.active[accountID] = make(map[*ProxyRequestLease]context.CancelCauseFunc)
	}
	g.active[accountID][lease] = cancel
	lease.epoch = g.epochs[accountID]
	g.mu.Unlock()
	return lease, nil
}

func snapshotProxyURL(snapshot *AccountProxySnapshot, now time.Time) (string, error) {
	if snapshot == nil || snapshot.ProxyID == nil || snapshot.Proxy == nil || snapshot.Proxy.ID != *snapshot.ProxyID ||
		!snapshot.Proxy.IsActive() || snapshot.Proxy.IsExpired(now) {
		return "", ErrProxyQuarantined
	}
	normalized, parsed, err := proxyurl.Parse(snapshot.Proxy.URL())
	if err != nil || parsed == nil {
		return "", ErrProxyQuarantined
	}
	return normalized, nil
}

func proxyRecoveryOrigin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return ""
	}
	return (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: "/"}).String()
}

func (l *ProxyRequestLease) Failure(err error) error {
	if l == nil || err == nil || !l.snapshot.RequiresProxy() {
		return err
	}
	if errors.Is(context.Cause(l.ctx), ErrProxyQuarantined) {
		return ErrProxyQuarantined
	}
	// Caller cancellation is not evidence that the proxy failed. A transport's
	// own timeout, including waiting for headers, must trip the safety gate.
	if errors.Is(err, context.Canceled) || errors.Is(l.ctx.Err(), context.Canceled) {
		return err
	}
	var networkErr net.Error
	if errors.As(err, &networkErr) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) ||
		strings.Contains(strings.ToLower(err.Error()), "proxy") || classifyUpstreamTransportError(err).Persistent ||
		strings.Contains(strings.ToLower(err.Error()), "connection") || strings.Contains(strings.ToLower(err.Error()), "tls") {
		l.guard.trip(l)
		return fmt.Errorf("%w: proxy transport failed", ErrProxyQuarantined)
	}
	return err
}

func (l *ProxyRequestLease) Close() {
	if l == nil {
		return
	}
	l.once.Do(func() {
		l.cancel(context.Canceled)
		l.guard.mu.Lock()
		delete(l.guard.active[l.snapshot.AccountID], l)
		if len(l.guard.active[l.snapshot.AccountID]) == 0 {
			delete(l.guard.active, l.snapshot.AccountID)
		}
		l.guard.mu.Unlock()
	})
}

func (g *ProxyFailureGuard) trip(l *ProxyRequestLease) {
	operation := &g.accountOperations[uint64(l.snapshot.AccountID)%64]
	operation.Lock()
	defer operation.Unlock()
	g.mu.Lock()
	if l.epoch != g.epochs[l.snapshot.AccountID] {
		g.mu.Unlock()
		return // An administrator reset superseded this in-flight failure.
	}
	if _, exists := g.blocks[l.snapshot.AccountID]; exists {
		g.mu.Unlock()
		return
	}
	state := ProxyFailureState{Generation: uuid.NewString(), NotBefore: g.now().Add(ProxyFailureCooldown),
		RetryAfter: g.now().Add(ProxyFailureCooldown), ProbeURL: l.origin}
	g.blocks[l.snapshot.AccountID] = proxyFailureLocal{snapshot: l.snapshot, state: state}
	var cancelers []context.CancelCauseFunc
	for _, cancel := range g.active[l.snapshot.AccountID] {
		cancelers = append(cancelers, cancel)
	}
	g.mu.Unlock()
	// Cancel in-flight requests before performing any database or probe work.
	for _, cancel := range cancelers {
		cancel(ErrProxyQuarantined)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if ok, err := g.repo.QuarantineAccountProxy(ctx, l.snapshot, state); err != nil {
		slog.Error("proxy_failure_guard.persist_failed", "account_id", l.snapshot.AccountID)
	} else if ok {
		g.mu.Lock()
		g.blocks[l.snapshot.AccountID] = proxyFailureLocal{snapshot: l.snapshot, state: state, persisted: true}
		g.mu.Unlock()
		slog.Warn("proxy_failure_guard.account_quarantined", "account_id", l.snapshot.AccountID, "until", state.NotBefore)
	} else if current, snapshotErr := g.repo.GetAccountProxySnapshot(ctx, l.snapshot.AccountID); snapshotErr == nil && current != nil && current.ResetEpoch != l.snapshot.ResetEpoch {
		g.mu.Lock()
		if local, exists := g.blocks[l.snapshot.AccountID]; exists && local.state.Generation == state.Generation {
			delete(g.blocks, l.snapshot.AccountID)
		}
		g.mu.Unlock()
	}
	// If persistence fails, the local block remains and is retried by recovery.
}

func (g *ProxyFailureGuard) Start() {
	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				g.CheckRecovery()
			case <-g.stop:
				return
			}
		}
	}()
}

func (g *ProxyFailureGuard) Stop() {
	g.once.Do(func() { close(g.stop) })
	g.mu.Lock()
	for _, leases := range g.active {
		for _, cancel := range leases {
			cancel(ErrProxyQuarantined)
		}
	}
	g.mu.Unlock()
	g.wg.Wait()
}

// Recovery is deliberately serial and sends no provider credentials or model
// request. A successful probe is required after the minimum cooldown, followed
// by a generation/route compare-and-swap before removing the durable marker.
func (g *ProxyFailureGuard) CheckRecovery() {
	if g == nil || g.repo == nil || g.prober == nil {
		return
	}
	if !g.recoverMu.TryLock() {
		return
	}
	defer g.recoverMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	ids, err := g.repo.ListProxyQuarantinedAccounts(ctx, g.cursor, 128)
	if err != nil {
		return
	}
	if len(ids) == 0 {
		g.cursor = 0
		ids, err = g.repo.ListProxyQuarantinedAccounts(ctx, 0, 128)
		if err != nil {
			return
		}
	}
	g.mu.Lock()
	for id := range g.blocks {
		ids = append(ids, id)
	}
	g.mu.Unlock()
	seen := make(map[int64]bool)
	for _, id := range ids {
		if seen[id] || ctx.Err() != nil {
			continue
		}
		seen[id] = true
		g.cursor = id
		g.recoverOne(ctx, id)
	}
}

func (g *ProxyFailureGuard) recoverOne(ctx context.Context, id int64) {
	operation := &g.accountOperations[uint64(id)%64]
	operation.Lock()
	defer operation.Unlock()
	snapshot, err := g.repo.GetAccountProxySnapshot(ctx, id)
	if err != nil || snapshot == nil || !snapshot.Active {
		return
	}
	g.mu.Lock()
	local, locallyBlocked := g.blocks[id]
	g.mu.Unlock()
	if snapshot.State == nil && locallyBlocked {
		if local.persisted {
			g.mu.Lock()
			delete(g.blocks, id)
			g.mu.Unlock()
			return
		}
		if local.snapshot.ResetEpoch != snapshot.ResetEpoch {
			g.mu.Lock()
			if current, exists := g.blocks[id]; exists && current.state.Generation == local.state.Generation {
				delete(g.blocks, id)
			}
			g.mu.Unlock()
			return
		}
		if ok, err := g.repo.QuarantineAccountProxy(ctx, snapshot, local.state); err != nil || !ok {
			return
		}
		g.mu.Lock()
		if current, exists := g.blocks[id]; exists && current.state.Generation == local.state.Generation {
			current.persisted = true
			g.blocks[id] = current
		}
		g.mu.Unlock()
		snapshot.State = &local.state
	}
	state := snapshot.State
	if state == nil || g.now().Before(state.NotBefore) || g.now().Before(state.RetryAfter) {
		return
	}
	proxy, err := snapshotProxyURL(snapshot, g.now())
	if err != nil || state.ProbeURL == "" {
		return
	}
	probeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	err = g.prober.ProbeAccountProxy(probeCtx, proxy, state.ProbeURL)
	cancel()
	if err != nil {
		return
	}
	if ok, err := g.repo.ClearAccountProxyQuarantine(ctx, snapshot); err != nil || !ok {
		return
	}
	g.mu.Lock()
	if current, exists := g.blocks[id]; exists && current.state.Generation == state.Generation {
		delete(g.blocks, id)
	}
	g.mu.Unlock()
	slog.Info("proxy_failure_guard.account_recovered", "account_id", id)
}

// ResetAccount is an explicit administrator action. It bypasses the cooldown
// and probe, but never changes the proxy route or authorizes direct fallback.
func (g *ProxyFailureGuard) ResetAccount(ctx context.Context, id int64) error {
	if g == nil || g.repo == nil || id <= 0 {
		return fmt.Errorf("proxy recovery service unavailable")
	}
	operation := &g.accountOperations[uint64(id)%64]
	operation.Lock()
	defer operation.Unlock()
	snapshot, err := g.repo.GetAccountProxySnapshot(ctx, id)
	if err != nil {
		return err
	}
	if snapshot == nil {
		return ErrAccountNotFound
	}
	g.mu.Lock()
	local, blocked := g.blocks[id]
	g.mu.Unlock()
	resetter, ok := g.repo.(proxyQuarantineResetter)
	if !ok {
		return fmt.Errorf("manual proxy recovery unavailable")
	}
	ok, err = resetter.ResetAccountProxyQuarantine(ctx, snapshot)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("account proxy state changed; retry recovery")
	}
	g.mu.Lock()
	g.epochs[id]++
	if current, exists := g.blocks[id]; blocked && exists && current.state.Generation == local.state.Generation {
		delete(g.blocks, id)
	}
	g.mu.Unlock()
	return nil
}
