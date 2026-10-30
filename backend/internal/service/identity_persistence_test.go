package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type memoryIdentityStore struct {
	mu  sync.Mutex
	ids map[int64]string
	err error
}

func (s *memoryIdentityStore) GetDeviceID(_ context.Context, id int64) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ids[id], s.err
}
func (s *memoryIdentityStore) GetOrCreateDeviceID(_ context.Context, id int64, candidate string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return "", s.err
	}
	if s.ids == nil {
		s.ids = make(map[int64]string)
	}
	if s.ids[id] == "" {
		s.ids[id] = candidate
	}
	return s.ids[id], nil
}
func (s *memoryIdentityStore) MigrateLegacyFingerprints(context.Context) error { return s.err }
func newTestIdentityService(cache IdentityCache) *IdentityService {
	return NewIdentityService(cache, &memoryIdentityStore{})
}

type concurrentIdentityCache struct {
	stubIdentityCache
	mu       sync.Mutex
	entries  map[int64]*Fingerprint
	readErr  error
	writeErr error
}

func (c *concurrentIdentityCache) GetFingerprint(_ context.Context, id int64) (*Fingerprint, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if fp := c.entries[id]; fp != nil {
		clone := *fp
		return &clone, c.readErr
	}
	return nil, c.readErr
}
func (c *concurrentIdentityCache) SetFingerprint(_ context.Context, id int64, fp *Fingerprint) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.writeErr != nil {
		return c.writeErr
	}
	if c.entries == nil {
		c.entries = make(map[int64]*Fingerprint)
	}
	clone := *fp
	c.entries[id] = &clone
	return nil
}

func TestClaudeIdentityConcurrentCreationAndRedisLoss(t *testing.T) {
	ctx := context.Background()
	store := &memoryIdentityStore{}
	cache := &concurrentIdentityCache{}
	services := []*IdentityService{NewIdentityService(cache, store), NewIdentityService(cache, store)}
	const n = 64
	results := make(chan *Fingerprint, n)
	failures := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			fp, err := services[i%2].GetOrCreateFingerprint(ctx, 41, http.Header{"User-Agent": {"claude-cli/2.999.999"}})
			if err != nil {
				failures <- err
			} else {
				results <- fp
			}
		}(i)
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	winner, _ := store.GetDeviceID(ctx, 41)
	require.True(t, ValidClaudeDeviceID(winner))
	for fp := range results {
		require.Equal(t, winner, fp.ClientID)
		require.Equal(t, defaultFingerprint().UserAgent, fp.UserAgent)
	}
	// Simulate a full Redis reset and service restart.
	fp, err := NewIdentityService(&concurrentIdentityCache{}, store).GetOrCreateFingerprint(ctx, 41, nil)
	require.NoError(t, err)
	require.Equal(t, winner, fp.ClientID)
	other, err := services[0].GetOrCreateFingerprint(ctx, 42, nil)
	require.NoError(t, err)
	require.NotEqual(t, winner, other.ClientID)
}

func TestClaudeIdentityInheritsLegacyAndUsesDatabaseWinner(t *testing.T) {
	ctx := context.Background()
	oldID := strings.Repeat("a", 64)
	winner := strings.Repeat("b", 64)
	for _, existing := range []string{"", winner} {
		t.Run(existing, func(t *testing.T) {
			store := &memoryIdentityStore{ids: map[int64]string{41: existing}}
			cache := &stubIdentityCache{fingerprint: &Fingerprint{ClientID: oldID, UserAgent: "claude-cli/2.999.999"}}
			fp, err := NewIdentityService(cache, store).GetOrCreateFingerprint(ctx, 41, nil)
			require.NoError(t, err)
			want := existing
			if want == "" {
				want = oldID
			}
			require.Equal(t, want, fp.ClientID)
			persisted, _ := store.GetDeviceID(ctx, 41)
			require.Equal(t, want, persisted)
			require.Equal(t, 1, fp.IdentityVersion)
		})
	}
}

func TestClaudeIdentityDoesNotInventIDOnStorageFailure(t *testing.T) {
	ctx := context.Background()
	sentinel := errors.New("storage unavailable")
	fp, err := NewIdentityService(&stubIdentityCache{}, &memoryIdentityStore{err: sentinel}).GetOrCreateFingerprint(ctx, 41, nil)
	require.ErrorIs(t, err, sentinel)
	require.Nil(t, fp)
	cache := &concurrentIdentityCache{readErr: sentinel}
	store := &memoryIdentityStore{}
	fp, err = NewIdentityService(cache, store).GetOrCreateFingerprint(ctx, 41, nil)
	require.Error(t, err)
	require.Nil(t, fp)
	require.Empty(t, store.ids)
	// Redis outage is harmless once the SQL record exists.
	store.ids = map[int64]string{41: strings.Repeat("c", 64)}
	cache.writeErr = sentinel
	fp, err = NewIdentityService(cache, store).GetOrCreateFingerprint(ctx, 41, nil)
	require.NoError(t, err)
	require.Equal(t, store.ids[41], fp.ClientID)
}
