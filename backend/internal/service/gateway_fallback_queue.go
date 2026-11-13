package service

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/MACOS-DO/sub4api/internal/config"
)

// 兜底排队的账号分配。
//
// 所有候选账号槽位都满时，原实现按「优先级 / 周额度重置 / 最后使用时间」排序后把
// 等待计划交给排第一的账号；同一波溢出请求看到的是同一份排序，于是全部排到同一个
// 账号上等待超时，其余账号空出槽位也无人领取。这里改为：
//   1. 按实时排队压力（在途 + 排队）/ 负载因子稳定重排，原排序只决定同压力时的先后；
//   2. 等待计划附带同批可替换账号，等待中的请求可抢占任一先空出的槽位。

// maxFallbackWaitAlternates 限制每次轮询额外尝试的账号数，控制 Redis 调用量。
const maxFallbackWaitAlternates = 8

func (s *GatewayService) freshQueuePressure(ctx context.Context, accounts []*Account) map[int64]float64 {
	return freshQueuePressure(ctx, s.concurrencyService, accounts)
}

// freshQueuePressure 绕过负载批量缓存读取实时负载，返回每个账号的排队压力。
// 少于两个账号或读取失败时返回 nil，调用方保持原顺序。
func freshQueuePressure(ctx context.Context, concurrency *ConcurrencyService, accounts []*Account) map[int64]float64 {
	if concurrency == nil || len(accounts) < 2 {
		return nil
	}
	eligible := make([]*Account, 0, len(accounts))
	for _, account := range accounts {
		if IsClaudeOAuthSchedulingAccount(account) {
			eligible = append(eligible, account)
		}
	}
	if len(eligible) < 2 {
		return nil
	}
	accounts = eligible
	loads := make([]AccountWithConcurrency, 0, len(accounts))
	for _, acc := range accounts {
		loads = append(loads, AccountWithConcurrency{ID: acc.ID, MaxConcurrency: acc.EffectiveLoadFactor()})
	}
	loadMap, err := concurrency.GetAccountsLoadBatchFresh(ctx, loads)
	if err != nil {
		slog.Debug("fallback_queue.load_batch_failed", "accounts", len(accounts), "error", err)
		return nil
	}
	pressure := make(map[int64]float64, len(accounts))
	for _, acc := range accounts {
		info := loadMap[acc.ID]
		if info == nil {
			return nil // incomplete load snapshots cannot safely rank candidates
		}
		pressure[acc.ID] = float64(info.CurrentConcurrency+info.WaitingCount) / float64(acc.EffectiveLoadFactor())
	}
	return pressure
}

// sortByQueuePressure 按排队压力升序稳定排序；压力相同的保持原有相对顺序。
func sortByQueuePressure[T any](items []T, accountOf func(T) *Account, pressure map[int64]float64) {
	if len(pressure) == 0 || len(items) < 2 {
		return
	}
	// Reorder only Claude OAuth positions in a mixed pool; other account positions stay unchanged.
	positions := make([]int, 0, len(items))
	eligible := make([]T, 0, len(items))
	for i, item := range items {
		if IsClaudeOAuthSchedulingAccount(accountOf(item)) {
			positions = append(positions, i)
			eligible = append(eligible, item)
		}
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		return pressure[accountOf(eligible[i]).ID] < pressure[accountOf(eligible[j]).ID]
	})
	for i, pos := range positions {
		items[pos] = eligible[i]
	}

}

// fallbackWaitAlternates 从已排序的候选中挑出可替换账号（不含被选中的账号）。
// 需要登记会话配额的账号不进入替换列表：登记有副作用，只能在真正选中时做。
func (s *GatewayService) fallbackWaitAlternates(cfg config.GatewaySchedulingConfig, chosenID int64, ordered []*Account, sessionHash string) []*Account {
	if !cfg.FallbackWaitAnyAccount {
		return nil
	}
	chosenEligible := false
	for _, account := range ordered {
		if account != nil && account.ID == chosenID && IsClaudeOAuthSchedulingAccount(account) {
			chosenEligible = true
			break
		}
	}
	if !chosenEligible {
		return nil
	}
	var alternates []*Account
	for _, acc := range ordered {
		if !IsClaudeOAuthSchedulingAccount(acc) || acc.ID == chosenID || s.sessionLimitApplies(acc, sessionHash) {
			continue
		}
		alternates = append(alternates, acc)
		if len(alternates) == maxFallbackWaitAlternates {
			break
		}
	}
	return alternates
}

// sessionLimitApplies 与 checkAndRegisterSession 的生效条件保持一致。
func (s *GatewayService) sessionLimitApplies(acc *Account, sessionHash string) bool {
	return sessionHash != "" && s.sessionLimitCache != nil && acc.IsAnthropicOAuthOrSetupToken() && acc.GetMaxSessions() > 0
}

// HydrateWaitAlternate 补全从 AccountWaitPlan.Alternates 中抢到槽位的账号，
// 与选号结果里的主账号走同一份调度快照。
func (s *GatewayService) HydrateWaitAlternate(ctx context.Context, account *Account) (*Account, error) {
	return s.hydrateSelectedAccount(ctx, account)
}

// RevalidateWaitAlternate is the simple single-account validation entry point.
func (s *GatewayService) RevalidateWaitAlternate(ctx context.Context, account *Account, requestedModel string) (*Account, error) {
	return s.revalidateClaudeOAuthWaitAccount(ctx, account, requestedModel, nil, "", 0)
}

func (s *GatewayService) revalidateClaudeOAuthWaitAccount(ctx context.Context, account *Account, requestedModel string, groupID *int64, sessionHash string, boundID int64) (*Account, error) {
	if !IsClaudeOAuthSchedulingAccount(account) {
		return nil, fmt.Errorf("wait-plan account is not Claude OAuth")
	}
	var latest *Account
	var err error
	// Read authoritative state once a slot is acquired; queue snapshots can be stale.
	if s.accountRepo != nil {
		latest, err = s.accountRepo.GetByID(ctx, account.ID)
	} else {
		latest, err = s.hydrateSelectedAccount(ctx, account)
	}
	if err != nil {
		return nil, err
	}
	if !IsClaudeOAuthSchedulingAccount(latest) || latest.ID != account.ID || !latest.IsSchedulable() || s.isAccountBlockedBySchedulingThreshold(ctx, latest) {
		return nil, fmt.Errorf("queued Claude OAuth account is no longer schedulable")
	}
	if latest.Concurrency != account.Concurrency {
		return nil, fmt.Errorf("queued account concurrency changed; wait plan must be rebuilt")
	}

	if groupID != nil && !s.isAccountInGroup(latest, groupID) {
		return nil, fmt.Errorf("queued account no longer belongs to the selected group")
	}
	if groupID == nil && len(latest.AccountGroups) > 0 {
		return nil, fmt.Errorf("queued account no longer belongs to the ungrouped pool")
	}
	if !s.isGatewayAccountProfitEligible(ctx, latest) ||
		(requestedModel != "" && (!s.isModelSupportedByAccountWithContext(ctx, latest, requestedModel) || !s.isAccountSchedulableForModelSelection(ctx, latest, requestedModel))) {
		return nil, fmt.Errorf("queued account no longer passes the model or pricing gate")
	}
	if s.needsUpstreamChannelRestrictionCheck(ctx, groupID) && s.isUpstreamModelRestrictedByChannel(ctx, *groupID, latest, requestedModel) {
		return nil, fmt.Errorf("queued account no longer passes the channel restriction")
	}
	// No prefetch context is reused here: limits may have changed during the wait.
	sticky := latest.ID == boundID
	if !s.isAccountSchedulableForQuota(latest) || !s.isAccountSchedulableForWindowCost(ctx, latest, sticky) || !s.isAccountSchedulableForRPM(ctx, latest, sticky) {
		return nil, fmt.Errorf("queued account no longer passes scheduling limits")
	}
	if !s.checkAndRegisterSession(ctx, latest, sessionHash) {
		return nil, fmt.Errorf("queued account session quota is full")
	}
	return latest, nil
}

// 粘性会话的缓存感知等待。
//
// 上游提示词缓存按账号隔离：同一会话换号后缓存命中率从约 96% 掉到约 18%，首字因重新
// 预填充变慢——5–20 万 token 约多 1.5 秒，20 万 token 以上约多 4.7 秒（2026-11 线上中位数）。
// stickyCacheGrace 给出按请求大小估算的换号首字代价，作为等原账号的预算上限，
// 并硬性封顶 maxStickyCacheGrace；是否真的等由 decideStickyWait 结合预计等待决定。
// 小请求没有缓存可丢，预算为 0。

// maxStickyCacheGrace 是宽限期的硬上限，配置得再大也不会超过它。
const maxStickyCacheGrace = 10 * time.Second

type promptSizeHintKey struct{}

// WithPromptSizeHint 记录请求体字节数，供调度估算换号的缓存代价。
func WithPromptSizeHint(ctx context.Context, bodyBytes int) context.Context {
	if bodyBytes <= 0 {
		return ctx
	}
	return context.WithValue(ctx, promptSizeHintKey{}, bodyBytes)
}

func promptSizeHintFromContext(ctx context.Context) int {
	if v, ok := ctx.Value(promptSizeHintKey{}).(int); ok {
		return v
	}
	return 0
}

// stickyCacheGrace 返回粘性等待中只等原账号的时长；请求大小未知时按普通宽限期处理。
func stickyCacheGrace(cfg config.GatewaySchedulingConfig, bodyBytes int) time.Duration {
	var grace time.Duration
	switch {
	case bodyBytes <= 0:
		grace = cfg.StickyCacheGrace
	case cfg.StickyCacheGraceLargeBytes > 0 && bodyBytes >= cfg.StickyCacheGraceLargeBytes:
		grace = cfg.StickyCacheGraceLarge
	case bodyBytes >= cfg.StickyCacheGraceMinBytes:
		grace = cfg.StickyCacheGrace
	}
	if grace < 0 {
		return 0
	}
	if grace > maxStickyCacheGrace {
		return maxStickyCacheGrace
	}
	return grace
}

// attachStickyWaitAlternates 为粘性等待计划补上可替换账号与宽限期（见 decideStickyWait）。
// others 只在需要时调用，返回除粘性账号外仍可调度的候选。
func (s *GatewayService) attachStickyWaitAlternates(ctx context.Context, cfg config.GatewaySchedulingConfig, plan *AccountWaitPlan, grace time.Duration, sessionHash string, others func() []*Account) {
	if !cfg.FallbackWaitAnyAccount || plan == nil {
		return
	}
	candidates := others()
	if len(candidates) == 0 {
		return
	}
	sortByQueuePressure(candidates, func(acc *Account) *Account { return acc }, s.freshQueuePressure(ctx, candidates))
	plan.Alternates = s.fallbackWaitAlternates(cfg, plan.AccountID, append([]*Account{{ID: plan.AccountID, Platform: PlatformAnthropic, Type: AccountTypeOAuth}}, candidates...), sessionHash)
	if len(plan.Alternates) > 0 {
		if plan.Timeout > 0 && grace >= plan.Timeout {
			grace = plan.Timeout / 2
		}
		plan.AlternatesAfter = grace
	}
}
