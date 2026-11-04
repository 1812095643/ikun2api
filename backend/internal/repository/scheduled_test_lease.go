package repository

import (
	"context"
	"time"
)

// WithAccountTestLease coordinates legacy scheduled checks with administrator
// warmup rounds. Both use the same account-scoped PostgreSQL advisory key.
func (r *scheduledTestPlanRepository) WithAccountTestLease(ctx context.Context, id int64, run func()) (bool, error) {
	conn, err := r.db.Conn(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	var locked bool
	if err = conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock(hashtextextended('admin-account-test/'||$1::text,0))", id).Scan(&locked); err != nil || !locked {
		return false, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(cleanup, "SELECT pg_advisory_unlock(hashtextextended('admin-account-test/'||$1::text,0))", id)
	}()
	run()
	return true, nil
}
