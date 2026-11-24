package repository

import (
	"context"
	"database/sql"
	"time"

	dbent "github.com/MACOS-DO/sub4api/ent"
	dbaccount "github.com/MACOS-DO/sub4api/ent/account"
	"github.com/MACOS-DO/sub4api/internal/accounttimer"
	"github.com/MACOS-DO/sub4api/internal/service"
)

func (r *accountRepository) SchedulableTimers() accounttimer.Repository {
	db, ok := r.sql.(*sql.DB)
	if !ok || db == nil {
		return nil
	}
	return &accounttimer.Store{
		DB: db, Policy: accounttimer.ConfiguredPolicy(),
		Enqueue: func(ctx context.Context, tx *sql.Tx, id int64) error {
			return enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountChanged, &id, nil, nil)
		},
		AfterCommit: r.syncSchedulerAccountSnapshotDetached,
	}
}

// 手动操作和取消计划在同一事务内提交，避免计划随后覆盖管理员的选择。
func cancelManualAccountTimers(ctx context.Context, exec accounttimer.Executor, id int64) error {
	return accounttimer.CancelPending(ctx, exec, id, time.Now())
}

func (r *accountRepository) setManualSchedulable(ctx context.Context, id int64, enabled bool) error {
	baseCtx := ctx
	contextTx := dbent.TxFromContext(ctx)
	client := r.client
	var tx *dbent.Tx
	if contextTx != nil {
		client = contextTx.Client()
	} else {
		var err error
		tx, err = client.Tx(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		client = tx.Client()
	}
	n, err := client.Account.Update().Where(dbaccount.IDEQ(id), dbaccount.DeletedAtIsNil()).SetSchedulable(enabled).Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return service.ErrAccountNotFound
	}
	if _, err := client.ExecContext(ctx, `UPDATE accounts SET extra=jsonb_set(COALESCE(extra,'{}'::jsonb),'{openai_oauth_recovery}','null'::jsonb)
	 WHERE id=$1 AND platform='openai' AND type='oauth'`, id); err != nil {
		return err
	}
	if err := cancelManualAccountTimers(ctx, client, id); err != nil {
		return err
	}
	if err := enqueueSchedulerOutbox(ctx, client, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		return err
	}
	if tx != nil {
		if err := tx.Commit(); err != nil {
			return err
		}
		r.syncSchedulerAccountSnapshot(baseCtx, id)
	}
	return nil
}
