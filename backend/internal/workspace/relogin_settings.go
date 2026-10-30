package workspace

import (
	"context"
	"fmt"
	"html"
	"math"
	"regexp"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

type accountTemplate struct {
	ProxyID        *int64  `json:"proxy_id"`
	GroupIDs       []int64 `json:"group_ids"`
	Concurrency    int     `json:"concurrency"`
	Priority       int     `json:"priority"`
	RateMultiplier float64 `json:"rate_multiplier"`
}
type reloginConfig struct {
	AutoEnabled    bool            `json:"auto_enabled"`
	Template       accountTemplate `json:"template"`
	QuotaEnabled   bool            `json:"quota_enabled"`
	QuotaInterval  int             `json:"quota_interval"`
	QuotaThreshold float64         `json:"quota_threshold"`
	Recipients     []string        `json:"recipients"`
}

func (h *Handler) loadReloginSettings(ctx context.Context) (reloginConfig, error) {
	s := reloginConfig{Template: accountTemplate{GroupIDs: []int64{}, Concurrency: 100, Priority: 1, RateMultiplier: 1}, QuotaInterval: 300, QuotaThreshold: 20, Recipients: []string{}}
	err := h.getRuntime(ctx, "relogin_settings", &s)
	return s, err
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
	var s reloginConfig
	if err := body(c, &s, 64<<10); err != nil {
		fail(c, err)
		return
	}
	if s.Template.Concurrency < 1 || s.Template.Concurrency > 10000 || s.Template.Priority < 0 || s.Template.RateMultiplier < 0 || math.IsNaN(s.Template.RateMultiplier) || math.IsInf(s.Template.RateMultiplier, 0) || s.QuotaInterval < 60 || s.QuotaInterval > 86400 || s.QuotaThreshold < 0 || math.IsNaN(s.QuotaThreshold) || math.IsInf(s.QuotaThreshold, 0) || len(s.Recipients) > 20 {
		fail(c, badRequest("请检查并发数、刷新间隔和额度阈值。"))
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
			fail(c, err)
			return
		}
		if !p.IsActive() || p.IsExpired(time.Now()) {
			fail(c, badRequest("所选默认代理当前不可用。"))
			return
		}
	}
	for _, id := range s.Template.GroupIDs {
		g, err := h.admin.GetGroup(ctx, id)
		if err != nil {
			fail(c, err)
			return
		}
		if g.Status != "active" || g.Platform != "openai" {
			fail(c, badRequest("请选择当前有效的 OpenAI 分组。"))
			return
		}
	}
	if err := h.setRuntime(ctx, "relogin_settings", s); err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"saved": true})
}
func (h *Handler) reloginOptions(c *gin.Context) {
	ctx := c.Request.Context()
	groups, err := h.admin.GetAllGroupsByPlatform(ctx, "openai")
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
	p := []gin.H{}
	for _, item := range groups {
		if item.Status == "active" {
			g = append(g, gin.H{"id": item.ID, "name": item.Name})
		}
	}
	for _, item := range proxies {
		if item.IsActive() && !item.IsExpired(time.Now()) {
			p = append(p, gin.H{"id": item.ID, "name": item.Name})
		}
	}
	response.Success(c, gin.H{"groups": g, "proxies": p})
}

var expiredCredentials = regexp.MustCompile(`(?i)401|unauthorized|token revoked|invalidated|authentication failed`)

func (h *Handler) automaticRelogin(ctx context.Context) (int, error) {
	accounts, err := h.allOpenAIAccounts(ctx)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, a := range accounts {
		if count >= 20 {
			break
		}
		if a.Status != "error" || a.ParentAccountID != nil || !expiredCredentials.MatchString(a.ErrorMessage) {
			continue
		}
		email := accountEmail(a)
		var recent int
		if err = h.db.QueryRowContext(ctx, "SELECT count(*) FROM workspace_relogin_runs WHERE account_id=$1 AND trigger='auto' AND started_at>NOW()-INTERVAL '6 hours'", a.ID).Scan(&recent); err != nil {
			return count, err
		}
		if recent > 0 {
			continue
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
		h.startUpstreamBalanceWorker()
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
