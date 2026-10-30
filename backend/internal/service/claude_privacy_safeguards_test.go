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

func claudePrivacySafetySample(t *testing.T, array bool) []byte {
	t.Helper()
	entry := map[string]any{
		"type": "dangerous_tool_use", "enabled": true,
		"classifier_context": map[string]any{
			"v": 1, "permission_mode": "auto", "platform": "win32",
			"live_cwd": "PRIVATE_CWD", "home_dir": "PRIVATE_HOME", "user_identity": "PRIVATE_IDENTITY",
			"git_state":           map[string]any{"cwd": "PRIVATE_GIT_CWD", "branch": "PRIVATE_BRANCH", "status": map[string]any{"porcelain": "PRIVATE_FILE"}, "visibility": map[string]any{"origin": map[string]any{"remote": "PRIVATE_REMOTE", "hash": "PRIVATE_HASH"}}},
			"rule_roots":          map[string]any{},
			"trusted_directories": map[string]any{"additional": []any{}, "network": []any{}, "block_reads_outside_working_directories": true},
			"chrome_navigation":   map[string]any{"enabled": true, "baselines": map[string]any{}, "tabs": map[string]any{"tab": map[string]any{"url": "PRIVATE_URL", "title": "PRIVATE_TITLE"}}},
			"rules": map[string]any{
				"allow": []any{map[string]any{"rule": "Read(src/**)", "canonical": "Read(src/**)", "source": "managed", "root": nil}},
				"deny":  []any{map[string]any{"rule": "Bash(git push *)", "source": "managed", "pattern": "git push *"}},
				"ask":   []any{map[string]any{"rule": "Bash(rm *)", "source": "managed"}},
			},
			"auto_mode":  map[string]any{"allow": []string{"Read(*)"}, "soft_deny": []string{"Bash(git push *)"}, "hard_deny": []string{"Bash(rm -rf *)"}, "environment": []string{"Do not modify production"}},
			"restricted": true, "classify_all_shell": true, "artifact_consent_holdback": true,
			"is_remote_mode": false, "case_insensitive_paths": true, "mcp_allow_rules_exact": true,
			"truncated": true, "dropped": map[string]any{"allow": 2, "deny": 0},
			"shed": []string{"prior_turn_context"}, "judge_only_tools": []string{"Bash"}, "tool_host_attached": true,
			"prior_turn_context": []any{}, "system_reminders": []any{}, "artifacts": map[string]any{},
		},
	}
	var safeguards any = entry
	if array {
		safeguards = []any{entry, entry}
	}
	body, err := json.Marshal(map[string]any{
		"model": "claude-sonnet-4-6", "max_tokens": 128, "safeguards": safeguards,
		"messages": []any{map[string]any{"role": "user", "content": "Explain git remote and hash abc123"}},
		"tools":    []any{map[string]any{"name": "Bash", "input_schema": map[string]any{"type": "object"}}},
	})
	require.NoError(t, err)
	return body
}

func TestClaudePrivacySafeguardsRemoveMetadataPreserveSecurity(t *testing.T) {
	for _, array := range []bool{false, true} {
		name := "object"
		path := "safeguards.classifier_context"
		if array {
			name, path = "array", "safeguards.0.classifier_context"
		}
		t.Run(name, func(t *testing.T) {
			body := claudePrivacySafetySample(t, array)
			clean, err := sanitizeClaudePrivacyBody(body)
			require.NoError(t, err)
			require.NotContains(t, string(clean), "PRIVATE")
			require.Equal(t, gjson.Null, gjson.GetBytes(clean, path+".user_identity").Type)
			for _, field := range []string{"rules", "auto_mode", "restricted", "classify_all_shell", "artifact_consent_holdback", "permission_mode", "platform", "dropped", "truncated", "judge_only_tools", "shed", "mcp_allow_rules_exact", "tool_host_attached", "is_remote_mode", "case_insensitive_paths", "trusted_directories", "chrome_navigation.enabled", "chrome_navigation.baselines"} {
				require.Equal(t, gjson.GetBytes(body, path+"."+field).Raw, gjson.GetBytes(clean, path+"."+field).Raw, field)
			}
			for _, field := range []string{"messages", "tools"} {
				require.Equal(t, gjson.GetBytes(body, field).Raw, gjson.GetBytes(clean, field).Raw)
			}
			if array {
				require.Len(t, gjson.GetBytes(clean, "safeguards").Array(), 2)
				require.Equal(t, gjson.GetBytes(clean, "safeguards.0").Raw, gjson.GetBytes(clean, "safeguards.1").Raw)
			}
			again, err := sanitizeClaudePrivacyBody(clean)
			require.NoError(t, err)
			require.Equal(t, clean, again)
			early, err := SanitizeClaudePrivacySafeguards(body)
			require.NoError(t, err)
			require.Equal(t, clean, early)
		})
	}
}

func TestClaudePrivacySafeguardsRejectUnknownAmbiguousOrPolicyDependentContext(t *testing.T) {
	for name, value := range map[string]string{
		"null": `null`, "string": `"PRIVATE"`, "number": `1`,
		"array_scalar": `["PRIVATE"]`, "array_missing_type": `[{"classifier_context":{}}]`,
		"unknown_type":          `[{"type":"future_policy_PRIVATE"}]`,
		"context_not_object":    `{"classifier_context":[{"git_state":"PRIVATE"}]}`,
		"future_context":        `{"classifier_context":{"future_identity":"PRIVATE"}}`,
		"future_policy":         `{"classifier_context":{"restricted":true},"future_policy":"PRIVATE"}`,
		"duplicate_context":     `{"classifier_context":{},"classifier_context":{"git_state":"PRIVATE"}}`,
		"duplicate_mode":        `{"classifier_context":{"permission_mode":"auto","permission_mo\u0064e":"PRIVATE"}}`,
		"wrong_bool":            `{"classifier_context":{"restricted":"PRIVATE"}}`,
		"rule_roots":            `{"classifier_context":{"rule_roots":{"project":"PRIVATE"}}}`,
		"trusted_primary":       `{"classifier_context":{"trusted_directories":{"primary":{"path":"PRIVATE","resolved":[]}}}}`,
		"trusted_additional":    `{"classifier_context":{"trusted_directories":{"additional":[{"path":"PRIVATE"}]}}}`,
		"trusted_network":       `{"classifier_context":{"trusted_directories":{"network":["PRIVATE"]}}}`,
		"future_trusted_policy": `{"classifier_context":{"trusted_directories":{"future_policy":true}}}`,
		"browser_baselines":     `{"classifier_context":{"chrome_navigation":{"enabled":true,"baselines":{"PRIVATE":"PRIVATE"}}}}`,
		"future_browser_policy": `{"classifier_context":{"chrome_navigation":{"future_policy":true}}}`,
		"wrong_browser_tabs":    `{"classifier_context":{"chrome_navigation":{"tabs":"PRIVATE"}}}`,
		"rule_root":             `{"classifier_context":{"rules":{"deny":[{"rule":"Read(*)","root":"PRIVATE"}]}}}`,
		"duplicate_deny":        `{"classifier_context":{"rules":{"deny":[],"deny":[{"rule":"PRIVATE"}]}}}`,
		"unknown_rule":          `{"classifier_context":{"rules":{"deny":[{"rule":"Read(*)","future_rule":"PRIVATE"}]}}}`,
		"future_rule_category":  `{"classifier_context":{"rules":{"future_rule":[]}}}`,
		"future_auto_policy":    `{"classifier_context":{"auto_mode":{"future_allow":["PRIVATE"]}}}`,
		"wrong_auto_policy":     `{"classifier_context":{"auto_mode":{"hard_deny":[{"rule":"PRIVATE"}]}}}`,
		"prior_turn":            `{"classifier_context":{"prior_turn_context":[{"text":"PRIVATE"}]}}`,
		"reminders":             `{"classifier_context":{"system_reminders":["PRIVATE"]}}`,
		"artifacts":             `{"classifier_context":{"artifacts":{"by_path":{"PRIVATE":{}}}}}`,
		"future_version":        `{"classifier_context":{"v":2}}`,
		"wrong_count":           `{"classifier_context":{"dropped":{"deny":-1}}}`,
		"unknown_count":         `{"classifier_context":{"dropped":{"PRIVATE":1}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}],"safeguards":` + value + `}`)
			for _, filter := range []func([]byte) ([]byte, error){sanitizeClaudePrivacyBody, SanitizeClaudePrivacySafeguards} {
				clean, err := filter(body)
				require.Error(t, err)
				require.Nil(t, clean)
				require.NotContains(t, err.Error(), "PRIVATE")
			}
		})
	}
}

func TestClaudePrivacySafeguardsNoopAndEarlyBoundary(t *testing.T) {
	for _, body := range []string{
		`{ "model":"claude-sonnet-4-6", "messages":[{"role":"user","content":"git hash abc"}] }`,
		`{"safeguards":[]}`, `{"safeguards":{"enabled":false}}`,
		`{"safeguards":[{"type":"dangerous_tool_use","classifier_context":{"user_identity":null,"rules":{"deny":[{"rule":"Bash(git push *)","root":""}]}}}]}`,
		`{"model":"claude-sonnet-4-6","system":"x-anthropic-billing-header: cc_project=keep_until_existing_filter;","metadata":{"user_id":"session"}}`,
	} {
		clean, err := SanitizeClaudePrivacySafeguards([]byte(body))
		require.NoError(t, err)
		require.Equal(t, body, string(clean))
	}
	_, err := SanitizeClaudePrivacySafeguards([]byte(`{"safeguards":{},"safe\u0067uards":{"classifier_context":{"git_state":"PRIVATE"}}}`))
	require.Error(t, err)
}

func TestClaudePrivacySafeguardsOnWireAndRefusedBeforeUpstreamBuild(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, route := range []string{"oauth", "passthrough", "count"} {
		t.Run(route, func(t *testing.T) {
			build := func(body []byte) (*http.Request, error) {
				svc := &GatewayService{cfg: &config.Config{}}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
				account := &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeSetupToken}
				switch route {
				case "passthrough":
					req, _, err := svc.buildUpstreamRequestAnthropicAPIKeyPassthrough(context.Background(), c, newAnthropicAPIKeyAccountForTest(), body, "local-only")
					return req, err
				case "count":
					req, _, err := svc.buildCountTokensRequest(context.Background(), c, account, body, "local-only", "oauth", "claude-sonnet-4-6", false)
					return req, err
				default:
					req, _, err := svc.buildUpstreamRequest(context.Background(), c, account, body, "local-only", "oauth", "claude-sonnet-4-6", false, false)
					return req, err
				}
			}
			req, err := build(claudePrivacySafetySample(t, true))
			require.NoError(t, err)
			var received []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				received, _ = io.ReadAll(r.Body)
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()
			local, err := http.NewRequest(req.Method, server.URL, req.Body)
			require.NoError(t, err)
			response, err := server.Client().Do(local)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			require.NotContains(t, string(received), "PRIVATE")
			require.Contains(t, string(received), "Bash(git push *)")
			require.True(t, gjson.GetBytes(received, "safeguards.0.classifier_context.restricted").Bool())
			blocked, err := build([]byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}],"safeguards":[{"type":"dangerous_tool_use","classifier_context":{"rule_roots":{"project":"PRIVATE"}}}]}`))
			require.Error(t, err)
			require.Nil(t, blocked)
		})
	}
}

func TestClaudePrivacySafeguardsDebugLogOmissionOnRejectedContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway.log")
	file, err := os.Create(path)
	require.NoError(t, err)
	defer file.Close()
	svc := &GatewayService{}
	svc.debugGatewayBodyFile.Store(file)
	svc.debugLogGatewaySnapshot("CLIENT_ORIGINAL", http.Header{}, []byte(`{"safeguards":[{"type":"dangerous_tool_use","classifier_context":{"rule_roots":{"project":"PRIVATE"}}}]}`), nil)
	logged, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(logged), "PRIVATE")
	require.True(t, strings.Contains(string(logged), "invalid_envelope_omitted"))
}
