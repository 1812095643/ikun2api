package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/google/uuid"
)

func (h *Handler) stopWarmupRun() {
	h.warmupMu.Lock()
	defer h.warmupMu.Unlock()
	if h.warmupCancel != nil {
		h.warmupCancel()
	}
}
func (h *Handler) startWarmupWorker() {
	if h.warmupGateway == nil || h.warmupUsage == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-h.background.Done():
				return
			case <-ticker.C:
			}
			ctx, cancel := context.WithTimeout(h.background, 40*time.Second)
			_ = h.enqueueWarmupSchedule(ctx)
			cancel()
		}
	}()
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-h.background.Done():
				return
			case <-ticker.C:
			}
			ctx, cancel := context.WithCancel(h.background)
			h.warmupMu.Lock()
			h.warmupCancel = cancel
			h.warmupMu.Unlock()
			_ = h.warmupTick(ctx)
			cancel()
			h.warmupMu.Lock()
			h.warmupCancel = nil
			h.warmupMu.Unlock()
		}
	}()
}
func (h *Handler) warmupEnabled(ctx context.Context) bool {
	var s warmupSettings
	return h.getRuntime(ctx, "account_warmup_settings", &s) == nil && s.Enabled
}
func waitWarmup(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

type warmupWork struct {
	ID, AccountID, RuleID, RuleVersion int64
	Deadline                           time.Time
	Config                             warmupConfig
	Quota                              *service.WarmupQuota
}

func (h *Handler) finishWarmup(ctx context.Context, id int64, status, reason string) error {
	_, err := h.db.ExecContext(ctx, "UPDATE workspace_warmup_runs SET status=$2,reason=$3,updated_at=NOW(),finished_at=NOW() WHERE id=$1 AND status<>'cancelled'", id, status, reason)
	return err
}
func (h *Handler) warmupTick(ctx context.Context) error {
	if !h.warmupEnabled(ctx) {
		return nil
	}
	conn, err := h.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var locked bool
	if err = conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock(748315264)").Scan(&locked); err != nil || !locked {
		return err
	}
	defer func() {
		release, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(release, "SELECT pg_advisory_unlock(748315264)")
	}()
	// Only the lock holder can classify an abandoned in-flight request. Never resend it.
	if _, err = h.db.ExecContext(ctx, "UPDATE workspace_warmup_runs SET status='unknown',reason='执行中断，是否被上游接受未知，不补发',finished_at=NOW(),updated_at=NOW() WHERE status IN ('running','checking')"); err != nil {
		return err
	}
	rules, err := h.warmupRules(ctx)
	if err != nil {
		return err
	}
	accounts, err := h.warmupAccounts(ctx)
	if err != nil {
		return err
	}
	overrides, err := h.warmupOverrides(ctx)
	if err != nil {
		return err
	}
	if err := h.enqueueWarmupSchedule(ctx); err != nil {
		return err
	}
	accountMap := map[int64]service.Account{}
	for _, a := range accounts {
		accountMap[a.ID] = a
	}
	rows, err := h.db.QueryContext(ctx, "SELECT r.id,r.account_id,r.rule_id,r.deadline,w.version,w.config FROM workspace_warmup_runs r JOIN workspace_warmup_rules w ON w.id=r.rule_id WHERE r.status='queued' AND r.planned_at<=NOW() AND w.enabled=TRUE ORDER BY r.planned_at,r.id LIMIT 500")
	if err != nil {
		return err
	}
	work := []warmupWork{}
	for rows.Next() {
		var x warmupWork
		var raw []byte
		if err = rows.Scan(&x.ID, &x.AccountID, &x.RuleID, &x.Deadline, &x.RuleVersion, &raw); err != nil {
			rows.Close()
			return err
		}
		if err = json.Unmarshal(raw, &x.Config); err != nil {
			rows.Close()
			return err
		}
		work = append(work, x)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	// Phase 1: all prospective accounts are actively refreshed, in sequence.
	// There is no passive/cached fallback and no parallel refresh goroutine.
	refreshed := map[int64]*service.WarmupQuota{}
	refreshErrors := map[int64]string{}
	for i := range work {
		x := &work[i]
		a, exists := accountMap[x.AccountID]
		reason := ""
		effective := effectiveWarmup(rules, a, overrides[a.ID])
		switch {
		case !exists || effective == nil || effective.ID != x.RuleID:
			reason = "账号已排除或生效规则已改变"
		case !h.warmupEnabled(ctx):
			return nil
		case time.Now().After(x.Deadline):
			reason = "执行时段已结束，不补跑"
		case a.IsRateLimited():
			reason = "账号仍处于 429 限流，跳过"
		case !a.IsSchedulable():
			reason = "账号当前不可调度"
		}
		if reason != "" {
			if err = h.finishWarmup(ctx, x.ID, "skipped", reason); err != nil {
				return err
			}
			continue
		}
		claim, e := h.db.ExecContext(ctx, "UPDATE workspace_warmup_runs SET status='checking',started_at=NOW(),updated_at=NOW() WHERE id=$1 AND status='queued'", x.ID)
		if e != nil {
			return e
		}
		claimed, _ := claim.RowsAffected()
		if claimed != 1 {
			continue
		}
		q, ok := refreshed[a.ID]
		problem := refreshErrors[a.ID]
		if !ok && problem == "" {
			probe, cancel := context.WithTimeout(ctx, 40*time.Second)
			q, err = h.warmupGateway.RefreshWarmupQuota(probe, h.warmupUsage, a.ID)
			cancel()
			if err != nil {
				problem = "主动额度查询失败或数据不完整，跳过"
				refreshErrors[a.ID] = problem
			} else {
				refreshed[a.ID] = q
			}
			if !waitWarmup(ctx, 2*time.Second) {
				return ctx.Err()
			}
		}
		if problem != "" {
			if err = h.finishWarmup(ctx, x.ID, "skipped", problem); err != nil {
				return err
			}
			continue
		}
		raw, _ := json.Marshal(map[string]any{"before": q, "quota_source": "active"})
		if _, err = h.db.ExecContext(ctx, "UPDATE workspace_warmup_runs SET snapshot=$2,updated_at=NOW() WHERE id=$1", x.ID, raw); err != nil {
			return err
		}
		if q.SkipReason() != "" {
			if err = h.finishWarmup(ctx, x.ID, "skipped", q.SkipReason()); err != nil {
				return err
			}
			continue
		}
		x.Quota = q
	}
	// Phase 2: only freshly checked accounts may send model requests.
	for _, x := range work {
		if x.Quota == nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := h.executeWarmup(ctx, conn, x); err != nil {
			return err
		}
		if !waitWarmup(ctx, time.Duration(x.Config.AccountGap)*time.Second) {
			return ctx.Err()
		}
	}
	return nil
}
func (h *Handler) executeWarmup(ctx context.Context, lease *sql.Conn, x warmupWork) error {
	runCtx, cancel := context.WithDeadline(ctx, x.Deadline)
	defer cancel()
	var accountLocked bool
	if err := lease.QueryRowContext(runCtx, "SELECT pg_try_advisory_lock(hashtextextended('admin-account-test/'||$1::text,0))", x.AccountID).Scan(&accountLocked); err != nil {
		return err
	}
	if !accountLocked {
		return h.finishWarmup(ctx, x.ID, "skipped", "旧定时测试正在使用该账号")
	}
	defer func() {
		release, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		_, _ = lease.ExecContext(release, "SELECT pg_advisory_unlock(hashtextextended('admin-account-test/'||$1::text,0))", x.AccountID)
	}()

	res, err := h.db.ExecContext(runCtx, "UPDATE workspace_warmup_runs SET status='running',updated_at=NOW() WHERE id=$1 AND status='checking'", x.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return nil
	}
	status, reason := "success", ""
	initialQuota := *x.Quota
	currentQuota := *x.Quota
	q := &currentQuota
	results := []service.WarmupAttemptResult{}
	session := uuid.NewString()
	finish := func() error {
		saveCtx, saveCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer saveCancel()
		raw, _ := json.Marshal(map[string]any{"before": &initialQuota, "last_active_quota": q, "results": results, "quota_source": "active"})
		if _, e := h.db.ExecContext(saveCtx, "UPDATE workspace_warmup_runs SET snapshot=$2 WHERE id=$1", x.ID, raw); e != nil {
			return e
		}
		return h.finishWarmup(saveCtx, x.ID, status, reason)
	}
	for ordinal := 1; ordinal <= x.Config.Count; ordinal++ {
		if !h.warmupEnabled(runCtx) {
			status, reason = "cancelled", "已停用或取消"
			break
		}
		a, err := h.admin.GetAccount(runCtx, x.AccountID)
		if err != nil {
			status, reason = "skipped", "账号已不可用"
			break
		}
		if a.IsRateLimited() {
			status, reason = "skipped", "账号仍处于 429 限流，跳过"
			break
		}
		if x.Config.SkipActive && ordinal == 1 && q.FiveHourReset != nil && q.FiveHourReset.After(time.Now()) && q.FiveHour > 0 {
			status, reason = "skipped", "五小时窗口已激活"
			break
		}
		if x.Config.IdleMinutes > 0 && a.LastUsedAt != nil && time.Since(*a.LastUsedAt) < time.Duration(x.Config.IdleMinutes)*time.Minute && ordinal == 1 {
			status, reason = "skipped", "近期已有业务请求"
			break
		}
		if time.Since(q.CheckedAt) > time.Minute {
			probe, stop := context.WithTimeout(runCtx, 40*time.Second)
			q, err = h.warmupGateway.RefreshWarmupQuota(probe, h.warmupUsage, x.AccountID)
			stop()
			if err != nil {
				status, reason = "skipped", "额度刷新失败，不使用旧数据"
				break
			}
		}
		if why := q.SkipReason(); why != "" {
			status, reason = "skipped", why
			break
		}
		loc, _ := time.LoadLocation(x.Config.Timezone)
		day := time.Now().In(loc)
		start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
		end := start.AddDate(0, 0, 1)
		var sent int
		if err = h.db.QueryRowContext(runCtx, "SELECT count(*) FROM workspace_warmup_attempts t JOIN workspace_warmup_runs r ON r.id=t.run_id WHERE r.account_id=$1 AND t.created_at >= $2 AND t.created_at < $3", x.AccountID, start, end).Scan(&sent); err != nil {
			return err
		}
		if sent >= x.Config.DailyMax {
			status, reason = "skipped", "已达每日请求上限"
			break
		}
		attemptCtx, stop := context.WithTimeout(runCtx, time.Duration(x.Config.Timeout)*time.Second)
		result, err := h.warmupGateway.RunAccountWarmup(attemptCtx, x.AccountID, x.Config.Model, session, ordinal, func() error {
			// A failed dedicated connection means the distributed lock is no longer trusted.
			if err := lease.PingContext(attemptCtx); err != nil {
				return err
			}
			if !h.warmupEnabled(attemptCtx) {
				return errors.New("warmup disabled")
			}
			var valid bool
			if err := h.db.QueryRowContext(attemptCtx, "SELECT r.status='running' AND w.enabled AND w.version=$2 FROM workspace_warmup_runs r JOIN workspace_warmup_rules w ON w.id=r.rule_id WHERE r.id=$1", x.ID, x.RuleVersion).Scan(&valid); err != nil || !valid {
				return errors.New("warmup rule changed or cancelled")
			}

			tx, err := h.db.BeginTx(attemptCtx, nil)
			if err != nil {
				return err
			}
			defer tx.Rollback()
			if _, err = tx.ExecContext(attemptCtx, "INSERT INTO workspace_warmup_attempts(run_id,ordinal,status) VALUES($1,$2,'sending')", x.ID, ordinal); err != nil {
				return err
			}
			if _, err = tx.ExecContext(attemptCtx, "UPDATE workspace_warmup_runs SET attempts=attempts+1,updated_at=NOW() WHERE id=$1", x.ID); err != nil {
				return err
			}
			return tx.Commit()
		})
		stop()
		if err != nil {
			status, reason = "failed", "执行条件检查失败"
			break
		}
		results = append(results, result)
		raw, _ := json.Marshal(result)
		if _, err = h.db.ExecContext(runCtx, "UPDATE workspace_warmup_attempts SET status=$3,summary=$4 WHERE run_id=$1 AND ordinal=$2", x.ID, ordinal, result.Status, raw); err != nil {
			status, reason = "unknown", "请求后保存失败，不补发"
			break
		}
		if _, err = h.db.ExecContext(runCtx, "UPDATE workspace_warmup_runs SET input_tokens=input_tokens+$2,output_tokens=output_tokens+$3,updated_at=NOW() WHERE id=$1", x.ID, result.InputTokens, result.OutputTokens); err != nil {
			status, reason = "unknown", "请求后保存失败，不补发"
			break
		}
		if result.Status != "success" {
			status, reason = result.Status, result.Reason
			break
		}
		// Invalidate the check between messages; the next message actively refreshes
		// again rather than trusting passive response headers for the >99% gate.
		q.CheckedAt = time.Time{}
		if ordinal < x.Config.Count && !waitWarmup(runCtx, time.Duration(x.Config.MessageGap)*time.Second) {
			status, reason = "cancelled", "已取消或时段结束"
			break
		}
	}
	if runCtx.Err() != nil {
		status, reason = "cancelled", "已取消或执行时段结束"
	}
	return finish()
}
func (h *Handler) enqueueWarmupSchedule(ctx context.Context) error {
	if !h.warmupEnabled(ctx) {
		return nil
	}
	rules, err := h.warmupRules(ctx)
	if err != nil {
		return err
	}
	accounts, err := h.warmupAccounts(ctx)
	if err != nil {
		return err
	}
	overrides, err := h.warmupOverrides(ctx)
	if err != nil {
		return err
	}
	now := time.Now()
	for _, a := range accounts {
		r := effectiveWarmup(rules, a, overrides[a.ID])
		if r == nil {
			continue
		}
		for _, o := range warmupOccurrences(r.Config, now) {
			occurrence := fmt.Sprintf("scheduled/%s/%s", o.Start.UTC().Format(time.RFC3339), o.Key)
			if _, err = h.db.ExecContext(ctx, "INSERT INTO workspace_warmup_runs(rule_id,account_id,occurrence,planned_at,deadline) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING", r.ID, a.ID, occurrence, o.Start, o.End); err != nil {
				return err
			}
		}
	}

	return nil
}
