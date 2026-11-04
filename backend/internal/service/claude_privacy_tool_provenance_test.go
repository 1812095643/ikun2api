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
	"github.com/tidwall/sjson"
)

const claudeProvenanceBody = `{
 "model":"claude-sonnet-4-6","max_tokens":32,
 "tools":[{"name":"mcp__audit__lookup","description":"Business URL https://business.invalid/path",
 "input_schema":{"type":"object","properties":{"ownership_metadata":{"type":"object","example":{"server_url":"https://business.invalid/schema"}}}},
 "ownership_metadata":{"verified":false,"owner":"mcp","server_url":"https://mcp.PRIVATE.invalid/ORG_PRIVATE/PATH_PRIVATE","directory_entry":"PRIVATE","future_identity":{"git":"PRIVATE"}}}],
 "messages":[
 {"role":"user","content":"Keep business URL https://business.invalid/prompt"},
 {"role":"assistant","content":[{"type":"thinking","thinking":"ownership_metadata server_url git hash are business words","signature":"SIGNED-BYTES-UNCHANGED"},{"type":"tool_use","id":"toolu_business","name":"mcp__audit__lookup","input":{"ownership_metadata":{"server_url":"https://business.invalid/input"},"n":9007199254740993}}]},
 {"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_business","content":[{"type":"tool_addition","tool":{"definition":{"ownership_metadata":{"server_url":"https://business.invalid/result","verified":true}}}}]},{"type":"tool_addition","tool":{"type":"custom","definition":{"name":"mcp__audit__later","input_schema":{"type":"object"},"ownership_metadata":{"verified":false,"owner":"mcp","server_url":"https://mcp.PRIVATE.invalid/ORG_PRIVATE/PATH_PRIVATE"},"cache_control":{"type":"ephemeral","ttl":"5m","future_path":"PRIVATE"}}}}]},
 {"role":"assistant","content":[{"type":"tool_addition","tool":{"type":"custom","definition":{"name":"mcp__audit__other","input_schema":{"type":"object"},"ownership_metadata":{"verified":false,"owner":"mcp","server_url":"https://mcp.PRIVATE.invalid/ORG_PRIVATE/PATH_PRIVATE"}}}}]}
 ]}`

func TestClaudePrivacyToolProvenancePreservesBusinessAndSafetySemantics(t *testing.T) {
	original := []byte(claudeProvenanceBody)
	for _, sanitize := range []struct {
		name string
		run  func([]byte) ([]byte, error)
	}{
		{"ingress", SanitizeClaudePrivacySafeguards}, {"outbound", sanitizeClaudePrivacyBody},
	} {
		t.Run(sanitize.name, func(t *testing.T) {
			clean, err := sanitize.run(original)
			require.NoError(t, err)
			require.NotContains(t, string(clean), "PRIVATE")
			for _, path := range []string{"tools.0.ownership_metadata", "messages.2.content.1.tool.definition.ownership_metadata", "messages.3.content.0.tool.definition.ownership_metadata"} {
				require.JSONEq(t, `{"verified":false,"owner":"mcp"}`, gjson.GetBytes(clean, path).Raw, path)
			}
			for _, path := range []string{"tools.0.input_schema", "tools.0.name", "tools.0.description", "messages.0", "messages.1", "messages.2.content.0", "messages.2.content.1.tool.definition.input_schema"} {
				require.Equal(t, gjson.GetBytes(original, path).Raw, gjson.GetBytes(clean, path).Raw, path)
			}
			require.Equal(t, "5m", gjson.GetBytes(clean, "messages.2.content.1.tool.definition.cache_control.ttl").String())
			again, err := sanitize.run(clean)
			require.NoError(t, err)
			require.Equal(t, clean, again)
		})
	}
}

func TestClaudePrivacyToolProvenanceRejectsAmbiguousShapes(t *testing.T) {
	for _, path := range []string{"tools.0.ownership_metadata", "messages.2.content.1.tool.definition.ownership_metadata"} {
		for name, shape := range map[string]string{
			"string": `"PRIVATE"`, "array": `["PRIVATE"]`, "true_verified": `{"verified":true,"owner":"mcp"}`,
			"string_verified": `{"verified":"false","owner":"mcp"}`, "other_owner": `{"verified":false,"owner":"PRIVATE"}`,
			"missing_owner": `{"verified":false,"server_url":"PRIVATE"}`, "missing_verified": `{"owner":"mcp"}`,
			"duplicate_known":   `{"verified":false,"owner":"mcp","owner":"PRIVATE"}`,
			"duplicate_dropped": `{"verified":false,"owner":"mcp","server_url":"PRIVATE","server_url":"PRIVATE"}`,
		} {
			t.Run(path+"/"+name, func(t *testing.T) {
				input, err := sjson.SetRawBytes([]byte(claudeProvenanceBody), path, []byte(shape))
				require.NoError(t, err)
				clean, err := SanitizeClaudePrivacySafeguards(input)
				require.Error(t, err)
				require.Nil(t, clean)
				require.NotContains(t, err.Error(), "PRIVATE")
			})
		}
	}
	for name, body := range map[string]string{
		"legacy_bad_cache_block": `{"tools":[{"type":"thinking","cache_control":{"type":"ephemeral"}}]}`,
		"duplicate_metadata":     `{"tools":[{"ownership_metadata":{"verified":false,"owner":"mcp"},"ownership_metadata":{"server_url":"PRIVATE"}}]}`,
		"duplicate_block_type":   `{"messages":[{"role":"user","content":[{"type":"text","type":"tool_addition","tool":{"definition":{"ownership_metadata":{"server_url":"PRIVATE"}}}}]}]}`,
		"duplicate_tool_wrapper": `{"messages":[{"role":"user","content":[{"type":"tool_addition","tool":{},"tool":{"definition":{"ownership_metadata":{"server_url":"PRIVATE"}}}}]}]}`,
		"bad_definition":         `{"messages":[{"role":"user","content":[{"type":"tool_addition","tool":{"definition":["PRIVATE"]}}]}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := SanitizeClaudePrivacySafeguards([]byte(body))
			require.Error(t, err)
			require.NotContains(t, err.Error(), "PRIVATE")
		})
	}
}

func TestClaudePrivacyToolProvenanceNullAndLegacyAreStable(t *testing.T) {
	for _, body := range []string{
		`{"tools":[{"name":"a","input_schema":{"type":"object"}}],"messages":[{"role":"user","content":"legacy"}]}`,
		`{"tools":[{"name":"a","ownership_metadata":{"verified":false,"owner":"mcp"}}]}`,
	} {
		clean, err := SanitizeClaudePrivacySafeguards([]byte(body))
		require.NoError(t, err)
		require.Equal(t, body, string(clean))
	}
	clean, err := SanitizeClaudePrivacySafeguards([]byte(`{"tools":[{"name":"a","ownership_metadata":null}]}`))
	require.NoError(t, err)
	require.JSONEq(t, `{"tools":[{"name":"a"}]}`, string(clean))
}

func TestClaudePrivacyToolProvenanceEveryBuilderOnWire(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, route := range []string{"setup-token", "passthrough", "count", "oauth", "oauth-mimic", "oauth-count"} {
		t.Run(route, func(t *testing.T) {
			svc := &GatewayService{cfg: &config.Config{}}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			c.Request.Header.Set("User-Agent", "claude-cli/2.1.318")
			c.Request.Header.Set("Anthropic-Beta", "tool-provenance-2026-10-26")
			c.Request.Header["x-claude-code-sandy-plover"] = []string{`[{"name":"PRIVATE","url":"PRIVATE","directory_entry":"PRIVATE"}]`}
			c.Request.Header["X-Claude-Code-Sandy-Plover"] = []string{`[{"name":"PRIVATE"}]`}
			c.Request.Header.Set("x-cc-internal-tool-provenance-server-url", "PRIVATE")
			account := &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeSetupToken}
			var req *http.Request
			var err error
			body := []byte(claudeProvenanceBody)
			switch route {
			case "setup-token":
				req, _, err = svc.buildUpstreamRequest(context.Background(), c, account, body, "synthetic", "oauth", "claude-sonnet-4-6", false, false)
			case "oauth", "oauth-mimic":
				account.Type = AccountTypeOAuth
				req, _, err = svc.buildUpstreamRequest(context.Background(), c, account, body, "synthetic", "oauth", "claude-sonnet-4-6", false, route == "oauth-mimic")
			case "passthrough":
				req, _, err = svc.buildUpstreamRequestAnthropicAPIKeyPassthrough(context.Background(), c, newAnthropicAPIKeyAccountForTest(), body, "synthetic")
			case "count", "oauth-count":
				if route == "oauth-count" {
					account.Type = AccountTypeOAuth
				}
				req, _, err = svc.buildCountTokensRequest(context.Background(), c, account, body, "synthetic", "oauth", "claude-sonnet-4-6", false)
			}
			require.NoError(t, err)
			var wire []byte
			var headers http.Header
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				wire, _ = io.ReadAll(r.Body)
				headers = r.Header.Clone()
				w.WriteHeader(204)
			}))
			defer server.Close()
			local, err := http.NewRequest(req.Method, server.URL, req.Body)
			require.NoError(t, err)
			local.Header = req.Header.Clone()
			response, err := server.Client().Do(local)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			headerJSON, err := json.Marshal(headers)
			require.NoError(t, err)
			require.NotContains(t, string(wire)+string(headerJSON), "PRIVATE")
			require.Equal(t, "SIGNED-BYTES-UNCHANGED", gjson.GetBytes(wire, "messages.1.content.0.signature").String())
			require.Equal(t, gjson.GetBytes(body, "messages.1.content.1.input").Raw, gjson.GetBytes(wire, "messages.1.content.1.input").Raw)
			// Metadata must follow the effective outbound capability, including mimicry.
			require.Equal(t, route != "oauth-mimic", strings.Contains(getHeaderRaw(headers, "anthropic-beta"), "tool-provenance-2026-10-26"))
			require.Equal(t, route != "oauth-mimic", gjson.GetBytes(wire, "tools.0.ownership_metadata").Exists())
			require.Equal(t, route != "oauth-mimic", gjson.GetBytes(wire, "messages.2.content.1.tool.definition.ownership_metadata").Exists())
		})
	}
}

func TestClaudePrivacyToolProvenanceDebugSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.log")
	file, err := os.Create(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = file.Close() })
	svc := &GatewayService{}
	svc.debugGatewayBodyFile.Store(file)
	svc.debugLogGatewaySnapshot("CLIENT_ORIGINAL", http.Header{"X-Claude-Code-Sandy-Plover": {"PRIVATE"}}, []byte(claudeProvenanceBody), nil)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "PRIVATE")
	require.Contains(t, string(raw), "SIGNED-BYTES-UNCHANGED")
}

func TestClaudePrivacySensitiveHeaderFamiliesRemainBlocked(t *testing.T) {
	for _, header := range []string{"X-Claude-Code-Sandy-Plover", "x-claude-code-wiggly-dove", "X-CC-Internal-Tool-Provenance-Server-URL", "x-cc-internal-tool-provenance-future"} {
		require.True(t, isClaudePrivacyBlockedHeader(header), header)
	}
	for _, header := range []string{"X-Claude-Code-Session-Id", "x-cc-compaction-request", "X-Anthropic-Additional-Protection"} {
		require.False(t, isClaudePrivacyBlockedHeader(header), header)
	}
	headers := http.Header{"x-claude-code-sandy-plover": {`[{"name":"PRIVATE"}]`}, "X-CLAUDE-CODE-SANDY-PLOVER": {`[{"name":"PRIVATE","url":"PRIVATE","directory_entry":"PRIVATE","future":{"value":"PRIVATE"}}]`}, "x-cc-internal-tool-provenance-future": {"PRIVATE"}, "X-Claude-Code-Session-Id": {"stable-session"}, "X-Anthropic-Additional-Protection": {"true"}}
	sanitizeClaudePrivacyHeaders(headers)
	raw, err := json.Marshal(headers)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "PRIVATE")
	require.Equal(t, "stable-session", getHeaderRaw(headers, "x-claude-code-session-id"))
	require.Equal(t, "true", getHeaderRaw(headers, "x-anthropic-additional-protection"))
}

func TestClaudePrivacyToolProvenanceRejectsEscapedDuplicateKeys(t *testing.T) {
	escaped := string(rune(92)) + "u006f"
	cases := []string{
		`{"tools":[{"ownership_metadata":{"verified":false,"owner":"mcp","` + escaped + `wner":"PRIVATE"}}]}`,
		`{"tools":[{"ownership_metadata":{"verified":false,"owner":"mcp"},"` + escaped + `wnership_metadata":{"verified":false,"owner":"mcp","server_url":"PRIVATE"}}]}`,
		`{"messages":[{"role":"user","content":[{"type":"text","type":"tool_addition","t` + escaped + `ol":{"definition":{"ownership_metadata":{"server_url":"PRIVATE"}}}}]}]}`,
	}
	for _, raw := range cases {
		require.True(t, gjson.Valid(raw))
		clean, err := SanitizeClaudePrivacySafeguards([]byte(raw))
		require.Error(t, err)
		require.Nil(t, clean)
		require.NotContains(t, err.Error(), "PRIVATE")
	}
}
