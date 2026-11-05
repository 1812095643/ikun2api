package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/timezone"
)

// 号池用量窗口标识。
const (
	poolWindowFiveHour = "5h"
	poolWindowSevenDay = "7d"
)

// PoolQuotaWindow 是号池单个用量窗口（5 小时 / 7 天）的读数。
//
// Utilization 尽量取真数，优先级：
//  1. upstream —— 账号 extra 里被动采样的上游真实利用率（Claude 的 7d 窗口有这项，
//     是上游响应的 utilization，最可信）；
//  2. quota    —— 账号上配置的窗口限额换算（window_cost_limit / quota_weekly_limit）；
//  3. local    —— 两者都没有，只有本地实测用量，Utilization 保持 0，
//     前端只显示数字不画进度条，绝不编一个百分比出来。
//
// 这里刻意不主动探上游（不触网）：号池页要快，也不能让上游超时把列表拖死。
type PoolQuotaWindow struct {
	Requests         int64   `json:"requests"`
	Tokens           int64   `json:"tokens"`
	Cost             float64 `json:"cost"`
	Utilization      float64 `json:"utilization"`
	ResetsAt         *string `json:"resets_at,omitempty"`
	RemainingSeconds int     `json:"remaining_seconds,omitempty"`
	Source           string  `json:"source"`
}

// PoolCapacity 是号池的容量视图：并发槽位 + 管理员配置的各类限额。
// 全部字段都来自账号表现有列与 extra，不新增存储。
type PoolCapacity struct {
	InUse            int     `json:"in_use"`
	Concurrency      int     `json:"concurrency"`
	LoadFactor       int     `json:"load_factor"`
	WindowCostUsed   float64 `json:"window_cost_used,omitempty"`
	WindowCostLimit  float64 `json:"window_cost_limit,omitempty"`
	QuotaUsed        float64 `json:"quota_used,omitempty"`
	QuotaLimit       float64 `json:"quota_limit,omitempty"`
	QuotaDailyUsed   float64 `json:"quota_daily_used,omitempty"`
	QuotaDailyLimit  float64 `json:"quota_daily_limit,omitempty"`
	QuotaWeeklyUsed  float64 `json:"quota_weekly_used,omitempty"`
	QuotaWeeklyLimit float64 `json:"quota_weekly_limit,omitempty"`

	// SupportsQuota 表示该账号类型真的会执行 quota_* 限额（api_key / bedrock / service_account）。
	// 其他类型即使配了限额也不会被调度层拦截，所以前端不展示这些徽标 ——
	// 显示了就会出现「额度满了还能请求」的误解。
	SupportsQuota bool `json:"supports_quota,omitempty"`
	// SupportsWindowCost 表示该账号类型真的会执行 5h 窗口费用限额（Anthropic oauth / setup_token）。
	SupportsWindowCost bool `json:"supports_window_cost,omitempty"`

	// QuotaWarning 标记「限额已经满了但账号仍在被调度」的不一致状态。
	// 这是号池最容易踩的坑：后台看着额度爆了，请求还在往这个号上落。
	// 只在可调度账号上出现，空字符串表示没有异常。
	QuotaWarning string `json:"quota_warning,omitempty"`
}

// PoolUserCapacity 是普通用户看到的容量：只有并发槽位，不含任何限额配置。
type PoolUserCapacity struct {
	InUse       int `json:"in_use"`
	Concurrency int `json:"concurrency"`
}

// PoolUserQuotaWindow 是普通用户看到的用量窗口：只有进度条读数，没有绝对额度数字。
type PoolUserQuotaWindow struct {
	Utilization      float64 `json:"utilization"`
	ResetsAt         *string `json:"resets_at,omitempty"`
	RemainingSeconds int     `json:"remaining_seconds,omitempty"`
	Enabled          bool    `json:"enabled"`
}

// poolAnyFloat 把 extra 里任意数值形态转成 float64。
func poolAnyFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case nil:
		return 0, false
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f, err == nil
	case bool:
		if n {
			return 1, true
		}
		return 0, false
	}
	return 0, false
}

func poolExtraFloat(extra map[string]any, key string) float64 {
	if extra == nil {
		return 0
	}
	f, _ := poolAnyFloat(extra[key])
	return f
}

// poolExtraPercent 读百分比型 extra。库里存的是 0-1 的比例（例如 0.87），
// 这里统一换算成 0-100；已经大于 1 的值按百分数原样用。
func poolExtraPercent(extra map[string]any, key string) (float64, bool) {
	if extra == nil {
		return 0, false
	}
	raw, ok := extra[key]
	if !ok || raw == nil {
		return 0, false
	}
	f, valid := poolAnyFloat(raw)
	if !valid || math.IsNaN(f) || math.IsInf(f, 0) || f < 0 {
		return 0, false
	}
	if f <= 1 {
		f *= 100
	}
	return min(f, 100), true
}

func poolExtraTime(extra map[string]any, key string) *time.Time {
	if extra == nil {
		return nil
	}
	raw, ok := extra[key]
	if !ok || raw == nil {
		return nil
	}
	if f, valid := poolAnyFloat(raw); valid {
		if f <= 0 {
			return nil
		}
		// 兼容毫秒时间戳。
		if f > 1e11 {
			f /= 1000
		}
		t := time.Unix(int64(f), 0)
		return &t
	}
	if s, isStr := raw.(string); isStr {
		s = strings.TrimSpace(s)
		if s == "" {
			return nil
		}
		if parsed, err := time.Parse(time.RFC3339, s); err == nil {
			return &parsed
		}
		if unix, err := strconv.ParseInt(s, 10, 64); err == nil && unix > 0 {
			t := time.Unix(unix, 0)
			return &t
		}
	}
	return nil
}

func poolPercent(used, limit float64) float64 {
	if limit <= 0 {
		return 0
	}
	return used / limit * 100
}

// poolCapacityFromAccount 从账号表现有字段与 extra 拼出容量视图。
func poolCapacityFromAccount(acc *Account) PoolCapacity {
	capacity := PoolCapacity{}
	if acc == nil {
		return capacity
	}
	capacity.Concurrency = acc.Concurrency
	capacity.LoadFactor = poolLoadFactor(acc)
	capacity.WindowCostLimit = poolExtraFloat(acc.Extra, "window_cost_limit")
	capacity.QuotaLimit = poolExtraFloat(acc.Extra, "quota_limit")
	capacity.QuotaUsed = poolExtraFloat(acc.Extra, "quota_used")
	capacity.QuotaDailyLimit = poolExtraFloat(acc.Extra, "quota_daily_limit")
	capacity.QuotaDailyUsed = poolExtraFloat(acc.Extra, "quota_daily_used")
	capacity.QuotaWeeklyLimit = poolExtraFloat(acc.Extra, "quota_weekly_limit")
	capacity.QuotaWeeklyUsed = poolExtraFloat(acc.Extra, "quota_weekly_used")
	capacity.SupportsQuota = acc.SupportsAccountQuota()
	capacity.SupportsWindowCost = acc.IsAnthropicOAuthOrSetupToken()
	return capacity
}

// poolQuotaWindowFromAccount 按窗口类型拼出额度读数。
func poolQuotaWindowFromAccount(acc *Account, kind string, capacity PoolCapacity) PoolQuotaWindow {
	window := PoolQuotaWindow{Source: "local"}
	if acc == nil {
		return window
	}
	now := timezone.Now()

	if kind == poolWindowFiveHour {
		// 5 小时窗口优先用上游响应头被动采样到的真实利用率
		// （session_window_utilization，库里存 0-1 小数），这是 Claude Max
		// 真正的 5h 额度读数，和官方账号用量面板同源。
		if pct, ok := poolExtraPercent(acc.Extra, "session_window_utilization"); ok {
			window.Utilization = pct
			window.Source = "upstream"
		} else {
			// 没有采样值时按窗口状态回退，与 estimateSetupTokenUsage 的估算口径一致。
			switch strings.ToLower(strings.TrimSpace(acc.SessionWindowStatus)) {
			case "rejected":
				window.Utilization = 100
				window.Source = "status"
			case "allowed_warning":
				window.Utilization = 80
				window.Source = "status"
			}
		}
		// 两者都没有时退到管理员配置的 5h 窗口费用限额（仅 Anthropic oauth 生效）。
		if window.Source == "local" && capacity.SupportsWindowCost && capacity.WindowCostLimit > 0 {
			window.Source = "quota"
		}
		switch {
		case acc.SessionWindowEnd != nil && acc.SessionWindowEnd.After(now):
			window.ResetsAt = poolFormatTimePtr(acc.SessionWindowEnd)
			window.RemainingSeconds = int(acc.SessionWindowEnd.Sub(now).Seconds())
		case acc.RateLimitResetAt != nil && acc.RateLimitResetAt.After(now):
			window.ResetsAt = poolFormatTimePtr(acc.RateLimitResetAt)
			window.RemainingSeconds = int(acc.RateLimitResetAt.Sub(now).Seconds())
		}
		// 窗口已经过期 → 额度确实重置了，读数和重置点一起清零，
		// 避免页面出现「100% 但重置时间已过去」这种自相矛盾的状态。
		if acc.SessionWindowEnd != nil && !now.Before(*acc.SessionWindowEnd) {
			window.Utilization = 0
			window.ResetsAt = nil
			window.RemainingSeconds = 0
		}
		return window
	}

	// 7 天窗口：优先上游被动采样的真实利用率。
	if pct, ok := poolExtraPercent(acc.Extra, "passive_usage_7d_utilization"); ok {
		window.Utilization = pct
		window.Source = "upstream"
	} else if capacity.SupportsQuota && capacity.QuotaWeeklyLimit > 0 {
		window.Utilization = poolPercent(capacity.QuotaWeeklyUsed, capacity.QuotaWeeklyLimit)
		window.Source = "quota"
	}
	if reset := poolExtraTime(acc.Extra, "passive_usage_7d_reset"); reset != nil {
		if reset.After(now) {
			window.ResetsAt = poolFormatTimePtr(reset)
			window.RemainingSeconds = int(reset.Sub(now).Seconds())
		} else if window.Source == "upstream" {
			window.Utilization = 0
		}
	}
	return window
}

// fillCapacity 把并发占用（Redis 实时值）回填进容量视图。
// 必须在 fillConcurrency 之后调用。
func (s *AccountPoolService) fillCapacity(_ context.Context, items []PoolAccount) {
	for i := range items {
		items[i].Capacity.InUse = items[i].InUse
	}
}

// accountStatsReader 取 repo 的批量窗口统计能力（可选能力，类型断言）。
func (s *AccountPoolService) accountStatsReader() (accountWindowStatsBatchReader, bool) {
	if s.usageLogRepo == nil {
		return nil, false
	}
	reader, ok := s.usageLogRepo.(accountWindowStatsBatchReader)
	return reader, ok
}

// fillWindowStats 用 usage_logs 补齐 5h / 7d 窗口的本地实测用量。
// 读取失败向页面报告网络异常，不把失败伪装成零值。
func (s *AccountPoolService) fillWindowStats(ctx context.Context, items []PoolAccount, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	reader, ok := s.accountStatsReader()
	if !ok {
		return nil
	}
	now := timezone.Now()

	stats, err := reader.GetAccountWindowStatsBatch(ctx, ids, now.Add(-5*time.Hour))
	if err != nil {
		return poolDataUnavailable(err)
	}
	{
		for i := range items {
			st := stats[items[i].ID]
			if st == nil {
				continue
			}
			items[i].FiveHour.Requests = st.Requests
			items[i].FiveHour.Tokens = st.Tokens
			items[i].FiveHour.Cost = st.Cost
			items[i].Capacity.WindowCostUsed = st.Cost
			if items[i].FiveHour.Source == "quota" {
				items[i].FiveHour.Utilization = poolPercent(st.Cost, items[i].Capacity.WindowCostLimit)
			}
		}
	}

	stats, err = reader.GetAccountWindowStatsBatch(ctx, ids, now.AddDate(0, 0, -7))
	if err != nil {
		return poolDataUnavailable(err)
	}
	{
		for i := range items {
			st := stats[items[i].ID]
			if st == nil {
				continue
			}
			items[i].SevenDay.Requests = st.Requests
			items[i].SevenDay.Tokens = st.Tokens
			items[i].SevenDay.Cost = st.Cost
		}
	}
	return nil
}

// fillUserCapacity 给普通用户视角补并发占用（Redis 实时值）；读取失败返回错误。
func (s *AccountPoolService) fillUserCapacity(ctx context.Context, items []PoolUserAccount, ids []int64) error {
	if len(ids) == 0 || s.concurrencyService == nil {
		return nil
	}
	counts, err := s.concurrencyService.GetAccountConcurrencyBatch(ctx, ids)
	if err != nil {
		return poolDataUnavailable(err)
	}
	for i := range items {
		items[i].Capacity.InUse = counts[items[i].ID]
	}
	return nil
}

// poolUserCapacityFrom 裁剪出用户可见的容量。
func poolUserCapacityFrom(capacity PoolCapacity) PoolUserCapacity {
	return PoolUserCapacity{InUse: capacity.InUse, Concurrency: capacity.Concurrency}
}

// poolUserQuotaWindowFrom 裁剪出用户可见的窗口读数：只留进度条，不给额度数字。
func poolUserQuotaWindowFrom(window PoolQuotaWindow) PoolUserQuotaWindow {
	return PoolUserQuotaWindow{
		Utilization:      window.Utilization,
		ResetsAt:         window.ResetsAt,
		RemainingSeconds: window.RemainingSeconds,
		Enabled:          window.Utilization > 0,
	}
}

// ==================== 账号名脱敏与伪名 ====================

// poolPseudoRotationSeconds 是 api_key 类账号伪名的轮换周期（7 天）。
const poolPseudoRotationSeconds int64 = 7 * 24 * 3600

// poolPseudoNameSalt 让伪名在换机器/换库后依然稳定，但不至于被反推。
const poolPseudoNameSalt = "ikun2api-pool-pseudo-v1"

var poolFakeLocalParts = []string{
	"csd", "amy", "brian", "cindy", "derek", "ellen", "frank", "grace",
	"henry", "ivy", "jason", "kelly", "leo", "mona", "nina", "oscar",
	"paul", "queen", "roy", "sara", "tony", "uma", "victor", "wendy",
	"xena", "yuri", "zoe", "alan", "bella", "carl", "diana", "eric",
}

var poolFakeDomains = []string{
	"@outlook.com", "@hotmail.com", "@outlook.jp", "@live.com", "@outlook.com",
}

// PoolMaskAccountName uses the same original-name masking as user usage records.
func PoolMaskAccountName(name string) string {
	return MaskAccountName(name)
}

// poolPseudoAccountName 为 api_key 类账号生成稳定的伪账号名。
//
// 同一个 (userID, accountID) 在一个轮换周期内恒定，跨周期自动换一个——
// 满足「一段时间内的请求都落在同一个账号」「什么时候都不会变」「可以隔段时间换」。
func poolPseudoAccountName(userID, accountID int64, at time.Time) string {
	bucket := at.Unix() / poolPseudoRotationSeconds
	seed := fmt.Sprintf("%d:%d:%d", userID, accountID, bucket)
	sum := sha256.Sum256([]byte(poolPseudoNameSalt + "|" + seed))
	local := poolFakeLocalParts[int(sum[0])%len(poolFakeLocalParts)]
	domain := poolFakeDomains[int(sum[1])%len(poolFakeDomains)]
	return local + strings.Repeat("*", 6) + domain
}

// poolDisplayAccountNameForUser keeps original names partially visible for all
// account types. Only missing names use a stable anonymous fallback.
func poolDisplayAccountNameForUser(acc *Account, userID int64, at time.Time) string {
	if acc == nil {
		return ""
	}
	if name := MaskAccountName(acc.Name); name != "" {
		return name
	}
	return poolPseudoAccountName(userID, acc.ID, at)
}

// PoolUsageAccountNames 批量计算用户端使用记录里展示的账号名。
//
// 规则与账号池列表完全一致：
//   - 所有现存账号 → 原始名称中间以三个星号遮挡；
//   - 账号名称缺失 → 同 (user, account) 稳定伪名。
//
// 账号已被删除但历史快照还在时，用伪名兜底 —— 绝不下发真实快照名。
func (s *AccountPoolService) PoolUsageAccountNames(ctx context.Context, userID int64, accountIDs []int64) map[int64]string {
	names := make(map[int64]string, len(accountIDs))
	if len(accountIDs) == 0 {
		return names
	}
	now := timezone.Now()
	byID := make(map[int64]*Account, len(accountIDs))
	if s.adminService != nil {
		if accounts, err := s.adminService.GetAccountsByIDs(ctx, accountIDs); err == nil {
			for _, acc := range accounts {
				if acc != nil {
					byID[acc.ID] = acc
				}
			}
		}
	}
	for _, id := range accountIDs {
		if id <= 0 {
			continue
		}
		if _, done := names[id]; done {
			continue
		}
		if acc, ok := byID[id]; ok {
			names[id] = poolDisplayAccountNameForUser(acc, userID, now)
			continue
		}
		names[id] = poolPseudoAccountName(userID, id, now)
	}
	return names
}

// poolQuotaWarning 检出「限额已满但账号仍可被调度」的不一致状态。
//
// 号池最危险的 bug 不是算错额度，而是额度已经满了、调度层却还在往这个号上发请求：
// 管理员看到的是 100%，用户看到的却是照常可用，等上游真的 429 才发现。
// 这里只做只读体检，返回的键由前端翻译成人话，不改任何状态。
func poolQuotaWarning(acc *Account, item PoolAccount) string {
	if acc == nil || !item.Schedulable {
		return ""
	}
	// 1. quota_* 类限额已经用尽（仅 api_key / bedrock / service_account 会真的拦截）。
	if acc.SupportsAccountQuota() && acc.IsQuotaExceeded() {
		return "quota_exceeded_but_schedulable"
	}
	// 2. 5h 窗口费用已达上限（仅 Anthropic oauth / setup_token 会真的拦截）。
	if acc.IsAnthropicOAuthOrSetupToken() {
		if limit := acc.GetWindowCostLimit(); limit > 0 && item.Capacity.WindowCostUsed >= limit {
			return "window_cost_exceeded_but_schedulable"
		}
	}
	// 3. 上游明确拒绝了 5h 窗口。
	if strings.EqualFold(strings.TrimSpace(acc.SessionWindowStatus), "rejected") {
		return "window_rejected_but_schedulable"
	}
	// 4. 上游采样到的 5h 利用率已经打满。
	if pct, ok := poolExtraPercent(acc.Extra, "session_window_utilization"); ok && pct >= 100 {
		return "window_exhausted_but_schedulable"
	}
	return ""
}
