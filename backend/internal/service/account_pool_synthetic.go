package service

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"
	"time"
)

// poolSyntheticRotationSeconds 合成账号属性的轮换周期，与伪名桶保持一致。
const poolSyntheticRotationSeconds = poolPseudoRotationSeconds

// poolFilterSyntheticUserItems 让合成账号也遵守用户当前选择的筛选条件。
// 少了这一步，「只看 openai」的用户仍然会看到 anthropic 合成号，穿帮。
func poolFilterSyntheticUserItems(items []PoolUserAccount, filter PoolAccountFilter) []PoolUserAccount {
	if len(items) == 0 {
		return items
	}
	search := strings.ToLower(strings.TrimSpace(filter.Search))
	kept := make([]PoolUserAccount, 0, len(items))
	for _, item := range items {
		if filter.Platform != "" && !strings.EqualFold(item.Platform, filter.Platform) {
			continue
		}
		if filter.Type != "" && !strings.EqualFold(item.Type, filter.Type) {
			continue
		}
		if filter.Status != "" && !strings.EqualFold(item.Status, filter.Status) {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(item.Name), search) {
			continue
		}
		kept = append(kept, item)
	}
	return kept
}

// buildSyntheticUserAccounts 为普通用户视图生成合成账号。
//
// 目的只是让号池看起来像一个真实在用的池子：账号名走同一套 7 天伪名，
// 容量与额度由 (用户, 序号, 桶) 的哈希派生出稳定值，所以刷新页面不会跳变，
// 换桶之后才会换一批。合成账号永远不写库，也不会出现在管理员视图里。
func (s *AccountPoolService) buildSyntheticUserAccounts(_ context.Context, userID int64, plan PoolSyntheticPlan, now time.Time) []PoolUserAccount {
	if plan.empty() || userID <= 0 {
		return nil
	}
	items := make([]PoolUserAccount, 0, plan.Count)
	for index := 0; index < plan.Count; index++ {
		items = append(items, poolSyntheticUserAccount(userID, index, plan, now))
	}
	return items
}

// poolSyntheticUserAccount 生成第 index 个合成账号。
func poolSyntheticUserAccount(userID int64, index int, plan PoolSyntheticPlan, now time.Time) PoolUserAccount {
	// 负数 id 保证与真实账号永远不冲突，前端 row key 也不会撞。
	id := int64(-(index + 1))
	seed := poolSyntheticSeed(userID, index, now)

	platform := strings.TrimSpace(plan.Platform)
	if platform == "" {
		platform = DefaultPoolUserSyntheticPlatform
	}

	banned := index >= plan.available()
	status := poolSyntheticStatusActive
	if banned {
		status = poolSyntheticBannedStatus(index)
	}

	// 容量与额度都从 seed 派生：同一桶内稳定，跨桶轮换。
	concurrency := 3                                     // fixed display capacity
	inUse := int((seed >> 8) % uint64(concurrency+1))    // 0..concurrency
	fiveHour := 20.0 + float64((seed>>16)%66)            // 20..85
	sevenDay := 30.0 + float64((seed>>24)%61)            // 30..90
	requests := int64(6 + (seed>>32)%38)                 // 6..43
	tokens := requests * int64(900+(seed>>40)%2100)      // 每请求 900..3000 tokens
	cost := float64(tokens) * poolSyntheticTokenPriceUSD // 粗略折算，只用于展示

	if banned {
		// 不可用的账号：额度打满、今日没有新流量，符合"跑废了"的样子。
		inUse = 0
		fiveHour = 100
		sevenDay = 100
		requests = 0
		tokens = 0
		cost = 0
	}

	return PoolUserAccount{
		ID:          id,
		Name:        poolPseudoAccountName(userID, id, now),
		Platform:    platform,
		Type:        poolSyntheticAccountType,
		Status:      status,
		Schedulable: !banned,
		LastUsedAt:  poolFormatTimePtr(poolSyntheticLastUsed(seed, banned, now)),
		Today: PoolWindow{
			Requests:     requests,
			Tokens:       tokens,
			Cost:         cost,
			StandardCost: cost,
			UserCost:     cost,
		},
		Capacity:  PoolUserCapacity{InUse: inUse, Concurrency: concurrency},
		FiveHour:  poolSyntheticWindow(fiveHour, now.Add(time.Duration(25+(seed>>48)%250)*time.Minute), now),
		SevenDay:  poolSyntheticWindow(sevenDay, now.Add(time.Duration(30+(seed>>56)%140)*time.Hour), now),
		Synthetic: true,
	}
}

const (
	poolSyntheticAccountType    = "oauth"
	poolSyntheticStatusActive   = "active"
	poolSyntheticStatusError    = "error"
	poolSyntheticStatusDisabled = "disabled"
	// poolSyntheticTokenPriceUSD 合成账号每 token 的展示单价，仅用于让今日费用
	// 与今日 tokens 保持量级一致，不参与任何计费。
	poolSyntheticTokenPriceUSD = 0.0000025
)

// poolSyntheticBannedStatus 不可用账号的状态：error 与 disabled 交替，
// 避免整列清一色同一种故障，看起来不像是人为摆出来的。
func poolSyntheticBannedStatus(index int) string {
	if index%2 == 0 {
		return poolSyntheticStatusError
	}
	return poolSyntheticStatusDisabled
}

// poolSyntheticSeed 以 (用户, 序号, 轮换桶) 派生稳定哈希。
func poolSyntheticSeed(userID int64, index int, now time.Time) uint64 {
	bucket := now.Unix() / poolSyntheticRotationSeconds
	hasher := fnv.New64a()
	_, _ = fmt.Fprintf(hasher, "pool-synthetic|%d|%d|%d", userID, index, bucket)
	return hasher.Sum64()
}

// poolSyntheticWindow 组装一条额度窗口读数。
func poolSyntheticWindow(utilization float64, resetsAt time.Time, now time.Time) PoolUserQuotaWindow {
	remaining := int(resetsAt.Sub(now).Seconds())
	if remaining < 0 {
		remaining = 0
	}
	formatted := resetsAt.Format(time.RFC3339)
	return PoolUserQuotaWindow{
		Utilization:      utilization,
		ResetsAt:         &formatted,
		RemainingSeconds: remaining,
		Enabled:          true,
	}
}

// poolSyntheticLastUsed 最近使用时间：可用账号落在近几小时，不可用账号落在更早。
func poolSyntheticLastUsed(seed uint64, banned bool, now time.Time) *time.Time {
	if banned {
		at := now.Add(-time.Duration(6+(seed>>4)%40) * time.Hour)
		return &at
	}
	at := now.Add(-time.Duration(3+(seed>>4)%170) * time.Minute)
	return &at
}

// poolPaginateUserItems 按页切分用户视图条目，与真实账号共用同一套边界处理。
func poolPaginateUserItems(items []PoolUserAccount, page, pageSize int) []PoolUserAccount {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		return items
	}
	start, end := len(items), len(items)
	if len(items) > 0 && page-1 <= (len(items)-1)/pageSize {
		start = (page - 1) * pageSize
		end = start + min(pageSize, len(items)-start)
	}
	return items[start:end]
}

// poolAccountGroupMatches 判断账号是否属于指定分组。
func poolAccountGroupMatches(acc *Account, groupID int64) bool {
	if acc == nil || groupID <= 0 {
		return false
	}
	for _, group := range acc.Groups {
		if group != nil && group.ID == groupID {
			return true
		}
	}
	return false
}
