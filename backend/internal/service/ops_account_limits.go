package service

import (
	"context"
	"sync"
	"time"
)

type opsAccountLimitContextKey struct{}

// Request-local receipts are only written after account state has been persisted.
// They cannot be supplied by an HTTP client or reused by another request.
type opsAccountLimitReceipts struct {
	mu      sync.Mutex
	handled map[int64]bool
}

func WithOpsAccountLimitTracking(ctx context.Context) context.Context {
	if _, ok := ctx.Value(opsAccountLimitContextKey{}).(*opsAccountLimitReceipts); ok {
		return ctx
	}
	return context.WithValue(ctx, opsAccountLimitContextKey{}, &opsAccountLimitReceipts{})
}

func recordOpsAccountLimit(ctx context.Context, accountID int64, handled bool) {
	if ctx == nil || accountID <= 0 {
		return
	}
	receipts, _ := ctx.Value(opsAccountLimitContextKey{}).(*opsAccountLimitReceipts)
	if receipts == nil {
		return
	}
	receipts.mu.Lock()
	defer receipts.mu.Unlock()
	if !handled {
		delete(receipts.handled, accountID)
		return
	}
	if receipts.handled == nil {
		receipts.handled = make(map[int64]bool)
	}
	if len(receipts.handled) < 128 {
		receipts.handled[accountID] = true
	}
}

func OpsAccountLimitHandled(ctx context.Context, accountID int64) bool {
	if ctx == nil || accountID <= 0 {
		return false
	}
	receipts, _ := ctx.Value(opsAccountLimitContextKey{}).(*opsAccountLimitReceipts)
	if receipts == nil {
		return false
	}
	receipts.mu.Lock()
	defer receipts.mu.Unlock()
	return receipts.handled[accountID]
}

func snapshotOpsAccountLimits(ctx context.Context) map[int64]bool {
	if ctx == nil {
		return nil
	}
	receipts, _ := ctx.Value(opsAccountLimitContextKey{}).(*opsAccountLimitReceipts)
	if receipts == nil {
		return nil
	}
	receipts.mu.Lock()
	defer receipts.mu.Unlock()
	if len(receipts.handled) == 0 {
		return nil
	}
	snapshot := make(map[int64]bool, len(receipts.handled))
	for id, handled := range receipts.handled {
		snapshot[id] = handled
	}
	return snapshot
}

func resetOpsAccountLimits(ctx context.Context) {
	if ctx == nil {
		return
	}
	receipts, _ := ctx.Value(opsAccountLimitContextKey{}).(*opsAccountLimitReceipts)
	if receipts == nil {
		return
	}
	receipts.mu.Lock()
	receipts.handled = nil
	receipts.mu.Unlock()
}

func (s *RateLimitService) setRateLimitedWithOpsReceipt(ctx context.Context, accountID int64, resetAt time.Time) error {
	err := s.accountRepo.SetRateLimited(ctx, accountID, resetAt)
	if err == nil && resetAt.After(time.Now()) {
		recordOpsAccountLimit(ctx, accountID, true)
	}
	return err
}
