package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"math"
	"time"

	"github.com/MACOS-DO/sub4api/internal/handler/dto"
	"github.com/MACOS-DO/sub4api/internal/pkg/response"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
)

type accountTemplate struct {
	ProxyID            *int64               `json:"proxy_id"`
	GroupIDs           []int64              `json:"group_ids"`
	Concurrency        int                  `json:"concurrency"`
	LoadFactor         *int                 `json:"load_factor"`
	Priority           int                  `json:"priority"`
	RateMultiplier     float64              `json:"rate_multiplier"`
	AutoPauseOnExpired bool                 `json:"auto_pause_on_expired"`
	Extra              accountTemplateExtra `json:"extra"`
	ModelMapping       map[string]string    `json:"model_mapping"`
}
type reloginConfig struct {
	AutoEnabled            bool            `json:"auto_enabled"`
	AutoErrorThreshold     int             `json:"auto_error_threshold"`
	AutoErrorWindowMinutes int             `json:"auto_error_window_minutes"`
	AutoCooldownMinutes    int             `json:"auto_cooldown_minutes"`
	Template               accountTemplate `json:"template"`
	QuotaEnabled           bool            `json:"quota_enabled"`
	QuotaInterval          int             `json:"quota_interval"`
	QuotaThreshold         float64         `json:"quota_threshold"`
	Recipients             []string        `json:"recipients"`
}

func (h *Handler) loadReloginSettings(ctx context.Context) (reloginConfig, error) {
	s := defaultReloginConfig()
	var stored json.RawMessage
	if err := h.getRuntime(ctx, "relogin_settings", &stored); err != nil {
		return s, err
	}
	if !hasCompleteReloginTemplate(stored) {
		if legacy, err := h.legacyReloginTemplate(ctx); err != nil {
			return s, err
		} else if len(legacy) > 0 {
			if err := json.Unmarshal(legacy, &s.Template); err != nil {
				return s, err
			}
			// Old host WS=off delegated to the plugin; native requests now use WS directly.
			s.Template.Extra.OpenAIWSMode = service.OpenAIWSIngressModeCtxPool
			s.Template.Extra.OpenAIWSEnabled = true
		}
	}
	if len(stored) > 0 {
		if err := json.Unmarshal(stored, &s); err != nil {
			return s, err
		}
	}
	if err := normalizeReloginTemplate(&s.Template); err != nil {
		return s, err
	}
	if s.AutoErrorThreshold == 0 {
		s.AutoErrorThreshold = 5
	}
	if s.AutoErrorWindowMinutes == 0 {
		s.AutoErrorWindowMinutes = 10
	}
	if s.AutoCooldownMinutes == 0 {
		s.AutoCooldownMinutes = 360
	}
	if err := h.pruneDeletedReloginGroups(ctx, &s); err != nil {
		return s, err
	}
	return s, nil
}

// 仅读到明确已删除的引用才修复保存值；数据库异常不当成分组删除。
func (h *Handler) pruneDeletedReloginGroups(ctx context.Context, s *reloginConfig) error {
	if len(s.Template.GroupIDs) == 0 {
		return nil
	}
	rows, err := h.db.QueryContext(ctx, `SELECT id FROM groups WHERE id=ANY($1) AND deleted_at IS NULL`, pq.Array(s.Template.GroupIDs))
	if err != nil {
		return err
	}
	existing := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		existing[id] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	cleaned := []int64{}
	for _, id := range s.Template.GroupIDs {
		if existing[id] {
			cleaned = append(cleaned, id)
		}
	}
	if len(cleaned) == len(s.Template.GroupIDs) {
		return nil
	}
	before, err := json.Marshal(s.Template.GroupIDs)
	if err != nil {
		return err
	}
	after, err := json.Marshal(cleaned)
	if err != nil {
		return err
	}
	// 只改分组字段，并比较原分组值，避免覆盖管理员并发保存的其他设置。
	if _, err = h.db.ExecContext(ctx, `UPDATE workspace_runtime
	 SET value=jsonb_set(value,'{template,group_ids}',$1::jsonb),updated_at=NOW()
	 WHERE key='relogin_settings' AND value#>'{template,group_ids}'=$2::jsonb`, string(after), string(before)); err != nil {
		return err
	}
	s.Template.GroupIDs = cleaned
	return nil
}
func (h *Handler) reloginSettings(c *gin.Context) {
	s, err := h.loadReloginSettings(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	var state map[string]any
	if err = h.getRuntime(c.Request.Context(), "relogin_status", &state); err != nil {
		fail(c, err)
		return
	}
	if state == nil {
		state = map[string]any{}
	}
	var quota map[string]any
	if err = h.getRuntime(c.Request.Context(), "relogin_quota", &quota); err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"settings": s, "state": state, "quota": quota})
}
func (h *Handler) saveReloginSettings(c *gin.Context) {
	s, err := h.loadReloginSettings(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	storedGroups := make(map[int64]bool, len(s.Template.GroupIDs))
	for _, id := range s.Template.GroupIDs {
		storedGroups[id] = true
	}
	if err := body(c, &s, 64<<10); err != nil {
		fail(c, err)
		return
	}
	if s.AutoErrorThreshold < 1 || s.AutoErrorThreshold > 100 || s.AutoErrorWindowMinutes < 1 || s.AutoErrorWindowMinutes > 60 || s.AutoCooldownMinutes < 1 || s.AutoCooldownMinutes > 1440 {
		fail(c, badRequest("认证异常次数应为 1 至 100 次，统计窗口应为 1 至 60 分钟，重登间隔应为 1 至 1440 分钟。"))
		return
	}
	if s.Template.Concurrency < 1 || s.Template.Concurrency > 10000 || s.Template.Priority < 0 || s.Template.RateMultiplier < 0 || math.IsNaN(s.Template.RateMultiplier) || math.IsInf(s.Template.RateMultiplier, 0) || s.QuotaInterval < 60 || s.QuotaInterval > 86400 || s.QuotaThreshold < 0 || math.IsNaN(s.QuotaThreshold) || math.IsInf(s.QuotaThreshold, 0) || len(s.Recipients) > 20 {
		fail(c, badRequest("请检查并发数、刷新间隔和额度阈值。"))
		return
	}
	if err := normalizeReloginTemplate(&s.Template); err != nil {
		fail(c, err)
		return
	}
	ctx := c.Request.Context()
	for i, email := range s.Recipients {
		normalized, err := normalizeEmail(email)
		if err != nil {
			fail(c, err)
			return
		}
		s.Recipients[i] = normalized
	}
	if s.Template.ProxyID != nil {
		p, err := h.admin.GetProxy(ctx, *s.Template.ProxyID)
		if err != nil {
			if errors.Is(err, service.ErrProxyNotFound) {
				fail(c, badRequest("默认代理已删除，请重新选择代理。"))
			} else {
				fail(c, err)
			}
			return
		}
		if !p.IsActive() || p.IsExpired(time.Now()) {
			fail(c, badRequest("所选默认代理当前不可用。"))
			return
		}
	}
	groups := []int64{}
	seenGroups := map[int64]bool{}
	for _, id := range s.Template.GroupIDs {
		g, err := h.admin.GetGroup(ctx, id)
		if err != nil {
			// 设置打开后分组也可能被删除；保存前再次核对并剔除失效引用。
			if errors.Is(err, service.ErrGroupNotFound) {
				continue
			}
			fail(c, err)
			return
		}
		if g.Status != "active" || (g.Platform != "openai" && (g.Platform != "composite" || h.cfg != nil && h.cfg.RunMode == "simple")) {
			if storedGroups[id] {
				continue
			}
			fail(c, badRequest(fmt.Sprintf("分组「%s」当前不可用于 OpenAI 账号。", g.Name)))
			return
		}
		if !seenGroups[id] {
			groups = append(groups, id)
			seenGroups[id] = true
		}
	}
	s.Template.GroupIDs = groups
	if err := h.setRuntime(ctx, "relogin_settings", s); err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"saved": true})
}
func (h *Handler) reloginOptions(c *gin.Context) {
	ctx := c.Request.Context()
	groups, err := h.admin.GetAllGroups(ctx)
	if err != nil {
		fail(c, err)
		return
	}
	proxies, err := h.admin.GetAllProxies(ctx)
	if err != nil {
		fail(c, err)
		return
	}
	g := []gin.H{}
	p := []*dto.Proxy{}
	for _, item := range groups {
		if item.Status == "active" && (item.Platform == "openai" || (item.Platform == "composite" && (h.cfg == nil || h.cfg.RunMode != "simple"))) {
			g = append(g, gin.H{"id": item.ID, "name": item.Name, "platform": item.Platform,
				"description": item.Description, "rate_multiplier": item.RateMultiplier,
				"subscription_type": item.SubscriptionType, "account_count": item.AccountCount, "status": item.Status})
		}
	}
	for _, item := range proxies {
		if item.IsActive() && !item.IsExpired(time.Now()) {
			p = append(p, dto.ProxyFromService(&item))
		}
	}
	response.Success(c, gin.H{"groups": g, "proxies": p})
}

func (h *Handler) automaticRelogin(ctx context.Context) (int, error) {
	h.reloginScanMu.Lock()
	defer h.reloginScanMu.Unlock()
	s, err := h.loadReloginSettings(ctx)
	if err != nil {
		return 0, err
	}
	accounts, err := h.allOpenAIAccounts(ctx)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, a := range accounts {
		if count >= 20 {
			break
		}
		if !service.IsOpenAIOAuthReloginAccount(&a) {
			continue
		}
		pending := service.OpenAIOAuthRecoveryPending(&a)
		thresholdReached := h.recovery != nil && a.IsActive() && a.Schedulable && h.recovery.OpenAIOAuthAuthFailureCount(&a, time.Duration(s.AutoErrorWindowMinutes)*time.Minute) >= s.AutoErrorThreshold
		if !pending && !thresholdReached {
			continue
		}
		email := accountEmail(a)
		var recent int
		if err = h.db.QueryRowContext(ctx, `SELECT count(*) FROM workspace_relogin_runs WHERE account_id=$1 AND trigger='auto'
		 AND (status IN ('running','challenge') OR started_at>NOW()-$2*INTERVAL '1 minute')`, a.ID, s.AutoCooldownMinutes).Scan(&recent); err != nil {
			return count, err
		}
		if recent > 0 {
			continue
		}
		// 先暂停达到阈值的坏凭据；缺少登录资料也不能继续反复失败。
		if thresholdReached || (pending && a.Extra[service.OpenAIOAuthRecoveryExtraKey] == nil) {
			message := a.ErrorMessage
			if thresholdReached {
				message = fmt.Sprintf("OpenAI OAuth authentication failed: %d 次认证异常，等待自动重登", s.AutoErrorThreshold)
			}
			if h.recovery == nil {
				continue
			}
			paused, pauseErr := h.recovery.PauseOpenAIOAuthForRelogin(ctx, &a, message, true)
			if pauseErr != nil {
				continue
			}
			a = *paused
		}
		secret, e := h.loadSecret(ctx, email)
		if e != nil {
			continue
		}
		id := a.ID
		if _, e = h.queueRelogin(ctx, secret, &id, "auto", true); e == nil {
			count++
		}
	}
	return count, h.setRuntime(ctx, "relogin_status", gin.H{"last_auto_run": time.Now(), "last_auto_count": count})
}
func (h *Handler) runAutomaticRelogin(c *gin.Context) {
	count, err := h.automaticRelogin(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"queued": count})
}

func (h *Handler) startBackground() {
	h.startOnce.Do(func() {
		h.startWarmupWorker()
		h.startUpstreamBalanceWorker()
		h.startUpstreamModelWorker()
		h.startLotteryWorker()
		h.startBlindboxWorker()
		ctx, cancel := context.WithTimeout(h.background, 10*time.Second)
		_, _ = h.db.ExecContext(ctx, "UPDATE workspace_relogin_runs SET status='interrupted',finished_at=NOW(),error_message='服务已重启，请重新发起授权。' WHERE status='running'")
		cancel()
		go func() {
			ticker := time.NewTicker(time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-h.background.Done():
					return
				case <-ticker.C:
				}
				ctx, cancel := context.WithTimeout(h.background, 5*time.Minute)
				s, err := h.loadReloginSettings(ctx)
				if err == nil {
					if s.AutoEnabled {
						_, _ = h.automaticRelogin(ctx)
					}
					if s.QuotaEnabled {
						_ = h.updateQuotaSnapshot(ctx, s)
					}
				}
				cancel()
			}
		}()
	})
}

type quotaSnapshot struct {
	UpdatedAt      time.Time `json:"updated_at"`
	TotalRemaining float64   `json:"total_remaining_percent"`
	Alerted        bool      `json:"alerted"`
	Items          []gin.H   `json:"items"`
}

func (h *Handler) updateQuotaSnapshot(ctx context.Context, s reloginConfig) error {
	if h.quota == nil {
		return nil
	}
	var old quotaSnapshot
	if err := h.getRuntime(ctx, "relogin_quota", &old); err != nil {
		return err
	}
	if time.Since(old.UpdatedAt) < time.Duration(s.QuotaInterval)*time.Second {
		return nil
	}
	accounts, err := h.allOpenAIAccounts(ctx)
	if err != nil {
		return err
	}
	snapshot := quotaSnapshot{UpdatedAt: time.Now(), Items: []gin.H{}}
	for _, account := range accounts {
		if account.Status != "active" || !account.Schedulable || account.ParentAccountID != nil {
			continue
		}
		usage, e := h.quota.QueryUsage(ctx, account.ID)
		if e != nil {
			return e
		}
		if usage.RateLimit == nil {
			continue
		}
		window := usage.RateLimit.SecondaryWindow
		if window == nil {
			window = usage.RateLimit.PrimaryWindow
		}
		if window == nil {
			continue
		}
		remaining := math.Max(0, 100-window.UsedPercent)
		snapshot.TotalRemaining += remaining
		snapshot.Items = append(snapshot.Items, gin.H{"account_id": account.ID, "email": accountEmail(account), "remaining_percent": remaining, "reset_at": window.ResetAt})
	}
	snapshot.Alerted = len(snapshot.Items) > 0 && snapshot.TotalRemaining < s.QuotaThreshold
	if snapshot.Alerted && !old.Alerted && h.email != nil {
		content := html.EscapeString(fmt.Sprintf("当前可调度账号的剩余额度合计为 %.1f%%，低于设置的 %.1f%%。请在失效重登页面查看账号状态。", snapshot.TotalRemaining, s.QuotaThreshold))
		for _, recipient := range s.Recipients {
			if err = h.email.SendEmail(ctx, recipient, "[IKUN2api] 账号额度提醒", content); err != nil {
				return err
			}
		}
	}
	return h.setRuntime(ctx, "relogin_quota", snapshot)
}
