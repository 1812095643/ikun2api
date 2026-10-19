package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/tlsfingerprint"
	"github.com/tidwall/gjson"
)

func TestLocalClaudeTelemetryUsesObservedRequest(t *testing.T) {
	userID := formatJSONMetadataUserID("local-device", "local-account", "local-session", nil)
	wire := []byte(fmt.Sprintf(`{"model":"claude-opus-5-5","metadata":{"user_id":%q},"messages":[{"role":"user","content":"never-upload-this-content"}]}`, userID))
	req, _ := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", nil)
	req.Header.Set("Authorization", "Bearer never-upload-this-token")
	result := &ForwardResult{RequestID: "actual-request", Usage: ClaudeUsage{InputTokens: 31, OutputTokens: 7, CacheReadInputTokens: 19}}
	body, name, err := buildClaudeTelemetryBody(req, wire, &Account{}, result, nil, time.Now().Add(-time.Second), 200, 2)
	if err != nil || name != "tengu_api_success" {
		t.Fatalf("真实成功事件未正确建立：name=%s err=%v", name, err)
	}
	event := gjson.GetBytes(body, "events.0.event_data")
	if event.Get("session_id").String() != "local-session" || event.Get("device_id").String() != "local-device" || event.Get("auth.account_uuid").String() != "local-account" {
		t.Fatal("遥测必须与实际出站会话和设备身份一致")
	}
	metadata, err := base64.StdEncoding.DecodeString(event.Get("additional_metadata").String())
	if err != nil || gjson.GetBytes(metadata, "inputTokens").Int() != 31 || gjson.GetBytes(metadata, "outputTokens").Int() != 7 || gjson.GetBytes(metadata, "attempt").Int() != 2 {
		t.Fatal("遥测用量和重试次数必须来自真实请求结果")
	}
	if strings.Contains(string(body), "never-upload") || strings.Contains(string(metadata), "never-upload") {
		t.Fatal("遥测不能携带提示词或令牌")
	}
	body, name, err = buildClaudeTelemetryBody(req, wire, &Account{}, nil, fmt.Errorf("never-upload-this-error"), time.Now(), 429, 1)
	if err != nil || name != "tengu_api_error" || strings.Contains(string(body), "never-upload") {
		t.Fatal("真实错误不能被标记为成功或上传原始异常")
	}
	metadata, _ = base64.StdEncoding.DecodeString(gjson.GetBytes(body, "events.0.event_data.additional_metadata").String())
	if gjson.GetBytes(metadata, "status").String() != "429" || gjson.GetBytes(metadata, "inputTokens").Exists() {
		t.Fatal("失败事件不能编造成功用量")
	}
	if _, _, err := buildClaudeTelemetryBody(req, []byte(`{"model":"claude-opus-5-5"}`), &Account{}, result, nil, time.Now(), 200, 1); err == nil {
		t.Fatal("没有实际会话身份时不能生成虚构遥测")
	}
}

type localClaudeTelemetryTransport struct {
	HTTPUpstream
	response string
	status   int
	calls    int
	check    func(*http.Request, string, int64, int, *tlsfingerprint.Profile)
}

func (s *localClaudeTelemetryTransport) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	s.calls++
	if s.check != nil {
		s.check(req, proxy, id, concurrency, profile)
	}
	return &http.Response{StatusCode: s.status, Body: io.NopCloser(strings.NewReader(s.response))}, nil
}

func TestLocalClaudeTelemetryRequiresAcknowledgement(t *testing.T) {
	profile := &tlsfingerprint.Profile{Name: "captured-local-profile"}
	for _, test := range []struct {
		name, response string
		status         int
		accepted       bool
	}{
		{"accepted", `{"accepted_count":1}`, 200, true},
		{"discarded", `{"accepted_count":0}`, 200, false},
		{"empty_success", `{}`, 200, false},
		{"malformed", `not-json`, 200, false},
		{"rejected", `{"accepted_count":1}`, 401, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			transport := &localClaudeTelemetryTransport{response: test.response, status: test.status, check: func(req *http.Request, proxy string, id int64, concurrency int, gotProfile *tlsfingerprint.Profile) {
				if !HTTPUpstreamRedirectsDisabled(req.Context()) {
					t.Fatal("携带令牌的遥测不能跟随跨站重定向")
				}
				if req.URL.String() != claudeTelemetryEndpoint || proxy != "socks5://local-proxy:443" || id != 2 || concurrency != 3 || gotProfile != profile {
					t.Fatal("遥测必须复用账号代理及 TLS，不得另建出站路径")
				}
				if req.Header.Get("Authorization") != "Bearer local-only" || req.Header.Get("User-Agent") != "claude-cli/2.1.302" || req.Header.Get("x-service-name") != "claude-code" {
					t.Fatal("遥测认证和请求身份不一致")
				}
			}}
			gateway := &GatewayService{httpUpstream: transport}
			_, _, err := gateway.sendClaudeTelemetry(context.Background(), claudeTelemetryDelivery{body: []byte(`{"events":[]}`), authorization: "Bearer local-only", userAgent: "claude-cli/2.1.302", proxyURL: "socks5://local-proxy:443", accountID: 2, concurrency: 3, profile: profile})
			if (err == nil) != test.accepted || transport.calls != 1 {
				t.Fatal("HTTP 200 不能代替遥测接收确认")
			}
		})
	}
}

func TestLocalClaudeTelemetryScope(t *testing.T) {
	t.Setenv("SUB6_CLAUDE_TELEMETRY_ENABLED", "true")
	transport := &localClaudeTelemetryTransport{}
	gateway := &GatewayService{httpUpstream: transport}
	req, _ := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", nil)
	req.Header.Set("Authorization", "Bearer local-only")
	for _, account := range []*Account{
		{Platform: PlatformOpenAI, Type: AccountTypeOAuth},
		{Platform: PlatformAnthropic, Type: AccountTypeAPIKey},
		{Platform: PlatformAnthropic, Type: AccountTypeOAuth, Extra: map[string]any{"custom_base_url_enabled": true}},
	} {
		gateway.enqueueClaudeTelemetry(req, nil, account, nil, nil, time.Now(), 200, 1, "", nil)
	}
	if transport.calls != 0 {
		t.Fatal("不能为 GPT、API Key 或缺少实际会话的请求发送 Claude 遥测")
	}
}

func TestLocalClaudeTelemetryAcceptsProductionWireHeaders(t *testing.T) {
	t.Setenv("SUB6_CLAUDE_TELEMETRY_ENABLED", "true")
	account := &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth}
	wire := []byte(fmt.Sprintf(`{"model":"claude-sonnet-4-6","metadata":{"user_id":%q},"messages":[]}`, formatJSONMetadataUserID("local-device", "local-account", "local-session", nil)))
	for _, rawHeaders := range []bool{false, true} {
		t.Run(fmt.Sprint(rawHeaders), func(t *testing.T) {
			type observed struct{ authorization, beta, requestID string }
			sent := make(chan observed, 1)
			transport := &localClaudeTelemetryTransport{status: 200, response: `{"accepted_count":1}`,
				check: func(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) {
					body, _ := io.ReadAll(req.Body)
					metadata, _ := base64.StdEncoding.DecodeString(gjson.GetBytes(body, "events.0.event_data.additional_metadata").String())
					sent <- observed{getHeaderRaw(req.Header, "authorization"), gjson.GetBytes(body, "events.0.event_data.betas").String(), gjson.GetBytes(metadata, "clientRequestId").String()}
				}}
			gateway := &GatewayService{httpUpstream: transport}
			req, _ := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", nil)
			for key, value := range map[string]string{"authorization": "Bearer offline-only", "anthropic-beta": "oauth-2025-04-20", "x-client-request-id": "actual-wire-request"} {
				if rawHeaders {
					setHeaderRaw(req.Header, key, value)
				} else {
					req.Header.Set(key, value)
				}
			}
			gateway.enqueueClaudeTelemetry(req, wire, account, nil, nil, time.Now(), 200, 1, "", nil)
			select {
			case event := <-sent:
				if event.authorization != "Bearer offline-only" || event.beta != "oauth-2025-04-20" || event.requestID != "actual-wire-request" {
					t.Fatal("原始小写头与规范头必须得到同样的认证和关联信息")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("生产使用的小写认证头未触发内存测试传输")
			}
		})
	}
}

func TestLocalClaudeTelemetryDisabledDoesNotSend(t *testing.T) {
	t.Setenv("SUB6_CLAUDE_TELEMETRY_ENABLED", "false")
	transport := &localClaudeTelemetryTransport{}
	gateway := &GatewayService{httpUpstream: transport}
	req, _ := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", nil)
	setHeaderRaw(req.Header, "authorization", "Bearer offline-only")
	gateway.enqueueClaudeTelemetry(req, nil, &Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth}, nil, nil, time.Now(), 200, 1, "", nil)
	if transport.calls != 0 {
		t.Fatal("未完成完整协议验收的实验遥测关闭时不得发送")
	}
}
