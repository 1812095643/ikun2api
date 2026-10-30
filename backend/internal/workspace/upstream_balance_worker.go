package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"time"
)

func (h *Handler) startUpstreamBalanceWorker() {
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-h.background.Done():
				return
			case <-ticker.C:
			case <-h.balanceWake:
			}
			ctx, cancel := context.WithTimeout(h.background, 4*time.Minute)
			err := h.processUpstreamBalance(ctx)
			cancel()
			if err != nil && h.background.Err() == nil {
				slog.Warn("上游余额检查未完成", "error_type", fmt.Sprintf("%T", err))
			}
		}
	}()
}
func upstreamBalanceDue(r upstreamBalanceRecord, now time.Time) bool {
	if r.leaseUntil != nil && r.leaseUntil.After(now) {
		return false
	}
	return r.Queued || (r.Config.MonitorEnabled && (r.NextCheckAt == nil || !r.NextCheckAt.After(now)))
}
func (h *Handler) processUpstreamBalance(ctx context.Context) error {
	// A transaction-scoped PostgreSQL advisory lock serializes scheduled checks,
	// manual refreshes and notifications across processes/restarts, not just tabs.
	conn, err := h.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	// SMTP has its own bounded I/O timeout. Keep ownership until it returns,
	// even if the request context expires while an email is being delivered.
	lock, err := conn.BeginTx(context.WithoutCancel(ctx), nil)
	if err != nil {
		return err
	}
	defer lock.Rollback()
	var acquired bool
	if err = lock.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(748315206)`).Scan(&acquired); err != nil || !acquired {
		return err
	}
	groups, records, err := h.prepareUpstreamBalances(ctx)
	if err != nil {
		return err
	}
	var chosen *upstreamBalanceRecord
	// Manual requests first; then process the oldest scheduled record. Avoid
	// starvation when a slow provider is due again before others were checked.
	for i := range records {
		r := &records[i]
		if !upstreamBalanceDue(*r, time.Now()) {
			continue
		}
		if chosen == nil || (r.Queued && !chosen.Queued) || (r.Queued == chosen.Queued && r.NextCheckAt.Before(*chosen.NextCheckAt)) {
			chosen = r
		}
	}
	if chosen == nil {
		return nil
	}
	r := *chosen
	claim, err := h.db.ExecContext(ctx, `UPDATE workspace_upstream_balances SET lease_until=NOW()+INTERVAL '5 minutes',refresh_requested=FALSE
  WHERE id=$1 AND revision=$2 AND (lease_until IS NULL OR lease_until<=NOW())`, r.ID, r.revision)
	if err != nil {
		return err
	}
	n, _ := claim.RowsAffected()
	if n == 0 {
		return nil
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = h.db.ExecContext(cleanup, `UPDATE workspace_upstream_balances SET lease_until=NULL WHERE id=$1`, r.ID)
	}()
	var group upstreamBalanceGroup
	for _, g := range groups {
		if g.ID == r.ID {
			group = g
			break
		}
	}
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	snapshot := h.queryUpstreamBalanceGroup(queryCtx, group, r)
	cancel()
	// Source rotation/deletion during I/O cannot publish a stale result.
	current, e := h.discoverUpstreamBalances(ctx)
	if e != nil {
		return e
	}
	matching := false
	for _, g := range current {
		if g.ID == group.ID && g.SourceHash == group.SourceHash {
			matching = true
		}
	}
	if !matching {
		return h.syncUpstreamBalances(ctx, current)
	}
	raw, _ := json.Marshal(snapshot)
	saved, err := h.db.ExecContext(ctx, `UPDATE workspace_upstream_balances SET snapshot=$3,last_checked_at=NOW(),next_check_at=NOW()+($4*INTERVAL '1 minute')
  WHERE id=$1 AND revision=$2 AND source_hash=$5`, r.ID, r.revision, string(raw), r.Config.IntervalMinutes, group.SourceHash)
	if err != nil {
		return err
	}
	n, _ = saved.RowsAffected()
	if n == 0 {
		return nil
	}
	return h.notifyUpstreamBalance(ctx, r, snapshot)
}
func (h *Handler) queryUpstreamBalanceGroup(ctx context.Context, g upstreamBalanceGroup, r upstreamBalanceRecord) upstreamBalanceSnapshot {
	wallet := ""
	if r.secret != "" && r.Config.Kind != "sub2api" {
		if h.encryptor == nil {
			return upstreamBalanceSnapshot{Readings: []upstreamBalanceReading{}, Error: "凭据加密服务不可用。"}
		}
		var err error
		wallet, err = h.encryptor.Decrypt(r.secret)
		if err != nil {
			return upstreamBalanceSnapshot{Readings: []upstreamBalanceReading{}, Error: "无法解密钱包访问令牌，请重新设置。"}
		}
	}
	// Query duplicate keys only once. Keep all associated account IDs for selection.
	readings := []upstreamBalanceReading{}
	indexes := map[string]int{}
	accounts := append([]upstreamBalanceAccount(nil), g.Accounts...)
	for i := range accounts {
		if accounts[i].ID == r.Config.PreferredAccountID {
			accounts[0], accounts[i] = accounts[i], accounts[0]
			break
		}
	}
	for _, a := range accounts {
		identity := a.key
		if wallet != "" {
			identity = "wallet-token"
		}
		if i, ok := indexes[identity]; ok {
			readings[i].AccountIDs = append(readings[i].AccountIDs, a.ID)
			readings[i].AccountNames = append(readings[i].AccountNames, a.Name)
			continue
		}
		reading := upstreamBalanceReading{}
		if ctx.Err() != nil {
			reading.Error = "本轮查询超时。"
		} else {
			client, closeClient, err := h.upstreamBalanceClient(ctx, g.URL, a.proxyID)
			if err == nil {
				reading, err = queryUpstreamBalance(ctx, client, g.URL, a.key, wallet, r.Config)
				closeClient()
			}
			if err != nil {
				reading.Error = err.Error()
			}
		}
		reading.AccountIDs = []int64{a.ID}
		reading.AccountNames = []string{a.Name}
		indexes[identity] = len(readings)
		readings = append(readings, reading)
	}
	return summarizeUpstreamBalances(readings, r.Config.PreferredAccountID)
}

func upstreamBalanceAlertPlan(r upstreamBalanceRecord, s upstreamBalanceSnapshot, now time.Time) (upstreamBalanceAlertStored, []string) {
	a := r.storedAlert
	a.Error = ""
	if !r.Config.MonitorEnabled {
		a.Low = false
		a.RecipientsSent = []string{}
		return a, nil
	}
	if s.Balance == nil && !s.Unlimited {
		a.Error = s.Error
		return a, nil
	}
	low := s.Balance != nil && *s.Balance < r.Config.Threshold
	if !low {
		a.Low = false
		a.RecipientsSent = []string{}
		return a, nil
	}
	if !a.Low {
		a.RecipientsSent = []string{}
	}
	a.Low = true
	if a.LastAttempt != nil && now.Sub(*a.LastAttempt) < time.Hour {
		return a, nil
	}
	pending := []string{}
	for _, to := range r.Config.Recipients {
		found := false
		for _, sent := range a.RecipientsSent {
			if sent == to {
				found = true
			}
		}
		if !found {
			pending = append(pending, to)
		}
	}
	return a, pending
}
func (h *Handler) notifyUpstreamBalance(ctx context.Context, r upstreamBalanceRecord, s upstreamBalanceSnapshot) error {
	now := time.Now().UTC()
	a, pending := upstreamBalanceAlertPlan(r, s, now)
	if len(pending) > 0 {
		a.LastAttempt = &now
	}
	persist := func() (bool, error) {
		raw, _ := json.Marshal(a)
		res, e := h.db.ExecContext(ctx, `UPDATE workspace_upstream_balances SET alert=$3 WHERE id=$1 AND revision=$2`, r.ID, r.revision, string(raw))
		if e != nil {
			return false, e
		}
		n, _ := res.RowsAffected()
		return n > 0, nil
	}
	ok, err := persist()
	if err != nil || !ok {
		return err
	}
	if s.Balance == nil {
		return nil
	}
	for _, to := range pending {
		var valid bool
		if err = h.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM workspace_upstream_balances WHERE id=$1 AND revision=$2)`, r.ID, r.revision).Scan(&valid); err != nil || !valid {
			return err
		}
		// Reuse system SMTP. Never include keys, raw upstream responses or mail credentials.
		content := "<p>" + html.EscapeString(r.Name) + " 的余额低于设置的阈值。</p><p>当前余额：" + html.EscapeString(fmt.Sprintf("%.4f %s", *s.Balance, s.Unit)) + "；提醒阈值：" + html.EscapeString(fmt.Sprintf("%g %s", r.Config.Threshold, s.Unit)) + "。</p><p>上游：" + html.EscapeString(r.URL) + "</p>"
		if h.email == nil || h.email.SendEmail(ctx, to, "[IKUN2api] 上游余额提醒", content) != nil {
			a.Error = "提醒邮件发送失败，请检查系统邮件设置；将在后续检查中重试。"
		} else {
			a.RecipientsSent = append(a.RecipientsSent, to)
			sent := time.Now().UTC()
			a.LastSent = &sent
		}
		if _, err = persist(); err != nil {
			return err
		}
	}
	return nil
}
