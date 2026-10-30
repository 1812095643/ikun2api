package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MACOS-DO/sub4api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func claudePrivacySample(t *testing.T) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"model": "claude-haiku-4-5", "max_tokens": 100, "stream": false,
		"system": []any{
			map[string]any{"type": "text", "text": "x-anthropic-billing-header: cc_version=2.1.304.2be; cch=64a1d; cc_gateway=1; cc_project=PRIVATE;"},
			map[string]any{"type": "text", "text": "  X-Claude-Code-Wiggly-Dove: PRIVATE"},
			map[string]any{"type": "text", "text": "Git remote URL: https://PRIVATE/repo\r\nKeep these instructions.\nproject_hash=PRIVATE\n", "cc_unknown": "PRIVATE", "cache_control": map[string]any{"type": "ephemeral", "ttl": "1h", "repo": "PRIVATE"}},
			map[string]any{"type": "text", "text": "gitStatus: This is the git status at the start of the conversation.\nCurrent branch: PRIVATE\nRecent commits:\nPRIVATE"},
		},
		"metadata":                  map[string]any{"user_id": `{"device_id":"device-stable","account_uuid":"account-stable","session_id":"session-stable","git_remote_url":"PRIVATE","hash":"PRIVATE"}`, "project": "PRIVATE"},
		"diagnostics":               map[string]any{"previous_message_id": "msg_previous", "hash": "PRIVATE"},
		"safeguards":                map[string]any{"enabled": true, "classifier_context": map[string]any{"live_cwd": "PRIVATE", "home_dir": "PRIVATE", "git_state": map[string]any{"visibility": map[string]any{"origin": map[string]any{"remote": "PRIVATE"}}}, "permission_mode": "auto", "restricted": true}},
		"x-claude-code-wiggly-dove": "PRIVATE", "cc_gateway": "PRIVATE", "future_client_telemetry": "PRIVATE",
		"messages": []any{
			map[string]any{"role": "user", "content": "Please compare git remote https://example.test/repo and hash abc123."},
			map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "thinking", "thinking": "reason", "signature": "signed-thinking"}, map[string]any{"type": "tool_use", "id": "tool_1", "name": "git", "input": map[string]any{"hash": "abc123"}}}},
			map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": "tool_1", "content": "git result abc123"}, map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": "AA=="}}}},
		},
		"tools":         []any{map[string]any{"name": "git", "description": "git and hash are business data", "input_schema": map[string]any{"type": "object", "properties": map[string]any{"hash": map[string]any{"type": "string"}}}}},
		"thinking":      map[string]any{"type": "enabled", "budget_tokens": 1024},
		"output_config": map[string]any{"effort": "medium"}, "speed": "fast",
	})
	require.NoError(t, err)
	return body
}

func TestClaudePrivacyBodyRemovesAncillaryDataPreservesBusinessContent(t *testing.T) {
	body := claudePrivacySample(t)
	clean, err := sanitizeClaudePrivacyBody(body)
	require.NoError(t, err)
	require.NotContains(t, string(clean), "PRIVATE")
	require.NotContains(t, string(clean), "cc_gateway")
	require.NotContains(t, string(clean), "cch=")
	require.Len(t, gjson.GetBytes(clean, "system").Array(), 1)
	require.Equal(t, "Keep these instructions.\n", gjson.GetBytes(clean, "system.0.text").String())
	require.Equal(t, "1h", gjson.GetBytes(clean, "system.0.cache_control.ttl").String())
	for _, field := range []string{"messages", "tools", "thinking", "output_config", "speed"} {
		require.Equal(t, gjson.GetBytes(body, field).Raw, gjson.GetBytes(clean, field).Raw, field)
	}
	require.JSONEq(t, `{"device_id":"device-stable","account_uuid":"account-stable","session_id":"session-stable"}`, gjson.GetBytes(clean, "metadata.user_id").String())
	require.True(t, gjson.GetBytes(clean, "safeguards.enabled").Bool())
	require.Equal(t, "auto", gjson.GetBytes(clean, "safeguards.classifier_context.permission_mode").String())
	require.True(t, gjson.GetBytes(clean, "safeguards.classifier_context.restricted").Bool())
	again, err := sanitizeClaudePrivacyBody(clean)
	require.NoError(t, err)
	require.Equal(t, clean, again)
}

func TestClaudePrivacySystemFormsAndNoOverfilter(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"string", `{"system":"x-anthropic-billing-header: cc_gateway=1; cch=abc;\nExplain hashes and git."}`, `{"system":"Explain hashes and git."}`},
		{"only_attribution", `{"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.304.abc;","cache_control":{"type":"ephemeral"}}]}`, `{}`},
		{"null", `{"system":null}`, `{}`},
		{"ordinary_words", `{"system":"Teach git remote, hash and x-claude-code-wiggly-dove concepts."}`, `{"system":"Teach git remote, hash and x-claude-code-wiggly-dove concepts."}`},
		{"email_id", `{"metadata":{"user_id":"private@example.test"}}`, `{"metadata":{}}`},
		{"opaque_id", `{"metadata":{"user_id":"user_123"}}`, `{"metadata":{"user_id":"user_123"}}`},
		{"future_fields", `{"model":"claude-haiku-4-5","new_tracking":{"hash":"private"}}`, `{"model":"claude-haiku-4-5"}`},
		{"raw_precision", `{"messages":[{"role":"user","content":"abc"}],"max_tokens":9007199254740993}`, `{"messages":[{"role":"user","content":"abc"}],"max_tokens":9007199254740993}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := sanitizeClaudePrivacyBody([]byte(tc.body))
			require.NoError(t, err)
			require.Equal(t, tc.want, string(got))
		})
	}
}

func TestClaudePrivacyRejectsAmbiguousOrMalformedEnvelopes(t *testing.T) {
	for _, body := range []string{
		`{"system":"safe","system":"x-anthropic-billing-header: private"}`,
		`{"system":[{"type":"text","text":"safe","text":"private"}]}`,
		`{"metadata":{"user_id":"safe","user_id":"private"}}`,
		`{"metadata":{"user_id":"{\"device_id\":\"ok\",\"device_id\":\"private\"}"}}`,
		`{"system":[{"type":"unknown","text":"private"}]}`, `{"system":`, `[]`,
	} {
		clean, err := sanitizeClaudePrivacyBody([]byte(body))
		require.Error(t, err)
		require.Nil(t, clean)
		require.NotContains(t, err.Error(), "private")
	}
}

func TestClaudePrivacyCleansBeforeSystemMigration(t *testing.T) {
	body := []byte(`{"system":"x-anthropic-billing-header: cc_gateway=PRIVATE;\nGit remote URL: PRIVATE\nDo the task.","messages":[{"role":"user","content":"hello"}]}`)
	out := rewriteSystemForNonClaudeCode(body, gjson.GetBytes(body, "system").String())
	clean, err := sanitizeClaudePrivacyBody(out)
	require.NoError(t, err)
	require.NotContains(t, string(clean), "PRIVATE")
	require.Contains(t, string(clean), "Do the task.")
}

func TestClaudePrivacyHeadersAfterAccountOverrides(t *testing.T) {
	headers := http.Header{
		"x-claude-code-wiggly-dove": {"PRIVATE"}, "X-Claude-Code-Wiggly-Dove": {"PRIVATE"},
		"X-Anthropic-Billing-Header": {"PRIVATE"}, "X-Unknown-Future": {"PRIVATE"},
		"X-Claude-Remote-Session-Id": {"PRIVATE"}, "X-Claude-Code-Prompt-Id": {"PRIVATE"},
		"authorization": {"Bearer upstream"}, "User-Agent": {"account-agent"},
		"Anthropic-Beta": {"oauth-2025-04-20"}, "X-Claude-Code-Session-Id": {"stable-session"},
		"x-cc-compaction-request": {"true"}, "X-Anthropic-Additional-Protection": {"true"},
	}
	sanitizeClaudePrivacyHeaders(headers)
	encoded, err := json.Marshal(headers)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "PRIVATE")
	require.Equal(t, "Bearer upstream", getHeaderRaw(headers, "authorization"))
	require.Equal(t, "stable-session", getHeaderRaw(headers, "x-claude-code-session-id"))
	require.Equal(t, "true", getHeaderRaw(headers, "x-cc-compaction-request"))
	require.Equal(t, "true", getHeaderRaw(headers, "x-anthropic-additional-protection"))
}

func TestClaudePrivacyDebugSnapshotDoesNotPersistAncillaryData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway.log")
	file, err := os.Create(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = file.Close() })
	svc := &GatewayService{}
	svc.debugGatewayBodyFile.Store(file)
	svc.debugLogGatewaySnapshot("CLIENT_ORIGINAL", http.Header{"X-Claude-Code-Wiggly-Dove": {"PRIVATE"}}, claudePrivacySample(t), nil)
	logged, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(logged), "PRIVATE")
	require.Contains(t, string(logged), "signed-thinking")
}

func TestClaudePrivacyAllHTTPBuildersOnWire(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, route := range []string{"oauth", "setup-token", "mimic", "apikey", "passthrough", "oauth-count", "passthrough-count", "vertex", "vertex-count", "bedrock", "native-anthropic"} {
		t.Run(route, func(t *testing.T) {
			body := claudePrivacySample(t)
			var envelope map[string]any
			require.NoError(t, json.Unmarshal(body, &envelope))
			envelope["messages"] = append([]any{map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "text", "text": claudePrivacyCapturedEnvironment},
				map[string]any{"type": "text", "text": claudePrivacyCapturedGit},
			}}}, envelope["messages"].([]any)...)
			body, marshalErr := json.Marshal(envelope)
			require.NoError(t, marshalErr)
			svc := &GatewayService{cfg: &config.Config{}}
			account := &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			c.Request.Header = http.Header{"X-Claude-Code-Wiggly-Dove": {"PRIVATE"}, "User-Agent": {"claude-cli/2.1.304"}}
			var req *http.Request
			var err error
			switch route {
			case "oauth", "setup-token", "mimic", "apikey":
				tokenType := "oauth"
				if route == "setup-token" {
					account.Type = AccountTypeSetupToken
				}
				if route == "apikey" {
					account.Type = AccountTypeAPIKey
					tokenType = "apikey"
				}
				req, _, err = svc.buildUpstreamRequest(context.Background(), c, account, body, "upstream", tokenType, "claude-haiku-4-5", false, route == "mimic")
			case "passthrough":
				account = newAnthropicAPIKeyAccountForTest()
				account.Credentials[credKeyHeaderOverrideEnabled] = true
				account.Credentials[credKeyHeaderOverrides] = map[string]any{"X-Claude-Code-Wiggly-Dove": "PRIVATE", "X-Future-Telemetry": "PRIVATE"}
				req, _, err = svc.buildUpstreamRequestAnthropicAPIKeyPassthrough(context.Background(), c, account, body, "upstream")
			case "oauth-count":
				req, _, err = svc.buildCountTokensRequest(context.Background(), c, account, body, "upstream", "oauth", "claude-haiku-4-5", false)
			case "passthrough-count":
				account = newAnthropicAPIKeyAccountForTest()
				account.Credentials[credKeyHeaderOverrideEnabled] = true
				account.Credentials[credKeyHeaderOverrides] = map[string]any{"X-Claude-Code-Wiggly-Dove": "PRIVATE"}
				req, err = svc.buildCountTokensRequestAnthropicAPIKeyPassthrough(context.Background(), c, account, body, "upstream")
			case "vertex", "vertex-count":
				account.Type = AccountTypeServiceAccount
				account.Credentials = map[string]any{"project_id": "test-project", "region": "us-east5"}
				if route == "vertex" {
					req, err = svc.buildUpstreamRequestAnthropicVertex(context.Background(), c, account, body, "upstream", "claude-haiku-4-5", false)
				} else {
					req, _, err = svc.buildCountTokensRequestAnthropicVertex(context.Background(), c, account, body, "upstream", "claude-haiku-4-5")
				}
			case "bedrock":
				req, err = svc.buildUpstreamRequestBedrockAPIKey(context.Background(), body, "anthropic.claude-test", "us-east-1", false, "upstream")
			case "native-anthropic":
				account = newAnthropicAPIKeyAccountForTest()
				native := &OpenAIGatewayService{}
				req, _, err = native.buildNativeAnthropicUpstreamRequest(context.Background(), c, account, body, "upstream", "https://api.anthropic.com/v1/messages")
			}
			require.NoError(t, err)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got, readErr := io.ReadAll(r.Body)
				if readErr != nil {
					w.WriteHeader(500)
					return
				}
				headerJSON, _ := json.Marshal(r.Header)
				if gjson.ValidBytes(got) && !containsClaudePrivacySentinel(got) && !containsClaudePrivacySentinel(headerJSON) {
					w.WriteHeader(204)
				} else {
					w.WriteHeader(400)
				}
			}))
			defer server.Close()
			local, err := http.NewRequest(req.Method, server.URL, req.Body)
			require.NoError(t, err)
			local.Header = req.Header
			resp, err := server.Client().Do(local)
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, http.StatusNoContent, resp.StatusCode)
		})
	}
}

func containsClaudePrivacySentinel(data []byte) bool {
	return strings.Contains(string(data), "PRIVATE") || strings.Contains(string(data), "x-anthropic-billing-header:") || strings.Contains(string(data), "wiggly-dove")
}

func TestClaudePrivacyAntigravityForwardUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Set("X-Claude-Code-Wiggly-Dove", "PRIVATE")
	upstream := &anthropicHTTPUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{"id":"msg_1","type":"message","model":"claude-haiku-4-5","usage":{"input_tokens":1,"output_tokens":1},"content":[{"type":"text","text":"ok"}]}`)),
	}}
	svc := &AntigravityGatewayService{httpUpstream: upstream}
	account := &Account{ID: 1, Platform: PlatformAntigravity, Type: AccountTypeUpstream, Credentials: map[string]any{"base_url": "https://example.test", "api_key": "test-key"}}
	_, err := svc.Forward(context.Background(), c, account, claudePrivacySample(t), false)
	require.NoError(t, err)
	require.False(t, containsClaudePrivacySentinel(upstream.lastBody))
	require.Empty(t, getHeaderRaw(upstream.lastReq.Header, "x-claude-code-wiggly-dove"))
}
