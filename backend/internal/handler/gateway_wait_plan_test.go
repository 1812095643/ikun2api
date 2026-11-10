package handler

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/stretchr/testify/require"
)

// waitPlanCacheStub 按账号返回抢槽结果：freeAfter[id]=n 表示该账号第 n+1 次尝试起可抢到，
// 未登记的账号永远抢不到。
type waitPlanCacheStub struct {
	helperConcurrencyCacheStub

	slotMu       sync.Mutex
	occupied     map[int64]bool // 非 nil 时按真实占用判断：被占用则抢不到，释放后可抢
	freeAfter    map[int64]int
	attempts     map[int64]int
	released     map[int64]int
	waitAllow    bool
	waitInc      map[int64]int
	waitDec      map[int64]int
	waitIncCalls int
}

func newWaitPlanCacheStub(freeAfter map[int64]int) *waitPlanCacheStub {
	return &waitPlanCacheStub{
		freeAfter: freeAfter,
		attempts:  map[int64]int{},
		released:  map[int64]int{},
		waitAllow: true,
		waitInc:   map[int64]int{},
		waitDec:   map[int64]int{},
	}
}

func (s *waitPlanCacheStub) AcquireAccountSlot(ctx context.Context, accountID int64, maxConcurrency int, requestID string) (bool, error) {
	s.slotMu.Lock()
	defer s.slotMu.Unlock()
	s.attempts[accountID]++
	if s.occupied != nil {
		if s.occupied[accountID] {
			return false, nil
		}
		s.occupied[accountID] = true
		return true, nil
	}
	after, ok := s.freeAfter[accountID]
	return ok && s.attempts[accountID] > after, nil
}

func (s *waitPlanCacheStub) ReleaseAccountSlot(ctx context.Context, accountID int64, requestID string) error {
	s.slotMu.Lock()
	defer s.slotMu.Unlock()
	s.released[accountID]++
	if s.occupied != nil {
		s.occupied[accountID] = false
	}
	return nil
}

func (s *waitPlanCacheStub) IncrementAccountWaitCount(ctx context.Context, accountID int64, maxWait int) (bool, error) {
	s.slotMu.Lock()
	defer s.slotMu.Unlock()
	s.waitIncCalls++
	if !s.waitAllow {
		return false, nil
	}
	s.waitInc[accountID]++
	return true, nil
}

func (s *waitPlanCacheStub) DecrementAccountWaitCount(ctx context.Context, accountID int64) error {
	s.slotMu.Lock()
	defer s.slotMu.Unlock()
	s.waitDec[accountID]++
	return nil
}

func newWaitPlanHelper(cache *waitPlanCacheStub) *ConcurrencyHelper {
	return NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, 5*time.Millisecond)
}

func waitPlanFor(planned *service.Account, timeout time.Duration, alternates ...*service.Account) *service.AccountWaitPlan {
	return &service.AccountWaitPlan{AccountID: planned.ID, MaxConcurrency: planned.Concurrency, Timeout: timeout, MaxWaiting: 10, Alternates: alternates}
}

func TestAcquireSlotFromWaitPlan_TakesAlternateThatFreesFirst(t *testing.T) {
	planned := &service.Account{ID: 1, Concurrency: 5}
	alt := &service.Account{ID: 2, Concurrency: 5}
	cache := newWaitPlanCacheStub(map[int64]int{2: 2}) // 账号 1 一直满，账号 2 第 3 次尝试空出
	helper := newWaitPlanHelper(cache)
	c, _ := newHelperTestContext(http.MethodPost, "/v1/messages")

	resolved := &service.Account{ID: 2, Concurrency: 5, Name: "hydrated"}
	streamStarted := false
	got, release, err := helper.AcquireSlotFromWaitPlan(c, planned, waitPlanFor(planned, time.Second, alt), func(_ context.Context, acc *service.Account) (*service.Account, error) {
		require.Equal(t, int64(2), acc.ID)
		return resolved, nil
	}, false, &streamStarted)

	require.NoError(t, err)
	require.Same(t, resolved, got, "returns the hydrated alternate")
	require.NotNil(t, release)
	release()
	require.Equal(t, 1, cache.released[2])
	require.Zero(t, cache.released[1])
	require.GreaterOrEqual(t, cache.attempts[1], 3, "planned account is still tried first on every poll")
}

func TestAcquireSlotFromWaitPlan_PrefersPlannedAccount(t *testing.T) {
	planned := &service.Account{ID: 1, Concurrency: 5}
	alt := &service.Account{ID: 2, Concurrency: 5}
	cache := newWaitPlanCacheStub(map[int64]int{1: 1, 2: 0})
	helper := newWaitPlanHelper(cache)
	c, _ := newHelperTestContext(http.MethodPost, "/v1/messages")

	streamStarted := false
	got, release, err := helper.AcquireSlotFromWaitPlan(c, planned, waitPlanFor(planned, time.Second, alt), nil, false, &streamStarted)
	require.NoError(t, err)
	// 第一轮账号 1 未空出、账号 2 可抢 → 改用账号 2。
	require.Equal(t, int64(2), got.ID)
	release()

	cache = newWaitPlanCacheStub(map[int64]int{1: 0, 2: 0})
	helper = newWaitPlanHelper(cache)
	got, release, err = helper.AcquireSlotFromWaitPlan(c, planned, waitPlanFor(planned, time.Second, alt), nil, false, &streamStarted)
	require.NoError(t, err)
	require.Same(t, planned, got, "both free: planned account wins")
	require.Zero(t, cache.attempts[2])
	release()
}

func TestAcquireSlotFromWaitPlan_ResolveFailureReleasesAndSkips(t *testing.T) {
	planned := &service.Account{ID: 1, Concurrency: 5}
	broken := &service.Account{ID: 2, Concurrency: 5}
	healthy := &service.Account{ID: 3, Concurrency: 5}
	cache := newWaitPlanCacheStub(map[int64]int{2: 0, 3: 1})
	helper := newWaitPlanHelper(cache)
	c, _ := newHelperTestContext(http.MethodPost, "/v1/messages")

	streamStarted := false
	got, release, err := helper.AcquireSlotFromWaitPlan(c, planned, waitPlanFor(planned, time.Second, broken, healthy), func(_ context.Context, acc *service.Account) (*service.Account, error) {
		if acc.ID == 2 {
			return nil, errors.New("snapshot miss")
		}
		return acc, nil
	}, false, &streamStarted)
	require.NoError(t, err)
	require.Equal(t, int64(3), got.ID)
	require.Equal(t, 1, cache.released[2], "slot taken on the broken alternate is released")
	require.Equal(t, 1, cache.attempts[2], "broken alternate is not retried")
	release()
}

func TestAcquireSlotFromWaitPlan_TimeoutReturnsConcurrencyError(t *testing.T) {
	planned := &service.Account{ID: 1, Concurrency: 5}
	alt := &service.Account{ID: 2, Concurrency: 5}
	cache := newWaitPlanCacheStub(nil)
	helper := newWaitPlanHelper(cache)
	c, _ := newHelperTestContext(http.MethodPost, "/v1/messages")

	streamStarted := false
	got, release, err := helper.AcquireSlotFromWaitPlan(c, planned, waitPlanFor(planned, 150*time.Millisecond, alt), nil, false, &streamStarted)
	var concurrencyErr *ConcurrencyError
	require.ErrorAs(t, err, &concurrencyErr)
	require.True(t, concurrencyErr.IsTimeout)
	require.Nil(t, release)
	require.Same(t, planned, got)
	require.Positive(t, cache.attempts[2])
}

func TestAcquireSlotFromWaitPlan_NoAlternatesKeepsSingleAccountWait(t *testing.T) {
	planned := &service.Account{ID: 1, Concurrency: 5}
	cache := newWaitPlanCacheStub(map[int64]int{1: 1})
	helper := newWaitPlanHelper(cache)
	c, _ := newHelperTestContext(http.MethodPost, "/v1/messages")

	streamStarted := false
	got, release, err := helper.AcquireSlotFromWaitPlan(c, planned, waitPlanFor(planned, time.Second), nil, false, &streamStarted)
	require.NoError(t, err)
	require.Same(t, planned, got)
	require.Equal(t, 2, cache.attempts[1])
	release()
}

func TestWaitForPlannedAccountSlot_CountsWaiterOnPlannedAccount(t *testing.T) {
	planned := &service.Account{ID: 1, Concurrency: 5}
	alt := &service.Account{ID: 2, Concurrency: 5}
	cache := newWaitPlanCacheStub(map[int64]int{2: 1})
	helper := newWaitPlanHelper(cache)
	c, _ := newHelperTestContext(http.MethodPost, "/v1/chat/completions")

	streamStarted := false
	got, release, err := helper.WaitForPlannedAccountSlot(c, planned, waitPlanFor(planned, time.Second, alt), nil, false, &streamStarted)
	require.NoError(t, err)
	require.Equal(t, int64(2), got.ID)
	require.Equal(t, 1, cache.waitInc[1])
	require.Equal(t, 1, cache.waitDec[1], "waiter is un-counted on the account it was counted on")
	require.Zero(t, cache.waitInc[2])
	release()
}

func TestWaitForPlannedAccountSlot_QueueFull(t *testing.T) {
	planned := &service.Account{ID: 1, Concurrency: 5}
	cache := newWaitPlanCacheStub(map[int64]int{1: 0})
	cache.waitAllow = false
	helper := newWaitPlanHelper(cache)
	c, _ := newHelperTestContext(http.MethodPost, "/v1/chat/completions")

	streamStarted := false
	_, release, err := helper.WaitForPlannedAccountSlot(c, planned, waitPlanFor(planned, time.Second), nil, false, &streamStarted)
	var queueFull *WaitQueueFullError
	require.ErrorAs(t, err, &queueFull)
	require.Nil(t, release)
	require.Zero(t, cache.attempts[1], "no slot attempt when the queue is full")
	require.Zero(t, cache.waitDec[1])

	status, _, code, _ := concurrencyErrorResponse(err, "account")
	require.Equal(t, http.StatusTooManyRequests, status)
	require.Equal(t, gatewayQueueFullCode, code)
}

func TestAcquireSlotFromWaitPlan_GraceOnlyTriesPlannedAccount(t *testing.T) {
	planned := &service.Account{ID: 1, Concurrency: 5}
	alt := &service.Account{ID: 2, Concurrency: 5}
	cache := newWaitPlanCacheStub(map[int64]int{2: 0}) // 替换账号一直空闲
	helper := newWaitPlanHelper(cache)
	c, _ := newHelperTestContext(http.MethodPost, "/v1/messages")

	plan := waitPlanFor(planned, time.Second, alt)
	plan.AlternatesAfter = 250 * time.Millisecond
	streamStarted := false
	start := time.Now()
	got, release, err := helper.AcquireSlotFromWaitPlan(c, planned, plan, nil, false, &streamStarted)
	elapsed := time.Since(start)

	require.NoError(t, err)
	require.Equal(t, int64(2), got.ID)
	require.GreaterOrEqual(t, elapsed, 250*time.Millisecond, "alternate must not be used during the grace period")
	require.Less(t, elapsed, 600*time.Millisecond, "grace expiry wakes the waiter instead of waiting for the next backoff tick")
	release()
}

func TestAcquireSlotFromWaitPlan_PlannedAccountWinsWithinGrace(t *testing.T) {
	planned := &service.Account{ID: 1, Concurrency: 5}
	alt := &service.Account{ID: 2, Concurrency: 5}
	cache := newWaitPlanCacheStub(map[int64]int{1: 1, 2: 0})
	helper := newWaitPlanHelper(cache)
	c, _ := newHelperTestContext(http.MethodPost, "/v1/messages")

	plan := waitPlanFor(planned, time.Second, alt)
	plan.AlternatesAfter = 5 * time.Second
	streamStarted := false
	got, release, err := helper.AcquireSlotFromWaitPlan(c, planned, plan, nil, false, &streamStarted)
	require.NoError(t, err)
	require.Same(t, planned, got)
	require.Zero(t, cache.attempts[2])
	release()
}

// 释放槽位应立即唤醒等待者，而不是等退避计时（最长 2 秒）。
func TestAcquireSlotFromWaitPlan_ReleaseWakesWaiterImmediately(t *testing.T) {
	cache := newWaitPlanCacheStub(nil)
	cache.occupied = map[int64]bool{}
	concurrency := service.NewConcurrencyService(cache)
	helper := NewConcurrencyHelper(concurrency, SSEPingFormatNone, 5*time.Millisecond)

	holder, err := concurrency.AcquireAccountSlot(context.Background(), 1, 1)
	require.NoError(t, err)
	require.True(t, holder.Acquired)

	planned := &service.Account{ID: 1, Concurrency: 1}
	c, _ := newHelperTestContext(http.MethodPost, "/v1/messages")
	type outcome struct {
		release func()
		err     error
		at      time.Time
	}
	done := make(chan outcome, 1)
	go func() {
		streamStarted := false
		_, release, err := helper.AcquireSlotFromWaitPlan(c, planned, waitPlanFor(planned, 5*time.Second), nil, false, &streamStarted)
		done <- outcome{release, err, time.Now()}
	}()

	// 首次立即尝试已失败、第一次退避轮询（≥80ms）还没到时释放：只有唤醒能让它马上拿到。
	time.Sleep(30 * time.Millisecond)
	releasedAt := time.Now()
	holder.ReleaseFunc()

	select {
	case res := <-done:
		require.NoError(t, res.err)
		require.Less(t, res.at.Sub(releasedAt), 25*time.Millisecond, "woken by the release, not by the next backoff tick")
		res.release()
	case <-time.After(3 * time.Second):
		t.Fatal("waiter was not woken by the release")
	}
}

func TestPollSlotWithPing_RecordsWaitTime(t *testing.T) {
	cache := newWaitPlanCacheStub(map[int64]int{1: 2})
	helper := newWaitPlanHelper(cache)
	c, _ := newHelperTestContext(http.MethodPost, "/v1/messages")

	streamStarted := false
	release, err := helper.AcquireAccountSlotWithWaitTimeout(c, 1, 5, time.Second, false, &streamStarted)
	require.NoError(t, err)
	release()
	require.Positive(t, gatewayWaitDuration(c))
}
