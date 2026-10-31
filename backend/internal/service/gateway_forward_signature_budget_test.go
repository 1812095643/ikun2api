//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/MACOS-DO/sub4api/internal/config"
	"github.com/MACOS-DO/sub4api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type signatureBudgetSettings struct {
	SettingRepository
	disabled bool
}

func (r *signatureBudgetSettings) GetValue(_ context.Context, key string) (string, error) {
	if key == SettingKeyRectifierSettings && r.disabled {
		return `{"enabled":false,"thinking_signature_enabled":false}`, nil
	}
	return "", ErrSettingNotFound
}

func (*signatureBudgetSettings) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{}, nil
}

type signatureBudgetUpstream struct {
	HTTPUpstream
	bodies      [][]byte
	delay       time.Duration
	retryDelay  time.Duration
	cancel      context.CancelFunc
	message     string
	keepFailing bool
}

func (u *signatureBudgetUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	u.bodies = append(u.bodies, body)
	if len(u.bodies) == 1 {
		time.Sleep(u.delay)
		if u.cancel != nil {
			u.cancel()
		}
	} else {
		time.Sleep(u.retryDelay)
	}
	response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}, "X-Request-Id": {"repaired-request"}}}
	if len(u.bodies) == 1 || u.keepFailing {
		message := u.message
		if message == "" {
			message = "messages.15.content.213: Invalid `signature` in `thinking` block"
		}
		payload, _ := json.Marshal(map[string]any{"type": "error", "error": map[string]string{"type": "invalid_request_error", "message": message}})
		response.StatusCode = http.StatusBadRequest
		response.Body = io.NopCloser(strings.NewReader(string(payload)))
		return response, nil
	}
	response.Body = io.NopCloser(strings.NewReader(`{"id":"msg_repaired","type":"message","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"repaired"}],"stop_reason":"end_turn","usage":{"input_tokens":11,"output_tokens":3}}`))
	if gjson.GetBytes(body, "stream").Bool() {
		response.Header.Set("Content-Type", "text/event-stream")
		response.Body = io.NopCloser(strings.NewReader(strings.ReplaceAll(anthropicStreamWithMidStreamText(), "claude-sonnet-4-6", "claude-opus-5-5")))
	}
	return response, nil
}

func (u *signatureBudgetUpstream) DoWithTLS(req *http.Request, proxy string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, accountID, concurrency)
}

func TestGatewayService_ForwardSignatureRepairAfterSlowInitialResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name        string
		stream      bool
		disabled    bool
		cancel      bool
		keepFailing bool
		retryDelay  time.Duration
		message     string
		wantCalls   int
		wantSuccess bool
	}{
		{name: "slow_signature_400_is_repaired", wantCalls: 2, wantSuccess: true},
		{name: "slow_signature_400_stream_is_repaired", stream: true, wantCalls: 2, wantSuccess: true},
		{name: "disabled_rectifier_stays_disabled", disabled: true, wantCalls: 1},
		{name: "cancelled_request_has_no_extra_attempt", cancel: true, wantCalls: 1},
		{name: "ordinary_400_is_not_retried", message: "invalid model name", wantCalls: 1},
		{name: "failed_repair_is_not_repeated", keepFailing: true, wantCalls: 2},
		{name: "tool_fallback_uses_its_own_window", message: "Invalid thinking signature in tool_use", keepFailing: true, wantCalls: 3},
		{name: "slow_repair_does_not_extend_tool_window", message: "Invalid thinking signature in tool_use", keepFailing: true, retryDelay: maxRetryElapsed + time.Second, wantCalls: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				u := &signatureBudgetUpstream{delay: maxRetryElapsed + time.Second, retryDelay: tc.retryDelay, keepFailing: tc.keepFailing, message: tc.message}
				if tc.cancel {
					u.cancel = cancel
				}
				cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
				svc := &GatewayService{cfg: cfg, responseHeaderFilter: compileResponseHeaderFilter(cfg), httpUpstream: u, rateLimitService: &RateLimitService{}, deferredService: &DeferredService{}, settingService: NewSettingService(&signatureBudgetSettings{disabled: tc.disabled}, cfg)}
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(ctx)
				c.Request.Header.Set("User-Agent", "claude-cli/2.1.309 (external, claude-desktop-3p, agent-sdk/0.3.309)")
				body := []byte(`{"model":"claude-opus-5-5","max_tokens":128,"thinking":{"type":"adaptive"},"metadata":{"user_id":"{\"device_id\":\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\",\"account_uuid\":\"11111111-1111-4111-8111-111111111111\",\"session_id\":\"22222222-2222-4222-8222-222222222222\"}"},"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"prior reasoning","signature":"opaque-signed-history"},{"type":"tool_use","id":"toolu_1","name":"lookup","input":{"n":9007199254740993}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"tool answer"},{"type":"text","text":"Continue the task."}]}]}`)
				if tc.stream {
					body = append([]byte(`{"stream":true,`), body[1:]...)
				}
				parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
				require.NoError(t, err)
				result, err := svc.Forward(ctx, c, newAnthropicOAuthAccountForPartialUsageTest(), parsed)
				require.Equal(t, tc.wantCalls, len(u.bodies), "upstream request count")
				if !tc.wantSuccess {
					require.Error(t, err)
					return
				}
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, "repaired-request", result.RequestID)
				require.NotContains(t, string(u.bodies[1]), "opaque-signed-history")
				require.Contains(t, string(u.bodies[1]), "prior reasoning")
				require.Contains(t, string(u.bodies[1]), "Continue the task.")
				require.Equal(t, "tool_use", gjson.GetBytes(u.bodies[1], "messages.0.content.1.type").String())
				require.Equal(t, "9007199254740993", gjson.GetBytes(u.bodies[1], "messages.0.content.1.input.n").Raw)
				require.JSONEq(t, gjson.GetBytes(body, "messages.1").Raw, gjson.GetBytes(u.bodies[1], "messages.1").Raw)
				require.Equal(t, u.bodies[1], parsed.Body.Bytes())
			})
		})
	}
}
