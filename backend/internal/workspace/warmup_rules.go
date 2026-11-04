package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/pkg/claude"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/response"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type warmupWindow struct {
	Start  string `json:"start"`
	End    string `json:"end"`
	Rounds int    `json:"rounds"`
}
type warmupConfig struct {
	Timezone    string         `json:"timezone"`
	Days        []int          `json:"days"`
	Date        string         `json:"date,omitempty"`
	Windows     []warmupWindow `json:"windows"`
	AccountIDs  []int64        `json:"account_ids"`
	GroupIDs    []int64        `json:"group_ids"`
	ExcludeIDs  []int64        `json:"exclude_ids"`
	Search      string         `json:"search"`
	Plans       []string       `json:"plans"`
	Model       string         `json:"model"`
	Count       int            `json:"count"`
	MessageGap  int            `json:"message_gap"`
	AccountGap  int            `json:"account_gap"`
	Timeout     int            `json:"timeout"`
	DailyMax    int            `json:"daily_max"`
	SkipActive  bool           `json:"skip_active"`
	IdleMinutes int            `json:"idle_minutes"`
}
type warmupRule struct {
	ID       int64        `json:"id"`
	Name     string       `json:"name"`
	Enabled  bool         `json:"enabled"`
	Priority int          `json:"priority"`
	Version  int64        `json:"version"`
	Config   warmupConfig `json:"config"`
}
type warmupSettings struct {
	Enabled bool `json:"enabled"`
}
type warmupOverride struct {
	Mode   string `json:"mode"`
	RuleID *int64 `json:"rule_id"`
}

func validateWarmupRule(r *warmupRule) error {
	c := &r.Config
	if strings.TrimSpace(r.Name) == "" || len([]rune(r.Name)) > 80 {
		return badRequest("规则名称请填写 1 至 80 字。")
	}
	if _, err := time.LoadLocation(c.Timezone); err != nil {
		return badRequest("请选择有效时区。")
	}
	if c.Count < 1 || c.Count > 5 || c.MessageGap < 5 || c.MessageGap > 300 || c.AccountGap < 5 || c.AccountGap > 3600 || c.Timeout < 5 || c.Timeout > 120 || c.DailyMax < 1 || c.DailyMax > 20 || c.IdleMinutes < 0 || c.IdleMinutes > 1440 {
		return badRequest("请检查次数、间隔、超时或每日上限。")
	}
	if !strings.HasPrefix(c.Model, "claude-") || len(c.Model) > 120 {
		return badRequest("请选择 Claude 模型。")
	}
	if len(c.Days) == 0 || len(c.Days) > 7 || len(c.Windows) == 0 || len(c.Windows) > 8 || len(c.AccountIDs) > 5000 || len(c.GroupIDs) > 500 || len(c.Plans) > 20 {
		return badRequest("请选择执行星期、时段和账号范围。")
	}
	for _, d := range c.Days {
		if d < 0 || d > 6 {
			return badRequest("执行星期无效。")
		}
	}
	if c.Date != "" {
		if _, e := time.Parse("2006-01-02", c.Date); e != nil {
			return badRequest("一次性日期无效。")
		}
	}
	occupied := [1440]bool{}
	for _, w := range c.Windows {
		a, e := time.Parse("15:04", w.Start)
		b, f := time.Parse("15:04", w.End)
		if e != nil || f != nil || a.Equal(b) || w.Rounds < 1 || w.Rounds > 6 {
			return badRequest("请设置不同的起止时间，每段 1 至 6 轮。")
		}
		start, end := a.Hour()*60+a.Minute(), b.Hour()*60+b.Minute()
		if end <= start {
			end += 1440
		}
		if end-start < w.Rounds {
			return badRequest("时段长度不足以安排所选轮数。")
		}
		for minute := start; minute < end; minute++ {
			if occupied[minute%1440] {
				return badRequest("执行时段不能重叠。")
			}
			occupied[minute%1440] = true
		}
	}
	return nil
}
func warmupOccurrences(c warmupConfig, now time.Time) []struct {
	Key        string
	Start, End time.Time
} {
	out := []struct {
		Key        string
		Start, End time.Time
	}{}
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return out
	}
	now = now.In(loc)
	for _, dayOffset := range []int{-1, 0} {
		day := time.Date(now.Year(), now.Month(), now.Day()+dayOffset, 0, 0, 0, 0, loc)
		allowed := false
		for _, d := range c.Days {
			if int(day.Weekday()) == d {
				allowed = true
			}
		}
		if !allowed || (c.Date != "" && day.Format("2006-01-02") != c.Date) {
			continue
		}
		for wi, w := range c.Windows {
			start, _ := time.ParseInLocation("2006-01-02 15:04", day.Format("2006-01-02")+" "+w.Start, loc)
			end, _ := time.ParseInLocation("2006-01-02 15:04", day.Format("2006-01-02")+" "+w.End, loc)
			if !end.After(start) {
				end = end.AddDate(0, 0, 1)
			}
			rounds := w.Rounds
			if rounds < 1 {
				continue
			}
			span := end.Sub(start) / time.Duration(rounds)
			for i := 0; i < rounds; i++ {
				a, b := start.Add(time.Duration(i)*span), start.Add(time.Duration(i+1)*span)
				// Only instantiate at the start minute. Restarting later never backfills.
				if !now.Before(a) && now.Before(a.Add(time.Minute)) {
					out = append(out, struct {
						Key        string
						Start, End time.Time
					}{fmt.Sprintf("%s/%d/%d", day.Format("2006-01-02"), wi, i), a, b})
				}
			}
		}
	}
	return out
}
func warmupMatches(r warmupRule, a service.Account) bool {
	c := r.Config
	has := func(ids []int64, id int64) bool {
		for _, x := range ids {
			if x == id {
				return true
			}
		}
		return false
	}
	if has(c.ExcludeIDs, a.ID) {
		return false
	}
	if len(c.AccountIDs) > 0 && !has(c.AccountIDs, a.ID) {
		return false
	}
	if len(c.GroupIDs) > 0 {
		match := false
		for _, id := range a.GroupIDs {
			if has(c.GroupIDs, id) {
				match = true
			}
		}
		if !match {
			return false
		}
	}
	if c.Search != "" && !strings.Contains(strings.ToLower(a.Name), strings.ToLower(c.Search)) {
		return false
	}
	if len(c.Plans) > 0 {
		match := false
		for _, p := range c.Plans {
			if strings.EqualFold(p, a.GetCredential("plan_type")) {
				match = true
			}
		}
		if !match {
			return false
		}
	}
	return true
}
func effectiveWarmup(rules []warmupRule, a service.Account, o warmupOverride) *warmupRule {
	if o.Mode == "disabled" {
		return nil
	}
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		if o.Mode == "rule" {
			if o.RuleID != nil && r.ID == *o.RuleID {
				return &r
			}
			continue
		}
		if warmupMatches(r, a) {
			return &r
		}
	}
	return nil
}
func (h *Handler) warmupRules(ctx context.Context) ([]warmupRule, error) {
	rows, err := h.db.QueryContext(ctx, "SELECT id,name,enabled,priority,version,config FROM workspace_warmup_rules ORDER BY priority DESC,id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []warmupRule{}
	for rows.Next() {
		var r warmupRule
		var b []byte
		if err = rows.Scan(&r.ID, &r.Name, &r.Enabled, &r.Priority, &r.Version, &b); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(b, &r.Config); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	specificity := func(c warmupConfig) int {
		if len(c.AccountIDs) > 0 {
			return 2
		}
		if len(c.GroupIDs) > 0 {
			return 1
		}
		return 0
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority > out[j].Priority
		}
		return specificity(out[i].Config) > specificity(out[j].Config)
	})
	return out, nil
}
func (h *Handler) warmupAccounts(ctx context.Context) ([]service.Account, error) {
	all := []service.Account{}
	for page := 1; ; page++ {
		items, total, err := h.admin.ListAccounts(ctx, page, 100, "anthropic", "oauth", "", "", 0, "", "id", "asc")
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if int64(len(all)) >= total {
			break
		}
		if len(items) == 0 {
			return nil, fmt.Errorf("账号列表不完整")
		}
	}
	out := []service.Account{}
	for _, a := range all {
		if a.ParentAccountID == nil && !a.IsSyntheticUITest() {
			out = append(out, a)
		}
	}
	return out, nil
}
func (h *Handler) warmupOverrides(ctx context.Context) (map[int64]warmupOverride, error) {
	rows, err := h.db.QueryContext(ctx, "SELECT account_id,mode,rule_id FROM workspace_warmup_overrides")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]warmupOverride{}
	for rows.Next() {
		var id int64
		var o warmupOverride
		if err = rows.Scan(&id, &o.Mode, &o.RuleID); err != nil {
			return nil, err
		}
		out[id] = o
	}
	return out, rows.Err()
}
func (h *Handler) getWarmup(c *gin.Context) {
	ctx := c.Request.Context()
	rules, err := h.warmupRules(ctx)
	if err != nil {
		fail(c, err)
		return
	}
	accounts, err := h.warmupAccounts(ctx)
	if err != nil {
		fail(c, err)
		return
	}
	overrides, err := h.warmupOverrides(ctx)
	if err != nil {
		fail(c, err)
		return
	}
	var settings warmupSettings
	if err = h.getRuntime(ctx, "account_warmup_settings", &settings); err != nil {
		fail(c, err)
		return
	}
	items := []gin.H{}
	for _, a := range accounts {
		r := effectiveWarmup(rules, a, overrides[a.ID])
		name := "未匹配规则"
		var ruleID int64
		if r != nil {
			name = r.Name
			ruleID = r.ID
		}
		items = append(items, gin.H{"id": a.ID, "name": a.Name, "status": a.Status, "schedulable": a.Schedulable, "plan": a.GetCredential("plan_type"), "group_ids": a.GroupIDs, "rate_limited": a.IsRateLimited(), "window_end": a.SessionWindowEnd, "last_used_at": a.LastUsedAt, "override": overrides[a.ID], "rule_id": ruleID, "rule_name": name})
	}
	tz := "Asia/Shanghai"
	if h.cfg != nil && h.cfg.Timezone != "" {
		tz = h.cfg.Timezone
	}
	groups, e := h.admin.GetAllGroupsByPlatform(ctx, "anthropic")
	if e != nil {
		fail(c, e)
		return
	}
	groupItems := []gin.H{}
	for _, g := range groups {
		groupItems = append(groupItems, gin.H{"id": g.ID, "name": g.Name})
	}
	response.Success(c, gin.H{"settings": settings, "rules": rules, "accounts": items, "timezone": tz, "models": claude.DefaultModels, "groups": groupItems})
}
func (h *Handler) saveWarmupSettings(c *gin.Context) {
	var p warmupSettings
	if err := body(c, &p, 1024); err != nil {
		fail(c, err)
		return
	}
	if err := h.setRuntime(c.Request.Context(), "account_warmup_settings", p); err != nil {
		fail(c, err)
		return
	}
	if !p.Enabled {
		h.stopWarmupRun()
		_, _ = h.db.ExecContext(c.Request.Context(), "UPDATE workspace_warmup_runs SET status='cancelled',reason='总开关已关闭',finished_at=NOW() WHERE status IN ('queued','checking','running')")
	}
	response.Success(c, p)
}
func (h *Handler) saveWarmupRule(c *gin.Context) {
	var p warmupRule
	if err := body(c, &p, 128<<10); err != nil {
		fail(c, err)
		return
	}
	if err := validateWarmupRule(&p); err != nil {
		fail(c, err)
		return
	}
	ctx := c.Request.Context()
	raw, _ := json.Marshal(p.Config)
	if c.Param("id") == "" {
		if err := h.db.QueryRowContext(ctx, "INSERT INTO workspace_warmup_rules(name,enabled,priority,config) VALUES($1,FALSE,$2,$3) RETURNING id,version", p.Name, p.Priority, raw).Scan(&p.ID, &p.Version); err != nil {
			fail(c, err)
			return
		}
		p.Enabled = false
	} else {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			fail(c, badRequest("规则编号无效。"))
			return
		}
		res, err := h.db.ExecContext(ctx, "UPDATE workspace_warmup_rules SET name=$2,enabled=$3,priority=$4,config=$5,version=version+1,updated_at=NOW() WHERE id=$1 AND version=$6", id, p.Name, p.Enabled, p.Priority, raw, p.Version)
		if err != nil {
			fail(c, err)
			return
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			fail(c, &requestError{409, "规则已变化，请刷新。"})
			return
		}
		h.stopWarmupRun()
		_, _ = h.db.ExecContext(ctx, "UPDATE workspace_warmup_runs SET status='cancelled',reason='规则已修改',finished_at=NOW() WHERE rule_id=$1 AND status IN ('queued','checking','running')", id)
	}
	response.Success(c, p)
}
func (h *Handler) deleteWarmupRule(c *gin.Context) {
	id, err := positiveID(c)
	if err != nil {
		fail(c, err)
		return
	}
	h.stopWarmupRun()
	_, err = h.db.ExecContext(c.Request.Context(), "UPDATE workspace_warmup_runs SET status='cancelled',reason='规则已删除',finished_at=NOW() WHERE rule_id=$1 AND status IN ('queued','checking','running')", id)
	if err == nil {
		_, err = h.db.ExecContext(c.Request.Context(), "DELETE FROM workspace_warmup_rules WHERE id=$1", id)
	}
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true})
}
func (h *Handler) previewWarmupRule(c *gin.Context) {
	rules, err := h.warmupRules(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	accounts, err := h.warmupAccounts(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	items := []gin.H{}
	for _, r := range rules {
		if r.ID == id {
			next := []string{}
			now := time.Now().Truncate(time.Minute)
			for i := 0; i < 8*24*60 && len(next) < 8; i++ {
				for _, o := range warmupOccurrences(r.Config, now.Add(time.Duration(i)*time.Minute)) {
					next = append(next, o.Start.Format(time.RFC3339))
				}
			}
			for _, a := range accounts {
				if warmupMatches(r, a) {
					items = append(items, gin.H{"id": a.ID, "name": a.Name, "rate_limited": a.IsRateLimited(), "schedulable": a.IsSchedulable()})
				}
			}
			response.Success(c, gin.H{"accounts": items, "next": next, "max_requests": len(items) * r.Config.Count, "quota_check": "执行前逐个串行主动查询；429 或任一五小时/周用量 >99% 跳过"})
			return
		}
	}
	fail(c, notFound())
}
func (h *Handler) queueWarmupRule(c *gin.Context) {
	var p struct {
		RequestID string `json:"request_id"`
		Confirm   bool   `json:"confirm"`
	}
	if err := body(c, &p, 2048); err != nil {
		fail(c, err)
		return
	}
	if _, err := uuid.Parse(p.RequestID); err != nil || !p.Confirm {
		fail(c, badRequest("请预览并确认实际请求。"))
		return
	}
	var settings warmupSettings
	if err := h.getRuntime(c.Request.Context(), "account_warmup_settings", &settings); err != nil || !settings.Enabled {
		fail(c, badRequest("请先开启热身总开关。"))
		return
	}
	rules, err := h.warmupRules(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	accounts, err := h.warmupAccounts(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	overrides, err := h.warmupOverrides(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	count := int64(0)
	for _, r := range rules {
		if r.ID == id && r.Enabled {
			for _, a := range accounts {
				effective := effectiveWarmup(rules, a, overrides[a.ID])
				if effective == nil || effective.ID != id {
					continue
				}
				res, e := h.db.ExecContext(c.Request.Context(), "INSERT INTO workspace_warmup_runs(rule_id,account_id,occurrence,planned_at,deadline) VALUES($1,$2,$3,NOW(),NOW()+INTERVAL '30 minutes') ON CONFLICT DO NOTHING", id, a.ID, "manual/"+p.RequestID)
				if e != nil {
					fail(c, e)
					return
				}
				n, _ := res.RowsAffected()
				count += n
			}
			response.Success(c, gin.H{"queued": count})
			return
		}
	}
	fail(c, badRequest("请先启用该规则。"))
}
func (h *Handler) saveWarmupOverrides(c *gin.Context) {
	var p struct {
		IDs    []int64 `json:"ids"`
		Mode   string  `json:"mode"`
		RuleID *int64  `json:"rule_id"`
	}
	if err := body(c, &p, 64<<10); err != nil {
		fail(c, err)
		return
	}
	if len(p.IDs) == 0 || len(p.IDs) > 500 || (p.Mode != "inherit" && p.Mode != "disabled" && p.Mode != "rule") || (p.Mode == "rule" && p.RuleID == nil) {
		fail(c, badRequest("请选择账号和有效策略。"))
		return
	}
	tx, err := h.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		fail(c, err)
		return
	}
	defer tx.Rollback()
	if p.Mode != "rule" {
		p.RuleID = nil
	}
	for _, id := range p.IDs {
		res, e := tx.ExecContext(c.Request.Context(), "INSERT INTO workspace_warmup_overrides(account_id,mode,rule_id) SELECT id,$2,$3 FROM accounts WHERE id=$1 AND platform='anthropic' AND type='oauth' AND deleted_at IS NULL ON CONFLICT(account_id) DO UPDATE SET mode=EXCLUDED.mode,rule_id=EXCLUDED.rule_id", id, p.Mode, p.RuleID)
		if e != nil {
			fail(c, e)
			return
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			fail(c, badRequest("账号不适用或已删除。"))
			return
		}
	}
	for _, id := range p.IDs {
		if _, err = tx.ExecContext(c.Request.Context(), "UPDATE workspace_warmup_runs SET status='cancelled',reason='账号策略已修改',finished_at=NOW() WHERE account_id=$1 AND status IN ('queued','checking','running')", id); err != nil {
			fail(c, err)
			return
		}
	}
	if err = tx.Commit(); err != nil {
		fail(c, err)
		return
	}
	h.stopWarmupRun()
	response.Success(c, gin.H{"saved": len(p.IDs)})
}
func (h *Handler) cancelWarmupRuns(c *gin.Context) {
	h.stopWarmupRun()
	_, err := h.db.ExecContext(c.Request.Context(), "UPDATE workspace_warmup_runs SET status='cancelled',reason='管理员取消',finished_at=NOW() WHERE status IN ('queued','checking','running')")
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"cancelled": true})
}
func (h *Handler) listWarmupRuns(c *gin.Context) {
	page, size := pageNumber(c.Query("page"), 1, 100000), pageNumber(c.Query("page_size"), 30, 100)
	ctx := c.Request.Context()
	var total int
	if err := h.db.QueryRowContext(ctx, "SELECT count(*) FROM workspace_warmup_runs").Scan(&total); err != nil {
		fail(c, err)
		return
	}
	rows, err := h.db.QueryContext(ctx, "SELECT r.id,r.account_id,a.name,r.rule_id,COALESCE(w.name,'规则已删除'),r.status,r.reason,r.planned_at,r.attempts,r.input_tokens,r.output_tokens,r.snapshot,r.updated_at FROM workspace_warmup_runs r JOIN accounts a ON a.id=r.account_id LEFT JOIN workspace_warmup_rules w ON w.id=r.rule_id ORDER BY r.id DESC LIMIT $1 OFFSET $2", size, (page-1)*size)
	if err != nil {
		fail(c, err)
		return
	}
	defer rows.Close()
	items := []gin.H{}
	for rows.Next() {
		var id, aid, attempts, input, output int64
		var rid *int64
		var name, rule, status, reason string
		var planned, updated time.Time
		var raw []byte
		if err = rows.Scan(&id, &aid, &name, &rid, &rule, &status, &reason, &planned, &attempts, &input, &output, &raw, &updated); err != nil {
			fail(c, err)
			return
		}
		var snapshot any
		_ = json.Unmarshal(raw, &snapshot)
		items = append(items, gin.H{"id": id, "account_id": aid, "account_name": name, "rule_name": rule, "status": status, "reason": reason, "planned_at": planned, "attempts": attempts, "input_tokens": input, "output_tokens": output, "snapshot": snapshot, "updated_at": updated})
	}
	if err = rows.Err(); err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"items": items, "total": total})
}
