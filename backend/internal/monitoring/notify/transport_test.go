package notify

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/stretchr/testify/require"
)

type routeTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (r routeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	copy := req.Clone(req.Context())
	u := *req.URL
	u.Host = r.target.Host
	u.Scheme = r.target.Scheme
	copy.URL = &u
	return r.base.RoundTrip(copy)
}

func TestNotificationPlatformProtocols(t *testing.T) {
	for _, kind := range domain.ChannelKinds {
		t.Run(kind, func(t *testing.T) {
			var sawSend, sawAuth bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				var body map[string]any
				require.NoError(t, json.Unmarshal(raw, &body))
				w.Header().Set("Content-Type", "application/json")
				if strings.Contains(r.URL.Path, "tenant_access_token") {
					sawAuth = true
					require.Equal(t, "app", body["app_id"])
					_, _ = io.WriteString(w, `{"code":0,"tenant_access_token":"access","expire":7200}`)
					return
				}
				if strings.Contains(r.URL.Path, "accessToken") {
					sawAuth = true
					require.Equal(t, "app", body["appKey"])
					_, _ = io.WriteString(w, `{"accessToken":"access","expireIn":7200}`)
					return
				}
				if strings.Contains(r.URL.Path, "getAppAccessToken") {
					sawAuth = true
					require.Equal(t, "app", body["appId"])
					_, _ = io.WriteString(w, `{"access_token":"access","expires_in":"7200"}`)
					return
				}
				sawSend = true
				switch kind {
				case "telegram":
					require.Equal(t, "/bot123:test/sendMessage", r.URL.Path)
					require.EqualValues(t, 7, body["message_thread_id"])
					_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":45}}`)
				case "feishu_webhook", "lark_webhook":
					stamp := body["timestamp"].(string)
					mac := hmac.New(sha256.New, []byte(stamp+"\nsigning"))
					require.Equal(t, base64.StdEncoding.EncodeToString(mac.Sum(nil)), body["sign"])
					require.Equal(t, "interactive", body["msg_type"])
					_, _ = io.WriteString(w, `{"code":0}`)
				case "feishu_app", "lark_app":
					require.Equal(t, "Bearer access", r.Header.Get("Authorization"))
					require.Equal(t, "chat_id", r.URL.Query().Get("receive_id_type"))
					require.Equal(t, "target", body["receive_id"])
					_, _ = io.WriteString(w, `{"code":0,"data":{"message_id":"om_test"}}`)
				case "wecom":
					require.Equal(t, "text", body["msgtype"])
					_, _ = io.WriteString(w, `{"errcode":0}`)
				case "dingtalk":
					require.Equal(t, "/v1.0/robot/groupMessages/send", r.URL.Path)
					require.Equal(t, "access", r.Header.Get("x-acs-dingtalk-access-token"))
					require.Equal(t, "robot", body["robotCode"])
					_, _ = io.WriteString(w, `{"processQueryKey":"ding_test"}`)
				case "qq":
					require.Equal(t, "/v2/groups/target/messages", r.URL.Path)
					require.Equal(t, "QQBot access", r.Header.Get("Authorization"))
					_, _ = io.WriteString(w, `{"id":"qq_test"}`)
				case "webhook":
					require.Equal(t, "firing", body["event_type"])
					require.NotEmpty(t, r.Header.Get("X-IKUN-Signature"))
					_, _ = io.WriteString(w, `{}`)
				}
			}))
			defer server.Close()
			target, err := url.Parse(server.URL)
			require.NoError(t, err)
			s := Sender{Client: &http.Client{Transport: routeTransport{target, http.DefaultTransport}}, Email: func(_ context.Context, to string, _ Message) error {
				require.Equal(t, "target", to)
				sawSend = true
				return nil
			}}
			c := domain.Channel{ID: kind, Kind: kind, Settings: map[string]string{"app_id": "app", "robot_code": "robot"}}
			endpoint := "https://public.example/hook"
			switch kind {
			case "feishu_webhook":
				endpoint = "https://open.feishu.cn/open-apis/bot/v2/hook/test"
			case "lark_webhook":
				endpoint = "https://open.larksuite.com/open-apis/bot/v2/hook/test"
			case "wecom":
				endpoint = "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=test"
			}
			secrets := map[string]string{"webhook": endpoint, "signing_secret": "signing", "app_secret": "secret", "bot_token": "123:test"}

			settings := map[string]string{"type": "group", "thread_id": "7"}
			if strings.HasSuffix(kind, "_app") {
				settings["type"] = "chat_id"
			}
			_, err = s.Send(context.Background(), c, secrets, domain.Target{Address: "target", Settings: settings}, Message{Title: "测试", Text: "状态更新", Kind: "firing"}, "")
			require.NoError(t, err)
			require.True(t, sawSend)
			if strings.HasSuffix(kind, "_app") || kind == "dingtalk" || kind == "qq" {
				require.True(t, sawAuth)
			}
		})
	}
}

func TestNotificationReceiptsAndUncertainty(t *testing.T) {
	cases := []struct {
		name, kind, body, want string
		status                 int
		uncertain              bool
	}{
		{"business_reject", "feishu_webhook", `{"code":99991663}`, "credentials", 200, false},
		{"rate_limit", "telegram", `{"ok":false,"error_code":429,"parameters":{"retry_after":12}}`, "rate_limit", 429, false},
		{"qq_window", "qq", `{"code":22009,"message":"proactive message requires interaction"}`, "platform_restricted", 400, false},
		{"permission", "wecom", `{"errcode":403,"errmsg":"permission denied"}`, "permission", 200, false},
		{"server_retry", "webhook", `{}`, "platform", 503, false},
		{"truncated_receipt", "telegram", `{`, "response", 200, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			s := Sender{Client: server.Client()}
			_, err := s.call(context.Background(), "POST", server.URL, nil, []byte(`{}`), tc.kind, false)
			var f *Failure
			require.ErrorAs(t, err, &f)
			require.Equal(t, tc.want, f.Class)
			if f.Class == "rate_limit" {
				require.Equal(t, 12*time.Second, f.RetryAfter)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := Sender{Client: http.DefaultClient}
	_, err := s.call(ctx, "POST", "https://example.test", nil, []byte(`{}`), "webhook", false)
	require.Error(t, err)
	require.NoError(t, ValidateEndpoint("webhook", "https://example.com/hook"))
	require.Error(t, ValidateEndpoint("webhook", "http://127.0.0.1/hook"))
	require.Error(t, ValidateEndpoint("feishu_webhook", "https://example.com/hook"))
}

func TestNotificationContentDoesNotExportEvidence(t *testing.T) {
	cfg := domain.DefaultConfig()
	cfg.DetailBaseURL = "https://site.example"
	i := domain.Incident{ID: "event", Rule: "U02", Severity: "P1", Status: "firing", Model: "gpt", LastSeen: time.Now(), LastActor: "private@example.com", ControlReason: "secret", Explanation: domain.Explanation{Facts: []string{"sk-secret"}, Advice: []string{"prompt text"}}}
	m := BuildMessage(cfg, i, "firing", false)
	raw, err := json.Marshal(m.Event)
	require.NoError(t, err)
	for _, secret := range []string{"private@example.com", "sk-secret", "prompt text", "secret"} {
		require.NotContains(t, string(raw), secret)
	}
	require.Equal(t, "https://site.example/admin/ops?incident=event", m.DetailURL)
}

func TestNotificationEditsAndAmbiguousTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		switch r.URL.Path {
		case "/bot123:test/editMessageText":
			require.EqualValues(t, 45, body["message_id"])
			_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":45}}`)
		case "/open-apis/auth/v3/tenant_access_token/internal":
			_, _ = io.WriteString(w, `{"code":0,"tenant_access_token":"access","expire":7200}`)
		case "/open-apis/im/v1/messages/om_test":
			require.Equal(t, "PATCH", r.Method)
			require.NotEmpty(t, body["content"])
			_, _ = io.WriteString(w, `{"code":0}`)
		default:
			time.Sleep(20 * time.Millisecond)
			_, _ = io.WriteString(w, `{}`)
		}
	}))
	defer server.Close()
	target, err := url.Parse(server.URL)
	require.NoError(t, err)
	s := Sender{Client: &http.Client{Transport: routeTransport{target, http.DefaultTransport}}}
	_, err = s.Send(context.Background(), domain.Channel{Kind: "telegram"}, map[string]string{"bot_token": "123:test"}, domain.Target{Address: "target"}, Message{Text: "更新"}, "45")
	require.NoError(t, err)
	_, err = s.Send(context.Background(), domain.Channel{ID: "app", Kind: "feishu_app", Settings: map[string]string{"app_id": "id"}}, map[string]string{"app_secret": "secret"}, domain.Target{Address: "target"}, Message{Text: "恢复"}, "om_test")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	_, err = s.call(ctx, "POST", "https://example.test/wait", nil, []byte(`{}`), "webhook", false)
	var failure *Failure
	require.ErrorAs(t, err, &failure)
	require.True(t, failure.Uncertain)
}
