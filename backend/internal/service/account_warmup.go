package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

var errWarmupSendStopped = errors.New("administrator warmup send stopped")

type WarmupQuota struct {
	FiveHour      float64    `json:"five_hour"`
	SevenDay      float64    `json:"seven_day"`
	Sonnet        *float64   `json:"seven_day_sonnet,omitempty"`
	Fable         *float64   `json:"seven_day_fable,omitempty"`
	FiveHourReset *time.Time `json:"five_hour_reset,omitempty"`
	CheckedAt     time.Time  `json:"checked_at"`
	Source        string     `json:"source"`
}

func (q WarmupQuota) SkipReason() string {
	values := []float64{q.FiveHour, q.SevenDay}
	if q.Sonnet != nil {
		values = append(values, *q.Sonnet)
	}
	if q.Fable != nil {
		values = append(values, *q.Fable)
	}
	for _, v := range values {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return "额度数据无效"
		}
	}
	if q.FiveHour > 99 {
		return "五小时用量超过 99%"
	}
	if q.SevenDay > 99 {
		return "周用量超过 99%"
	}
	if q.Sonnet != nil && *q.Sonnet > 99 {
		return "Sonnet 周用量超过 99%"
	}
	if q.Fable != nil && *q.Fable > 99 {
		return "模型周用量超过 99%"
	}
	return ""
}

// RefreshClaudeForWarmup deliberately bypasses both success/negative cache,
// estimates and passive snapshots. Caller serializes the whole warmup queue.
func (s *AccountUsageService) RefreshClaudeForWarmup(ctx context.Context, a *Account) (*WarmupQuota, error) {
	if a == nil || a.Platform != PlatformAnthropic || a.Type != AccountTypeOAuth || !a.CanGetUsage() || a.IsSyntheticUITest() {
		return nil, errors.New("不支持主动额度查询")
	}
	raw, err := s.fetchOAuthUsageRaw(ctx, a)
	if err != nil {
		return nil, errors.New("主动额度查询失败")
	}
	// Presence information is captured from the actual HTTP JSON, never inferred
	// from zero-value structs (an empty/partial response must not mean 0%).
	if raw == nil || !raw.warmupFiveObserved || !raw.warmupWeekObserved {
		return nil, errors.New("上游未提供完整的五小时及周额度")
	}
	q := &WarmupQuota{FiveHour: raw.FiveHour.Utilization, SevenDay: raw.SevenDay.Utilization, CheckedAt: time.Now(), Source: "active"}
	if raw.FiveHour.ResetsAt != "" {
		t, e := time.Parse(time.RFC3339, raw.FiveHour.ResetsAt)
		if e != nil {
			return nil, errors.New("窗口重置时间无效")
		}
		q.FiveHourReset = &t
	}
	if raw.warmupSonnetObserved {
		v := raw.SevenDaySonnet.Utilization
		q.Sonnet = &v
	}
	if raw.warmupFableObserved {
		v := raw.SevenDayOverageIncluded.Utilization
		q.Fable = &v
	}
	for _, v := range []float64{q.FiveHour, q.SevenDay} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return nil, errors.New("额度数据无效")
		}
	}
	if s.cache != nil {
		s.cache.apiCache.Store(a.ID, &apiUsageCache{response: raw, timestamp: q.CheckedAt})
	}
	s.syncActiveToPassive(ctx, a.ID, s.buildUsageInfo(raw, &q.CheckedAt))
	return q, nil
}

// UnmarshalJSON records required field presence without changing normal UI semantics.
func (r *ClaudeUsageResponse) UnmarshalJSON(data []byte) error {
	type plain ClaudeUsageResponse
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	valid := func(k string) bool {
		var window map[string]json.RawMessage
		if json.Unmarshal(fields[k], &window) != nil {
			return false
		}
		var value *float64
		return json.Unmarshal(window["utilization"], &value) == nil && value != nil
	}
	*r = ClaudeUsageResponse(decoded)
	r.warmupFiveObserved = valid("five_hour")
	r.warmupWeekObserved = valid("seven_day")
	r.warmupSonnetObserved = valid("seven_day_sonnet")
	r.warmupFableObserved = valid("seven_day_overage_included")
	return nil
}
func (s *GatewayService) RefreshWarmupQuota(ctx context.Context, usage *AccountUsageService, id int64) (*WarmupQuota, error) {
	a, err := s.accountRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if a.Platform != PlatformAnthropic || a.Type != AccountTypeOAuth || a.ParentAccountID != nil {
		return nil, errors.New("账号类型不适用")
	}
	token, _, err := s.GetAccessToken(ctx, a)
	if err != nil {
		return nil, errors.New("账号授权不可用")
	}
	clone := *a
	clone.Credentials = make(map[string]any, len(a.Credentials))
	for k, v := range a.Credentials {
		clone.Credentials[k] = v
	}
	clone.Credentials["access_token"] = token
	return usage.RefreshClaudeForWarmup(ctx, &clone)
}

type WarmupAttemptResult struct {
	Status          string     `json:"status"`
	Reason          string     `json:"reason,omitempty"`
	InputTokens     int        `json:"input_tokens"`
	OutputTokens    int        `json:"output_tokens"`
	WindowReset     *time.Time `json:"window_reset,omitempty"`
	WindowConfirmed bool       `json:"window_confirmed"`
}

// Warmup uses the normal production request builder, privacy filters, proxy,
// identity and transport. No retry can send a second network request within
// this attempt: the private callback fences ALL sendUpstream calls.
func (s *GatewayService) RunAccountWarmup(ctx context.Context, id int64, model, session string, turn int, beforeSend func() error) (WarmupAttemptResult, error) {
	out := WarmupAttemptResult{Status: "skipped"}
	a, err := s.accountRepo.GetByID(ctx, id)
	if err != nil {
		return out, err
	}
	if a.Platform != PlatformAnthropic || a.Type != AccountTypeOAuth || a.ParentAccountID != nil || a.IsSyntheticUITest() || !a.IsSchedulable() {
		out.Reason = "账号当前不可调度"
		return out, nil
	}
	if a.IsRateLimited() || a.GetRateLimitRemainingTimeWithContext(ctx, model) > 0 {
		out.Reason = "账号仍处于限流"
		return out, nil
	}
	if model == "" || len(model) > 120 || !strings.HasPrefix(model, "claude-") {
		return out, errors.New("请选择有效 Claude 模型")
	}
	if s.concurrencyService == nil {
		return out, errors.New("并发服务不可用")
	}
	load, err := s.concurrencyService.GetAccountConcurrencyBatch(ctx, []int64{id})
	if err != nil {
		return out, err
	}
	if load[id] > 0 {
		out.Reason = "账号正在处理业务请求"
		return out, nil
	}
	slot, err := s.concurrencyService.AcquireAccountSlot(ctx, id, a.Concurrency)
	if err != nil {
		return out, err
	}
	if !slot.Acquired {
		out.Reason = "账号繁忙"
		return out, nil
	}
	defer slot.ReleaseFunc()
	if s.identityService == nil {
		return out, errors.New("账号身份服务不可用")
	}
	fp, err := s.identityService.GetOrCreateFingerprint(ctx, a.ID, http.Header{})
	if err != nil || fp == nil {
		return out, errors.New("账号身份不可用")
	}
	metadata := FormatMetadataUserID(fp.ClientID, a.GetExtraString("account_uuid"), session, ExtractCLIVersion(fp.UserAgent))
	text := "hi"
	if turn > 1 {
		text = "hello"
	}
	payload := map[string]any{"model": model, "max_tokens": 64, "stream": false, "messages": []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": text}}}}, "metadata": map[string]any{"user_id": metadata}}
	raw, err := json.Marshal(payload)
	if err != nil {
		return out, err
	}
	parsed, err := ParseGatewayRequest(NewRequestBodyRef(raw), PlatformAnthropic)
	if err != nil {
		return out, err
	}
	sent := false
	parsed.warmupBeforeSend = func() error {
		if sent {
			return errors.New("warmup attempt already sent")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := beforeSend(); err != nil {
			return err
		}
		sent = true
		return nil
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequestWithContext(ctx, http.MethodPost, "/internal/account-warmup", nil)
	c.Request.Header.Set("User-Agent", "ikun2api-admin-warmup")
	result, forwardErr := s.Forward(ctx, c, a, parsed)
	if result != nil {
		out.InputTokens = result.Usage.InputTokens
		out.OutputTokens = result.Usage.OutputTokens
		if reset := result.UpstreamHeaders.Get("anthropic-ratelimit-unified-5h-reset"); reset != "" {
			var sec int64
			if _, e := fmt.Sscan(reset, &sec); e == nil {
				if sec > 1e11 {
					sec /= 1000
				}
				t := time.Unix(sec, 0)
				if t.After(time.Now()) && t.Before(time.Now().Add(6*time.Hour)) {
					out.WindowReset = &t
					out.WindowConfirmed = true
				}
			}
		}
	}
	if forwardErr != nil || w.Code >= 400 {
		if !sent {
			out.Status = "skipped"
			out.Reason = "未发送，请检查账号及模型"
		} else {
			out.Status = "unknown"
			out.Reason = "上游结果未完成，请查看账号状态"
		}
		var upstream *UpstreamFailoverError
		if errors.As(forwardErr, &upstream) {
			if upstream.StatusCode == 429 {
				out.Status = "rate_limited"
				out.Reason = "上游返回 429，已交由限流管理"
			} else if upstream.StatusCode >= 400 {
				out.Status = "failed"
				out.Reason = fmt.Sprintf("上游返回 %d", upstream.StatusCode)
			}
		}
		return out, nil
	}
	if !sent || result == nil {
		out.Reason = "未确认实际出站"
		return out, nil
	}
	out.Status = "success"
	return out, nil
}
