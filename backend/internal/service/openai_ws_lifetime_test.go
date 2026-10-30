package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/config"
	"github.com/stretchr/testify/require"
)

func newLifetimeTestPool(t *testing.T) (*openAIWSConnPool, openAIWSAcquireRequest) {
	t.Helper()
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
	cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
	pool := newOpenAIWSConnPool(cfg)
	pool.setClientDialerForTest(&openAIWSFakeDialer{})
	t.Cleanup(pool.Close)
	return pool, openAIWSAcquireRequest{
		Account: &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 1},
		WSURL:   "wss://example.com/responses", Headers: http.Header{}, ExecutionScope: "thread-a",
	}
}

func TestOpenAIWSLifetimeAcquisitionBypassesCleanupThrottle(t *testing.T) {
	for _, pinned := range []bool{false, true} {
		for _, strict := range []bool{false, true} {
			t.Run(testBoolName(pinned)+"/"+testBoolName(strict), func(t *testing.T) {
				pool, request := newLifetimeTestPool(t)
				first, err := pool.Acquire(context.Background(), request)
				require.NoError(t, err)
				old := first.conn
				first.Release()
				if pinned {
					require.True(t, pool.PinConn(request.Account.ID, old.id))
				}
				old.createdAtNano.Store(time.Now().Add(-55 * time.Minute).UnixNano())
				ap := pool.getOrCreateAccountPool(request.Account.ID)
				ap.mu.Lock()
				ap.lastCleanupAt = time.Now()
				ap.mu.Unlock()
				request.PreferredConnID, request.ForcePreferredConn = old.id, strict
				second, err := pool.Acquire(context.Background(), request)
				if strict {
					require.ErrorIs(t, err, errOpenAIWSPreferredConnUnavailable)
					require.Nil(t, second)
					request.PreferredConnID, request.ForcePreferredConn = "", false
					second, err = pool.Acquire(context.Background(), request)
				}
				require.NoError(t, err)
				defer second.Release()
				require.NotEqual(t, old.id, second.ConnID())
				require.False(t, second.Reused())
				require.True(t, old.isClosed())
				ap.mu.Lock()
				defer ap.mu.Unlock()
				require.NotContains(t, ap.conns, old.id)
				require.NotContains(t, ap.pinnedConns, old.id)
			})
		}
	}
}

func testBoolName(value bool) string {
	if value {
		return "enabled"
	}
	return "disabled"
}

func TestOpenAIWSLifetimeKeepsHealthyAffinity(t *testing.T) {
	pool, request := newLifetimeTestPool(t)
	first, err := pool.Acquire(context.Background(), request)
	require.NoError(t, err)
	first.conn.createdAtNano.Store(time.Now().Add(-54 * time.Minute).UnixNano())
	first.Release()
	require.True(t, pool.PinConn(request.Account.ID, first.ConnID()))
	request.PreferredConnID, request.ForcePreferredConn = first.ConnID(), true
	second, err := pool.Acquire(context.Background(), request)
	require.NoError(t, err)
	defer second.Release()
	require.Equal(t, first.ConnID(), second.ConnID())
	require.True(t, second.Reused())
}

func TestOpenAIWSLifetimeSweepRetiresPinnedButKeepsActive(t *testing.T) {
	for _, active := range []bool{false, true} {
		t.Run(testBoolName(active), func(t *testing.T) {
			pool, request := newLifetimeTestPool(t)
			lease, err := pool.Acquire(context.Background(), request)
			require.NoError(t, err)
			if !active {
				lease.Release()
			}
			require.True(t, pool.PinConn(request.Account.ID, lease.ConnID()))
			now := time.Now()
			lease.conn.createdAtNano.Store(now.Add(-55 * time.Minute).UnixNano())
			pool.runBackgroundCleanupSweep(now)
			require.Equal(t, !active, lease.conn.isClosed())
			if active {
				lease.Release()
				require.True(t, lease.conn.isClosed())
			}
			ap := pool.getOrCreateAccountPool(request.Account.ID)
			ap.mu.Lock()
			defer ap.mu.Unlock()
			require.NotContains(t, ap.conns, lease.ConnID())
			require.NotContains(t, ap.pinnedConns, lease.ConnID())
		})
	}
}

func TestOpenAIWSLifetimeReleaseWakesQueuedRequest(t *testing.T) {
	pool, request := newLifetimeTestPool(t)
	first, err := pool.Acquire(context.Background(), request)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	type acquireResult struct {
		lease *openAIWSConnLease
		err   error
	}
	result := make(chan acquireResult, 1)
	go func() {
		lease, err := pool.Acquire(ctx, request)
		result <- acquireResult{lease, err}
	}()
	require.Eventually(t, func() bool { return first.conn.waiters.Load() == 1 }, time.Second, time.Millisecond)
	first.conn.createdAtNano.Store(time.Now().Add(-55 * time.Minute).UnixNano())
	first.Release()
	select {
	case second := <-result:
		require.NoError(t, second.err)
		require.NotEqual(t, first.ConnID(), second.lease.ConnID())
		second.lease.Release()
	case <-ctx.Done():
		t.Fatal("到龄回收没有唤醒等待者重新建连")
	}
	require.True(t, first.conn.isClosed())
}

func TestOpenAIWSLifetimeLeaseTokenChecksAgeAfterWaiting(t *testing.T) {
	for _, poolChanged := range []bool{false, true} {
		t.Run(testBoolName(poolChanged), func(t *testing.T) {
			conn := newOpenAIWSConn("expired", 42, &openAIWSFakeConn{}, nil)
			defer conn.close()
			require.True(t, conn.tryAcquire())
			conn.createdAtNano.Store(time.Now().Add(-55 * time.Minute).UnixNano())
			conn.release()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			var err error
			if poolChanged {
				err = conn.acquireOrPoolChanged(ctx, make(chan struct{}))
			} else {
				err = conn.acquire(ctx)
			}
			require.ErrorIs(t, err, errOpenAIWSConnClosed)
			require.True(t, conn.isUnusable())
		})
	}
}
