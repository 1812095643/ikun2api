//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func signaled(w *SlotReleaseWatch) bool {
	select {
	case <-w.C():
		return true
	default:
		return false
	}
}

func TestSlotWaitHub_WakesOldestWaiterFirst(t *testing.T) {
	hub := newSlotWaitHub()
	first, second := hub.watch(), hub.watch()
	second.Add(SlotKindAccount, 7)
	first.Add(SlotKindAccount, 7) // 后登记账号也按最初入队顺序排在前面

	hub.notify(SlotKindAccount, 7)
	require.True(t, signaled(first))
	require.False(t, signaled(second))

	// 最早的等待者已有未处理信号时，下一次释放交给下一个等待者。
	hub.notify(SlotKindAccount, 7)
	hub.notify(SlotKindAccount, 7)
	require.True(t, signaled(first))
	require.True(t, signaled(second))
}

func TestSlotWaitHub_KeysAreIndependentAndCloseUnregisters(t *testing.T) {
	hub := newSlotWaitHub()
	accountWaiter := hub.watch()
	accountWaiter.Add(SlotKindAccount, 1)
	userWaiter := hub.watch()
	userWaiter.Add(SlotKindUser, 1)

	hub.notify(SlotKindUser, 1)
	require.False(t, signaled(accountWaiter))
	require.True(t, signaled(userWaiter))

	accountWaiter.Close()
	hub.notify(SlotKindAccount, 1)
	require.False(t, signaled(accountWaiter))
	userWaiter.Close()
	require.Empty(t, hub.queues)
}

func TestSlotWaitHub_NilSafe(t *testing.T) {
	var w *SlotReleaseWatch
	require.Nil(t, w.C())
	w.Add(SlotKindAccount, 1)
	w.Poke()
	w.Close()
	require.Nil(t, (&ConcurrencyService{}).WatchSlotRelease(SlotKindAccount, 1))
}

func TestConcurrencyService_ReleaseWakesWaiter(t *testing.T) {
	svc := NewConcurrencyService(&stubConcurrencyCacheForTest{acquireResult: true})
	accountWatch := svc.WatchSlotRelease(SlotKindAccount, 42)
	defer accountWatch.Close()
	userWatch := svc.WatchSlotRelease(SlotKindUser, 9)
	defer userWatch.Close()

	result, err := svc.AcquireAccountSlot(context.Background(), 42, 1)
	require.NoError(t, err)
	require.True(t, result.Acquired)
	require.False(t, signaled(accountWatch))
	result.ReleaseFunc()
	require.True(t, signaled(accountWatch))

	userResult, err := svc.AcquireUserSlot(context.Background(), 9, 1)
	require.NoError(t, err)
	userResult.ReleaseFunc()
	require.False(t, signaled(userWatch), "user-wide concurrency retains its existing polling behavior")
}
