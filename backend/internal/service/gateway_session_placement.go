package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/MACOS-DO/sub4api/internal/config"
)

// Claude OAuth 会话放置与粘性等待决策。API Key、Setup Token 和其他平台不参与。
func IsClaudeOAuthSchedulingAccount(account *Account) bool {
	return account != nil && account.Platform == PlatformAnthropic && account.Type == AccountTypeOAuth
}

// accountQuotaUsedPercent 返回账号当前仍有效的额度窗口（5h/7d 等）中最高的使用百分比。
func accountQuotaUsedPercent(account *Account, now time.Time) float64 {
	if !IsClaudeOAuthSchedulingAccount(account) {
		return 0
	}
	candidates := anthropicThresholdCandidates(account)
	var used float64
	for _, c := range candidates {
		if c == nil || c.until == nil || !c.until.After(now) {
			continue
		}
		if c.usedPercent > used {
			used = c.usedPercent
		}
	}
	return used
}

// preferQuotaHeadroom 把额度使用已达软阈值的账号排除在新会话放置之外；
// 全部都达到软阈值时保持原集合（宁可用也不拒绝）。softPercent<=0 关闭。
func preferQuotaHeadroom(accounts []accountWithLoad, softPercent int, now time.Time) []accountWithLoad {
	if softPercent <= 0 || len(accounts) < 2 {
		return accounts
	}
	headroom := make([]accountWithLoad, 0, len(accounts))
	for _, item := range accounts {
		if !IsClaudeOAuthSchedulingAccount(item.account) || accountQuotaUsedPercent(item.account, now) < float64(softPercent) {
			headroom = append(headroom, item)
		}
	}
	if len(headroom) == 0 {
		return accounts
	}
	return headroom
}

func preferQuotaHeadroomAccounts(accounts []*Account, softPercent int, now time.Time) []*Account {
	if softPercent <= 0 || len(accounts) < 2 {
		return accounts
	}
	headroom := make([]*Account, 0, len(accounts))
	for _, account := range accounts {
		if !IsClaudeOAuthSchedulingAccount(account) || accountQuotaUsedPercent(account, now) < float64(softPercent) {
			headroom = append(headroom, account)
		}
	}
	if len(headroom) == 0 {
		return accounts
	}
	return headroom
}

// sessionTTLReader 由支持读取剩余 TTL 的粘性缓存实现（Redis PTTL）。
type sessionTTLReader interface {
	GetSessionTTL(ctx context.Context, groupID int64, sessionHash string) (time.Duration, error)
}

// stickySessionIdle 返回距该会话上次在绑定账号上成功请求过了多久。每次成功转发都会把
// 绑定 TTL 刷新为 stickySessionTTL，所以 空闲 = stickySessionTTL - 剩余 TTL。
func (s *GatewayService) stickySessionIdle(ctx context.Context, groupID *int64, sessionHash string) (time.Duration, bool) {
	reader, ok := s.cache.(sessionTTLReader)
	if !ok || sessionHash == "" {
		return 0, false
	}
	ttl, err := reader.GetSessionTTL(ctx, derefGroupID(groupID), sessionHash)
	if err != nil || ttl <= 0 || ttl > stickySessionTTL {
		return 0, false
	}
	return stickySessionTTL - ttl, true
}

// stickyWaitDecision 是粘性账号忙时的决策结果。
type stickyWaitDecision struct {
	wait   bool          // 是否给出等原账号的等待计划
	grace  time.Duration // 只等原账号的时长，过后可改用其他账号
	warm   bool          // 原账号上的提示词缓存大概率仍有效
	reason string
}

// decideStickyWait 决定粘性账号忙时是等它还是立即换号。
func (s *GatewayService) decideStickyWait(ctx context.Context, cfg config.GatewaySchedulingConfig, groupID *int64, sessionHash string, bound *Account, waitingAhead int) stickyWaitDecision {
	if !cfg.FallbackWaitAnyAccount {
		return stickyWaitDecision{wait: true, warm: true, reason: "legacy_wait"}
	}
	if !IsClaudeOAuthSchedulingAccount(bound) {
		return stickyWaitDecision{wait: true, reason: "legacy_account"}
	}
	if idle, ok := s.stickySessionIdle(ctx, groupID, sessionHash); ok && cfg.StickyCacheWarmWindow > 0 && idle > cfg.StickyCacheWarmWindow {
		return stickyWaitDecision{reason: "cache_cold"}
	}
	budget := stickyCacheGrace(cfg, promptSizeHintFromContext(ctx))
	if budget <= 0 {
		return stickyWaitDecision{warm: true, reason: "switch_cheap"}
	}
	expected := s.expectedStickyWait(cfg, bound, waitingAhead)
	if expected > budget {
		slog.Debug("sticky.wait_skipped_expected_too_long",
			"account_id", bound.ID, "waiting_ahead", waitingAhead,
			"expected_ms", expected.Milliseconds(), "budget_ms", budget.Milliseconds())
		return stickyWaitDecision{warm: true, reason: "expected_wait_too_long"}
	}
	return stickyWaitDecision{wait: true, grace: budget, warm: true, reason: "worth_waiting"}
}

// expectedStickyWait 估算排到原账号一个空槽位的期望时长：(前面排队人数+1)·平均占用/并发。
func (s *GatewayService) expectedStickyWait(cfg config.GatewaySchedulingConfig, bound *Account, waitingAhead int) time.Duration {
	hold := cfg.StickyExpectedHoldDefault
	if s.concurrencyService != nil {
		if observed, ok := s.concurrencyService.AccountMeanHold(bound.ID); ok {
			hold = observed
		}
	}
	if hold <= 0 {
		hold = 10 * time.Second
	}
	concurrency := bound.Concurrency
	if concurrency <= 0 {
		concurrency = 1
	}
	if waitingAhead < 0 {
		waitingAhead = 0
	}
	return time.Duration(waitingAhead+1) * hold / time.Duration(concurrency)
}

// movePreferredFirst 把偏好账号（缓存仍热的粘性账号）移到排队候选最前，作为等待计划的主账号；
// 等待计划仍带其余候选，任一账号先空出都能用，所以这只让原账号在同一轮里先被尝试。
func movePreferredFirst(candidates []*Account, preferredID int64) {
	if preferredID <= 0 {
		return
	}
	for i, acc := range candidates {
		if acc != nil && acc.ID == preferredID {
			copy(candidates[1:i+1], candidates[:i])
			candidates[0] = acc
			return
		}
	}
}

func (s *GatewayService) claudeOAuthSchedulingEnabled(account *Account) bool {
	if !IsClaudeOAuthSchedulingAccount(account) || s == nil {
		return false
	}
	cfg := s.schedulingConfig()
	return cfg.LoadBatchEnabled && cfg.FallbackWaitAnyAccount
}

// BindClaudeOAuthSessionAfterSuccess is a no-op for every excluded account type.
// The call belongs on successful forward paths, not selection or partial errors.
func (s *GatewayService) BindClaudeOAuthSessionAfterSuccess(ctx context.Context, groupID *int64, sessionHash string, account *Account) error {
	if !s.claudeOAuthSchedulingEnabled(account) || sessionHash == "" {
		return nil
	}
	if gatewayProfitControlGateActive(ctx) {
		return s.BindStickySessionAfterProfitAdmission(ctx, groupID, sessionHash, account.ID)
	}
	return s.BindStickySession(ctx, groupID, sessionHash, account.ID)
}
