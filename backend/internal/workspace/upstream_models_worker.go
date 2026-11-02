package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"strings"
	"time"
)

func (h *Handler) startUpstreamModelWorker() {
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-h.background.Done():
				return
			case <-ticker.C:
			}
			ctx, cancel := context.WithTimeout(h.background, 4*time.Minute)
			err := h.processUpstreamModels(ctx)
			cancel()
			if err != nil && h.background.Err() == nil {
				slog.Warn("上游模型检测未完成", "error_type", fmt.Sprintf("%T", err))
			}
		}
	}()
}
func (h *Handler) processUpstreamModels(ctx context.Context) error {
	conn, err := h.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	lock, err := conn.BeginTx(context.WithoutCancel(ctx), nil)
	if err != nil {
		return err
	}
	defer lock.Rollback()
	var acquired bool
	if err = lock.QueryRowContext(ctx, "SELECT pg_try_advisory_xact_lock(748315207)").Scan(&acquired); err != nil || !acquired {
		return err
	}
	groups, err := h.discoverUpstreamBalances(ctx)
	if err != nil {
		return err
	}
	var group upstreamBalanceGroup
	var record upstreamModelRecord
	chosen := false
	for _, g := range groups {
		r, e := h.loadModelRecord(ctx, g.ID)
		if e != nil {
			continue
		}
		if r.Summary.Checking || (!r.Summary.Queued && (!r.Config.Enabled || r.next.After(time.Now()))) {
			continue
		}
		if !chosen || (r.Summary.Queued && !record.Summary.Queued) || (r.Summary.Queued == record.Summary.Queued && r.next.Before(record.next)) {
			group = g
			record = r
			chosen = true
		}
	}
	if !chosen {
		return nil
	}
	claim, err := h.db.ExecContext(ctx, `UPDATE workspace_upstream_models SET lease_until=NOW()+INTERVAL '5 minutes',refresh_requested=FALSE WHERE upstream_id=$1 AND revision=$2 AND (lease_until IS NULL OR lease_until<NOW())`, group.ID, record.Revision)
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
		_, _ = h.db.ExecContext(cleanup, `UPDATE workspace_upstream_models SET lease_until=NULL WHERE upstream_id=$1`, group.ID)
	}()
	before := modelChangesDigest(record.Accounts)
	old := map[int64]upstreamModelAccountState{}
	for _, s := range record.Accounts {
		old[s.AccountID] = s
	}
	states := []upstreamModelAccountState{}
	type cached struct {
		models []string
		err    error
	}
	cache := map[string]cached{}
	for _, a := range group.Accounts {
		proxy := ""
		if a.proxyID != nil {
			proxy = fmt.Sprint(*a.proxyID)
		}
		cacheID := digestString(a.key + ":" + a.Platform + ":" + proxy)
		result, ok := cache[cacheID]
		if !ok {
			if ctx.Err() != nil {
				result.err = badRequest("本轮检测超时。")
			} else {
				client, closeClient, e := h.upstreamBalanceClient(ctx, group.URL, a.proxyID)
				if e != nil {
					result.err = badRequest("模型查询连接不可用，请检查地址和代理。")
				} else {
					result.models, result.err = queryUpstreamModels(ctx, client, group.URL, a.key)
					closeClient()
				}
			}
			cache[cacheID] = result
		}
		states = append(states, observeModelCatalog(old[a.ID], a, result.models, result.err, time.Now().UTC()))
	}
	// Revalidate the live source before publishing or notifying.
	current, err := h.discoverUpstreamBalances(ctx)
	if err != nil {
		return err
	}
	valid := false
	for _, g := range current {
		if g.ID == group.ID && g.SourceHash == group.SourceHash {
			valid = true
		}
	}
	if !valid {
		return nil
	}
	record.Accounts = states
	if before != modelChangesDigest(states) {
		addModelHistory(&record, "detected", 0, modelChanges(states))
	}
	raw, _ := json.Marshal(states)
	hist, _ := json.Marshal(record.History)
	saved, err := h.db.ExecContext(ctx, `UPDATE workspace_upstream_models SET state=$3,history=$4,revision=revision+1,last_checked_at=NOW(),next_check_at=NOW()+($5*INTERVAL '1 minute') WHERE upstream_id=$1 AND revision=$2`, group.ID, record.Revision, string(raw), string(hist), record.Config.IntervalMinutes)
	if err != nil {
		return err
	}
	n, _ = saved.RowsAffected()
	if n == 0 {
		return nil
	}
	record.Revision++
	return h.notifyUpstreamModels(ctx, group, record)
}
func (h *Handler) notifyUpstreamModels(ctx context.Context, g upstreamBalanceGroup, r upstreamModelRecord) error {
	if !r.Config.Notify {
		return nil
	}
	changes := modelChanges(r.Accounts)
	a, pending, keys := modelNotificationPlan(r)
	if len(pending) == 0 {
		raw, _ := json.Marshal(a)
		_, err := h.db.ExecContext(ctx, `UPDATE workspace_upstream_models SET notification=$3 WHERE upstream_id=$1 AND revision=$2`, g.ID, r.Revision, string(raw))
		return err
	}
	now := time.Now().UTC()
	if a.Attempt != nil && now.Sub(*a.Attempt) < time.Hour {
		return nil
	}
	a.Attempt = &now
	persist := func() error {
		raw, _ := json.Marshal(a)
		_, err := h.db.ExecContext(ctx, `UPDATE workspace_upstream_models SET notification=$3 WHERE upstream_id=$1 AND revision=$2`, g.ID, r.Revision, string(raw))
		return err
	}
	// Save attempt before SMTP so a crash cannot cause a tight retry loop.
	if err := persist(); err != nil {
		return err
	}
	var name string
	if err := h.db.QueryRowContext(ctx, "SELECT name FROM workspace_upstream_balances WHERE id=$1", g.ID).Scan(&name); err != nil {
		return err
	}
	var content strings.Builder
	content.WriteString("<p>" + html.EscapeString(name) + " 的模型目录发生变化。</p>")
	for _, d := range changes {
		content.WriteString(fmt.Sprintf("<p>账号 #%d：新增 %d 个，下架 %d 个。</p>", d.AccountID, len(d.Added), len(d.Removed)))
	}
	content.WriteString("<p>请在上游余额页面查看并选择应用或忽略。模型映射尚未自动修改。</p>")
	a.Error = ""
	for _, to := range pending {
		var valid bool
		if err := h.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM workspace_upstream_models WHERE upstream_id=$1 AND revision=$2 AND config->>'notify'='true')`, g.ID, r.Revision).Scan(&valid); err != nil {
			return err
		} else if !valid {
			return nil
		}
		if h.email == nil || h.email.SendEmail(ctx, to, "[IKUN2api] 上游模型变更", content.String()) != nil {
			a.Error = "模型变更邮件发送失败，将在后续检测中重试。"
		} else {
			a.Delivered[to] = append([]string{}, keys...)
		}
		if err := persist(); err != nil {
			return err
		}
	}
	return nil
}

// Deduplicate individual pending changes per recipient, so acknowledging some
// items never resends the others. A disappearance clears the event for reappearance.
func modelNotificationPlan(r upstreamModelRecord) (upstreamModelNotice, []string, []string) {
	a := r.notice
	if a.Delivered == nil {
		a.Delivered = map[string][]string{}
	}
	keys := []string{}
	for _, s := range r.Accounts {
		d := pendingModelChange(s)
		for _, id := range d.Added {
			keys = append(keys, digestString(fmt.Sprintf("%d:%s:add:%s", s.AccountID, s.Source, id)))
		}
		for _, id := range d.Removed {
			keys = append(keys, digestString(fmt.Sprintf("%d:%s:remove:%s", s.AccountID, s.Source, id)))
		}
	}
	keys = sortedModelSet(keys)
	for to, sent := range a.Delivered {
		a.Delivered[to] = modelIntersection(sent, keys)
	}
	pending := []string{}
	for _, to := range r.Config.Recipients {
		if len(modelDifference(keys, a.Delivered[to])) > 0 {
			pending = append(pending, to)
		}
	}
	if len(keys) == 0 {
		a.Attempt = nil
		a.Error = ""
	}
	return a, pending, keys
}
