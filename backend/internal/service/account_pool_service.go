package service

import (
	"context"
	"strings"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/timezone"
	"github.com/MACOS-DO/sub4api/internal/pkg/usagestats"
)

// AccountPoolService 提供「实时号池」的只读聚合视图。
//
// 设计边界（刻意保持窄）：
//   - 不新增写入路径，不改动调度、计费与鉴权。
//   - 账号维度统计全部来自 usage_logs，与「使用记录」同源，保证两边可对账。
//   - 不主动触发上游配额探查（不触网），只读库内已有的运行态字段，
//     避免号池页被上游限流或超时拖慢。
type AccountPoolService struct {
	adminService        AdminService
	accountUsageService *AccountUsageService
	concurrencyService  *ConcurrencyService
	usageLogRepo        UsageLogRepository
}

// NewAccountPoolService 创建号池聚合服务。
func NewAccountPoolService(
	adminService AdminService,
	accountUsageService *AccountUsageService,
	concurrencyService *ConcurrencyService,
	usageLogRepo UsageLogRepository,
) *AccountPoolService {
	return &AccountPoolService{
		adminService:        adminService,
		accountUsageService: accountUsageService,
		concurrencyService:  concurrencyService,
		usageLogRepo:        usageLogRepo,
	}
}

// PoolWindow 表示一个时间窗口内的用量聚合。
type PoolWindow struct {
	Requests     int64   `json:"requests"`
	Tokens       int64   `json:"tokens"`
	Cost         float64 `json:"cost"`
	StandardCost float64 `json:"standard_cost"`
	UserCost     float64 `json:"user_cost"`
}

// PoolGroupRef 是账号所属分组的展示引用。
type PoolGroupRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// PoolAccount 是号池中的账号视图（仅管理员可见）。
type PoolAccount struct {
	ID       int64   `json:"id"`
	Name     string  `json:"name"`
	Platform string  `json:"platform"`
	Type     string  `json:"type"`
	Status   string  `json:"status"`
	Notes    *string `json:"notes,omitempty"`

	Schedulable    bool    `json:"schedulable"`
	ErrorMessage   string  `json:"error_message,omitempty"`
	LastUsedAt     *string `json:"last_used_at,omitempty"`
	RateMultiplier float64 `json:"rate_multiplier"`
	Priority       int     `json:"priority"`
	Concurrency    int     `json:"concurrency"`
	LoadFactor     int     `json:"load_factor"`

	InUse int `json:"in_use"`

	RateLimitedAt           *string `json:"rate_limited_at,omitempty"`
	RateLimitResetAt        *string `json:"rate_limit_reset_at,omitempty"`
	OverloadUntil           *string `json:"overload_until,omitempty"`
	TempUnschedulableUntil  *string `json:"temp_unschedulable_until,omitempty"`
	TempUnschedulableReason string  `json:"temp_unschedulable_reason,omitempty"`

	SessionWindowStart  *string `json:"session_window_start,omitempty"`
	SessionWindowEnd    *string `json:"session_window_end,omitempty"`
	SessionWindowStatus string  `json:"session_window_status,omitempty"`

	ParentAccountID *int64         `json:"parent_account_id,omitempty"`
	Groups          []PoolGroupRef `json:"groups,omitempty"`

	Today PoolWindow `json:"today"`
}

// PoolOverview 是号池概览。
type PoolOverview struct {
	TotalAccounts    int64            `json:"total_accounts"`
	ActiveAccounts   int64            `json:"active_accounts"`
	ErrorAccounts    int64            `json:"error_accounts"`
	DisabledAccounts int64            `json:"disabled_accounts"`
	SchedulableCount int64            `json:"schedulable_count"`
	PlatformCounts   map[string]int64 `json:"platform_counts"`
	TotalInUse       int              `json:"total_in_use"`
	Today            PoolWindow       `json:"today"`
	Window7d         PoolWindow       `json:"window_7d"`
	GeneratedAt      string           `json:"generated_at"`
}

// PoolAccountList 是号池列表返回值。
type PoolAccountList struct {
	Items    []PoolAccount `json:"items"`
	Total    int64         `json:"total"`
	Page     int           `json:"page"`
	PageSize int           `json:"page_size"`
	Overview PoolOverview  `json:"overview"`
}

// PoolAccountFilter 是号池筛选条件。
type PoolAccountFilter struct {
	Page     int
	PageSize int
	Platform string
	Type     string
	Status   string
	Search   string
	GroupID  int64
}

const (
	poolDefaultPageSize = 20
	poolMaxPageSize     = 200
	poolOverviewBatch   = 200
	poolOverviewPages   = 50
)

func poolFormatTimePtr(t *time.Time) *string {
	if t == nil || t.IsZero() {
		return nil
	}
	s := t.In(timezone.Location()).Format(time.RFC3339)
	return &s
}

func poolRateMultiplier(acc *Account) float64 {
	if acc == nil || acc.RateMultiplier == nil {
		return 1
	}
	return *acc.RateMultiplier
}

func poolLoadFactor(acc *Account) int {
	if acc == nil {
		return 0
	}
	if acc.LoadFactor != nil {
		return *acc.LoadFactor
	}
	return acc.Concurrency
}

// ListAccounts 返回号池账号列表（管理员口径）。
func (s *AccountPoolService) ListAccounts(ctx context.Context, filter PoolAccountFilter) (*PoolAccountList, error) {
	page := filter.Page
	if page <= 0 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize <= 0 {
		pageSize = poolDefaultPageSize
	}
	if pageSize > poolMaxPageSize {
		pageSize = poolMaxPageSize
	}

	accounts, total, err := s.adminService.ListAccounts(
		ctx, page, pageSize,
		strings.TrimSpace(filter.Platform), strings.TrimSpace(filter.Type),
		strings.TrimSpace(filter.Status), strings.TrimSpace(filter.Search),
		filter.GroupID, "", "id", "asc",
	)
	if err != nil {
		return nil, err
	}

	items := make([]PoolAccount, 0, len(accounts))
	ids := make([]int64, 0, len(accounts))
	for i := range accounts {
		acc := &accounts[i]
		item := PoolAccount{
			ID:                      acc.ID,
			Name:                    acc.Name,
			Platform:                acc.Platform,
			Type:                    acc.Type,
			Status:                  acc.Status,
			Notes:                   acc.Notes,
			Schedulable:             acc.Schedulable,
			ErrorMessage:            acc.ErrorMessage,
			LastUsedAt:              poolFormatTimePtr(acc.LastUsedAt),
			RateMultiplier:          poolRateMultiplier(acc),
			Priority:                acc.Priority,
			Concurrency:             acc.Concurrency,
			LoadFactor:              poolLoadFactor(acc),
			RateLimitedAt:           poolFormatTimePtr(acc.RateLimitedAt),
			RateLimitResetAt:        poolFormatTimePtr(acc.RateLimitResetAt),
			OverloadUntil:           poolFormatTimePtr(acc.OverloadUntil),
			TempUnschedulableUntil:  poolFormatTimePtr(acc.TempUnschedulableUntil),
			TempUnschedulableReason: acc.TempUnschedulableReason,
			SessionWindowStart:      poolFormatTimePtr(acc.SessionWindowStart),
			SessionWindowEnd:        poolFormatTimePtr(acc.SessionWindowEnd),
			SessionWindowStatus:     acc.SessionWindowStatus,
			ParentAccountID:         acc.ParentAccountID,
		}
		for _, g := range acc.Groups {
			if g == nil {
				continue
			}
			item.Groups = append(item.Groups, PoolGroupRef{ID: g.ID, Name: g.Name})
		}
		items = append(items, item)
		ids = append(ids, acc.ID)
	}

	s.fillTodayStats(ctx, items, ids)
	s.fillConcurrency(ctx, items, ids)

	return &PoolAccountList{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

// fillTodayStats 批量填充今日用量；失败时静默降级为零值，不影响列表可用性。
func (s *AccountPoolService) fillTodayStats(ctx context.Context, items []PoolAccount, ids []int64) {
	if len(ids) == 0 || s.accountUsageService == nil {
		return
	}
	stats, err := s.accountUsageService.GetTodayStatsBatch(ctx, ids)
	if err != nil || len(stats) == 0 {
		return
	}
	for i := range items {
		st, ok := stats[items[i].ID]
		if !ok || st == nil {
			continue
		}
		items[i].Today = PoolWindow{
			Requests:     st.Requests,
			Tokens:       st.Tokens,
			Cost:         st.Cost,
			StandardCost: st.StandardCost,
			UserCost:     st.UserCost,
		}
	}
}

// fillConcurrency 批量填充当前并发占用；Redis 不可用时保持 0。
func (s *AccountPoolService) fillConcurrency(ctx context.Context, items []PoolAccount, ids []int64) {
	if len(ids) == 0 || s.concurrencyService == nil {
		return
	}
	counts, err := s.concurrencyService.GetAccountConcurrencyBatch(ctx, ids)
	if err != nil || len(counts) == 0 {
		return
	}
	for i := range items {
		items[i].InUse = counts[items[i].ID]
	}
}

// Overview 返回号池概览：账号状态分布 + 全站今日/近 7 天用量。
func (s *AccountPoolService) Overview(ctx context.Context) (*PoolOverview, error) {
	overview := &PoolOverview{
		PlatformCounts: map[string]int64{},
		GeneratedAt:    timezone.Now().Format(time.RFC3339),
	}

	now := timezone.Now()
	for page := 1; page <= poolOverviewPages; page++ {
		accounts, total, err := s.adminService.ListAccounts(ctx, page, poolOverviewBatch, "", "", "", "", 0, "", "id", "asc")
		if err != nil {
			return nil, err
		}
		for i := range accounts {
			acc := &accounts[i]
			overview.TotalAccounts++
			if acc.Platform != "" {
				overview.PlatformCounts[acc.Platform]++
			}
			switch acc.Status {
			case StatusActive:
				overview.ActiveAccounts++
			case StatusError:
				overview.ErrorAccounts++
			default:
				overview.DisabledAccounts++
			}
			if acc.Schedulable && !poolExpired(acc, now) {
				overview.SchedulableCount++
			}
		}
		if len(accounts) == 0 || int64(page*poolOverviewBatch) >= total {
			break
		}
	}

	if s.usageLogRepo != nil {
		// 概览必须与逐个账号卡片同口径，否则「卡片之和 != 概览」会被当成 bug。
		// 走 poolGlobalWindowStats：cost 已乘账号倍率。
		dayStart := timezone.Today()
		if stats := s.poolGlobalWindowStats(ctx, dayStart); stats != nil {
			overview.Today = poolWindowFromAccountStats(stats)
		}
		weekStart := dayStart.AddDate(0, 0, -6)
		if stats := s.poolGlobalWindowStats(ctx, weekStart); stats != nil {
			overview.Window7d = poolWindowFromAccountStats(stats)
		}
	}

	return overview, nil
}

func poolExpired(acc *Account, now time.Time) bool {
	if acc == nil || acc.ExpiresAt == nil {
		return false
	}
	return acc.ExpiresAt.Before(now)
}

func poolWindowFromStats(stats *usagestats.UsageStats) PoolWindow {
	if stats == nil {
		return PoolWindow{}
	}
	window := PoolWindow{
		Requests:     stats.TotalRequests,
		Tokens:       stats.TotalTokens,
		StandardCost: stats.TotalCost,
		UserCost:     stats.TotalActualCost,
	}
	if stats.TotalAccountCost != nil {
		window.Cost = *stats.TotalAccountCost
	}
	return window
}

// poolWindowFromAccountStats 把 usagestats.AccountStats 转成号池窗口。
// 与 poolWindowFromStats 的区别：AccountStats 的 Cost 已经乘过账号倍率。
func poolWindowFromAccountStats(stats *usagestats.AccountStats) PoolWindow {
	if stats == nil {
		return PoolWindow{}
	}
	return PoolWindow{
		Requests:     stats.Requests,
		Tokens:       stats.Tokens,
		Cost:         stats.Cost,
		StandardCost: stats.StandardCost,
		UserCost:     stats.UserCost,
	}
}

// ==================== 普通用户视角（脱敏） ====================

// PoolUserAccount 是普通用户看到的账号视图。
//
// 脱敏原则（与管理员视图的差异）：
//   - 不返回备注、错误信息、限流/过载窗口、调度优先级、负载因子、分组归属。
//   - 成本只给用户自己实际被计费的那一份（user_cost），不暴露账号在全站的消耗。
//   - 平台与类型保留：用户需要知道自己在用哪家的哪个通道，但不给账号凭据线索。
type PoolUserAccount struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Platform    string     `json:"platform"`
	Type        string     `json:"type"`
	Status      string     `json:"status"`
	Schedulable bool       `json:"schedulable"`
	LastUsedAt  *string    `json:"last_used_at"`
	Today       PoolWindow `json:"today"`
}

// PoolUserOverview 是普通用户看到的号池概览：只有自己的数字。
type PoolUserOverview struct {
	AccountCount   int64            `json:"account_count"`
	ActiveCount    int64            `json:"active_count"`
	PlatformCounts map[string]int64 `json:"platform_counts"`
	Today          PoolWindow       `json:"today"`
	Window7d       PoolWindow       `json:"window_7d"`
	GeneratedAt    string           `json:"generated_at"`
}

// PoolUserAccountList 是普通用户的号池列表：条目只含脱敏字段。
type PoolUserAccountList struct {
	Items    []PoolUserAccount `json:"items"`
	Total    int64             `json:"total"`
	Page     int               `json:"page"`
	PageSize int               `json:"page_size"`
}

// userPoolStatsReader 是用户维度批量统计的可选能力。
// 用类型断言取，避免把「用户视角」强塞进 UsageLogRepository 接口、
// 逼所有测试桩跟着改。
// poolGlobalStatsReader 是 repo 的可选能力：全站号池口径窗口聚合（cost 含账号倍率）。
type poolGlobalStatsReader interface {
	GetPoolGlobalWindowStats(ctx context.Context, startTime time.Time) (*usagestats.AccountStats, error)
}

type userPoolStatsReader interface {
	ListAccountIDsByUser(ctx context.Context, userID int64, startTime time.Time) ([]int64, error)
	GetUserAccountWindowStatsBatch(ctx context.Context, userID int64, accountIDs []int64, startTime time.Time) (map[int64]*usagestats.AccountStats, error)
}

// OverviewForUser 返回该用户的号池概览（只含本人数据）。
func (s *AccountPoolService) OverviewForUser(ctx context.Context, userID int64) (*PoolUserOverview, error) {
	now := timezone.Now()
	overview := &PoolUserOverview{
		PlatformCounts: map[string]int64{},
		GeneratedAt:    now.Format(time.RFC3339),
	}
	if userID <= 0 {
		return overview, nil
	}

	reader, ok := s.userStatsReader()
	if !ok {
		return overview, nil
	}

	dayStart := timezone.Today()
	accountIDs, err := reader.ListAccountIDsByUser(ctx, userID, dayStart)
	if err != nil {
		return nil, err
	}

	// 概览需要「今日」与「近 7 天」两个窗口，账号集合按 7 天取，
	// 否则今天没调用的账号会从卡片里消失。
	weekStart := dayStart.AddDate(0, 0, -6)
	if weekIDs, weekErr := reader.ListAccountIDsByUser(ctx, userID, weekStart); weekErr == nil && len(weekIDs) > len(accountIDs) {
		accountIDs = weekIDs
	}

	overview.AccountCount = int64(len(accountIDs))
	if len(accountIDs) > 0 {
		accounts, accErr := s.adminService.GetAccountsByIDs(ctx, accountIDs)
		if accErr == nil {
			for _, acc := range accounts {
				if acc == nil {
					continue
				}
				if strings.EqualFold(acc.Status, "active") && acc.Schedulable {
					overview.ActiveCount++
				}
				platform := strings.TrimSpace(acc.Platform)
				if platform == "" {
					platform = "unknown"
				}
				overview.PlatformCounts[platform]++
			}
		}
	}

	if stats, err := reader.GetUserAccountWindowStatsBatch(ctx, userID, accountIDs, dayStart); err == nil {
		overview.Today = poolSumUserWindows(stats)
	}
	if stats, err := reader.GetUserAccountWindowStatsBatch(ctx, userID, accountIDs, weekStart); err == nil {
		overview.Window7d = poolSumUserWindows(stats)
	}

	return overview, nil
}

// ListAccountsForUser 返回该用户用过（近 7 天）的账号列表，附带本人今日用量。
func (s *AccountPoolService) ListAccountsForUser(ctx context.Context, userID int64, filter PoolAccountFilter) (*PoolUserAccountList, error) {
	result := &PoolUserAccountList{
		Items:    []PoolUserAccount{},
		Page:     filter.Page,
		PageSize: filter.PageSize,
	}
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 {
		filter.PageSize = poolDefaultPageSize
	}
	if filter.PageSize > poolMaxPageSize {
		filter.PageSize = poolMaxPageSize
	}
	result.Page = filter.Page
	result.PageSize = filter.PageSize
	if userID <= 0 {
		return result, nil
	}

	reader, ok := s.userStatsReader()
	if !ok {
		return result, nil
	}

	dayStart := timezone.Today()
	weekStart := dayStart.AddDate(0, 0, -6)

	accountIDs, err := reader.ListAccountIDsByUser(ctx, userID, weekStart)
	if err != nil {
		return nil, err
	}
	if len(accountIDs) == 0 {
		return result, nil
	}

	accounts, err := s.adminService.GetAccountsByIDs(ctx, accountIDs)
	if err != nil {
		return nil, err
	}

	todayStats, err := reader.GetUserAccountWindowStatsBatch(ctx, userID, accountIDs, dayStart)
	if err != nil {
		todayStats = map[int64]*usagestats.AccountStats{}
	}

	search := strings.ToLower(strings.TrimSpace(filter.Search))
	items := make([]PoolUserAccount, 0, len(accounts))
	for _, acc := range accounts {
		if acc == nil {
			continue
		}
		if filter.Platform != "" && !strings.EqualFold(acc.Platform, filter.Platform) {
			continue
		}
		if filter.Type != "" && !strings.EqualFold(acc.Type, filter.Type) {
			continue
		}
		if filter.Status != "" && !strings.EqualFold(acc.Status, filter.Status) {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(acc.Name), search) {
			continue
		}
		item := PoolUserAccount{
			ID:          acc.ID,
			Name:        acc.Name,
			Platform:    acc.Platform,
			Type:        acc.Type,
			Status:      acc.Status,
			Schedulable: acc.Schedulable,
			LastUsedAt:  poolFormatTimePtr(acc.LastUsedAt),
		}
		if stats, ok := todayStats[acc.ID]; ok && stats != nil {
			// 普通用户只看自己实付：cost 与 user_cost 同为实付金额，
			// 避免前端在两种口径间二选一。
			item.Today = PoolWindow{
				Requests:     stats.Requests,
				Tokens:       stats.Tokens,
				Cost:         stats.UserCost,
				StandardCost: stats.StandardCost,
				UserCost:     stats.UserCost,
			}
		}
		items = append(items, item)
	}

	result.Total = int64(len(items))
	start := (result.Page - 1) * result.PageSize
	if start > len(items) {
		start = len(items)
	}
	end := start + result.PageSize
	if end > len(items) {
		end = len(items)
	}
	result.Items = items[start:end]
	return result, nil
}

// poolGlobalWindowStats 取全站从 startTime 起的号池口径聚合。
//
// 优先用 repo 的 GetPoolGlobalWindowStats（cost 乘了账号倍率，与账号卡片一致）；
// repo 不支持时返回 nil，调用方保持零值，不用 GetGlobalStats 兜底——
// 那个口径的 cost 不含倍率，兜底会让概览和卡片对不上账，宁可显示 0 也不显示错的数。
func (s *AccountPoolService) poolGlobalWindowStats(ctx context.Context, startTime time.Time) *usagestats.AccountStats {
	reader, ok := s.usageLogRepo.(poolGlobalStatsReader)
	if !ok {
		return nil
	}
	stats, err := reader.GetPoolGlobalWindowStats(ctx, startTime)
	if err != nil || stats == nil {
		return nil
	}
	return stats
}

// userStatsReader 在底层 repo 支持用户维度聚合时返回 reader。
func (s *AccountPoolService) userStatsReader() (userPoolStatsReader, bool) {
	if s.usageLogRepo == nil {
		return nil, false
	}
	reader, ok := s.usageLogRepo.(userPoolStatsReader)
	return reader, ok
}

// poolSumUserWindows 合并「多账号」的用户维度窗口统计。
func poolSumUserWindows(stats map[int64]*usagestats.AccountStats) PoolWindow {
	window := PoolWindow{}
	for _, item := range stats {
		if item == nil {
			continue
		}
		window.Requests += item.Requests
		window.Tokens += item.Tokens
		// 用户视角的 cost 就是实付：与 user_cost 同口径，便于前端直接展示。
		window.Cost += item.UserCost
		window.StandardCost += item.StandardCost
		window.UserCost += item.UserCost
	}
	return window
}
