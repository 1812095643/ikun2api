package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MACOS-DO/sub4api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func TestClaudePrivacyAuxiliaryRejectsMalformedShapesAtEveryBuilder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	base := []byte(`{"model":"claude-haiku-4-5","max_tokens":10,"system":[{"type":"text","text":"system"}],"tools":[{"name":"tool","input_schema":{"type":"object"}}],"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]},{"role":"assistant","content":[{"type":"text","text":"ok"}]}]}`)
	cases := map[string][]string{
		"diagnostics":   {`["PRIVATE"]`, `"PRIVATE"`, `{"previous_message_id":{"path":"PRIVATE"}}`, `{"previous_message_id":["PRIVATE"]}`, `{"previous_message_id":"https://PRIVATE/repo"}`, `{"previous_message_id":"msg_a","previous_message_id":{"path":"PRIVATE"}}`},
		"cache_control": {`["PRIVATE"]`, `"PRIVATE"`, `{"type":"ephemeral","ttl":{"path":"PRIVATE"}}`, `{"type":["PRIVATE"]}`, `{"type":"ephemeral","ttl":"2h"}`, `{"type":"ephemeral","scope":{"path":"PRIVATE"}}`, `{"type":"ephemeral","ttl":"5m","ttl":{"path":"PRIVATE"}}`},
	}
	for kind, shapes := range cases {
		paths := []string{kind}
		if kind == "cache_control" {
			paths = append(paths, "system.0.cache_control", "tools.0.cache_control", "messages.0.content.0.cache_control", "messages.1.content.0.cache_control")
		}
		for _, path := range paths {
			for _, shape := range shapes {
				t.Run(path+shape, func(t *testing.T) {
					body, err := sjson.SetRawBytes(base, path, []byte(shape))
					require.NoError(t, err)
					_, err = SanitizeClaudePrivacySafeguards(body)
					require.Error(t, err)
					require.NotContains(t, err.Error(), "PRIVATE")
					for _, route := range []string{"messages", "count_tokens", "compatibility", "apikey"} {
						svc := &GatewayService{cfg: &config.Config{}}
						account := &Account{ID: 41, Platform: PlatformAnthropic, Type: AccountTypeOAuth}
						c, _ := gin.CreateTestContext(httptest.NewRecorder())
						c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
						switch route {
						case "messages", "compatibility":
							_, _, err = svc.buildUpstreamRequest(context.Background(), c, account, body, "synthetic", "oauth", "claude-haiku-4-5", false, route == "compatibility")
						case "count_tokens":
							_, _, err = svc.buildCountTokensRequest(context.Background(), c, account, body, "synthetic", "oauth", "claude-haiku-4-5", false)
						case "apikey":
							_, _, err = svc.buildUpstreamRequestAnthropicAPIKeyPassthrough(context.Background(), c, newAnthropicAPIKeyAccountForTest(), body, "synthetic")
						}
						require.Error(t, err, route)
						require.NotContains(t, err.Error(), "PRIVATE")
					}
				})
			}
		}
	}
}

func TestClaudePrivacyAuxiliaryPreservesBusinessBytesAndValidControls(t *testing.T) {
	body := []byte(`{"cache_control":{"type":"ephemeral","ttl":"1h","scope":"org","private":"DROP"},"diagnostics":{"previous_message_id":"msg_01abc","private":"DROP"},"tools":[{"name":"x","input_schema":{"type":"object","properties":{"cache_control":{"type":"array"},"diagnostics":{"type":"object"}}},"cache_control":{"type":"ephemeral","ttl":"5m","private":"DROP"}}],"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"git remote hash","signature":"UNCHANGED-SIGNATURE"},{"type":"tool_use","input":{"cache_control":["business"],"diagnostics":{"nested":"business"}},"id":"tool_1","name":"x"}]},{"role":"user","content":[{"type":"text","text":"cache_control git hash are business words","cache_control":{"type":"ephemeral","ttl":"5m","private":"DROP"}}]}]}`)
	clean, err := sanitizeClaudePrivacyBody(body)
	require.NoError(t, err)
	require.NotContains(t, string(clean), "DROP")
	for _, path := range []string{"messages.0.content", "tools.0.input_schema", "messages.1.content.0.text"} {
		require.Equal(t, gjson.GetBytes(body, path).Raw, gjson.GetBytes(clean, path).Raw, path)
	}
	require.Equal(t, "msg_01abc", gjson.GetBytes(clean, "diagnostics.previous_message_id").String())
	again, err := sanitizeClaudePrivacyBody(clean)
	require.NoError(t, err)
	require.Equal(t, clean, again)
}

func TestClaudePrivacyAuxiliaryNullIsAbsent(t *testing.T) {
	clean, err := sanitizeClaudePrivacyBody([]byte(`{"cache_control":null,"diagnostics":null,"system":[{"type":"text","text":"hello","cache_control":null}]}`))
	require.NoError(t, err)
	require.NotContains(t, string(clean), "cache_control")
	require.NotContains(t, string(clean), "diagnostics")
}
