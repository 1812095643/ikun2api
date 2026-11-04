package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MACOS-DO/sub4api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestClaudePrivacyToolProvenanceBetaUsesExactFinalCapability(t *testing.T) {
	body, err := sanitizeClaudePrivacyBody([]byte(claudeProvenanceBody))
	require.NoError(t, err)
	for _, tc := range []struct {
		name, beta string
		keep       bool
	}{
		{"missing", "", false},
		{"other", "oauth-2025-04-20", false},
		{"present", claudeToolProvenanceBeta, true},
		{"duplicate", "oauth-2025-04-20, " + claudeToolProvenanceBeta + ", " + claudeToolProvenanceBeta, true},
		{"prefix", claudeToolProvenanceBeta + "-private", false},
		{"wrong_case", "Tool-Provenance-2026-10-26", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clean, changed := sanitizeAnthropicBodyForBetaTokens(body, tc.beta)
			require.Equal(t, !tc.keep, changed)
			for _, path := range []string{"tools.0.ownership_metadata", "messages.2.content.1.tool.definition.ownership_metadata", "messages.3.content.0.tool.definition.ownership_metadata"} {
				require.Equal(t, tc.keep, gjson.GetBytes(clean, path).Exists(), path)
				if tc.keep {
					require.JSONEq(t, `{"verified":false,"owner":"mcp"}`, gjson.GetBytes(clean, path).Raw)
				}
			}
			for _, path := range []string{"messages.0", "messages.1", "messages.2.content.0", "tools.0.input_schema", "tools.0.description", "tools.0.name", "messages.2.content.1.tool.definition.name", "messages.2.content.1.tool.definition.input_schema"} {
				require.Equal(t, gjson.GetBytes(body, path).Raw, gjson.GetBytes(clean, path).Raw, path)
			}
			again, changedAgain := sanitizeAnthropicBodyForBetaTokens(clean, tc.beta)
			require.False(t, changedAgain)
			require.Equal(t, clean, again)
		})
	}
}

func TestClaudePrivacyToolProvenanceBetaFollowsAccountOverridesAndPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, route := range []string{"oauth", "oauth-mimic", "oauth-count", "oauth-count-mimic", "apikey", "passthrough", "passthrough-count", "native"} {
		for _, tc := range []struct {
			name, incoming string
			override       *string
			policyDrop     bool
		}{
			{name: "incoming", incoming: claudeToolProvenanceBeta},
			{name: "absent"},
			{name: "account_add", override: stringPtrForProvenance(claudeToolProvenanceBeta)},
			{name: "account_remove", incoming: claudeToolProvenanceBeta, override: stringPtrForProvenance("oauth-2025-04-20")},
			{name: "policy_drop", incoming: claudeToolProvenanceBeta, policyDrop: true},
		} {
			t.Run(route+"/"+tc.name, func(t *testing.T) {
				svc := &GatewayService{cfg: &config.Config{}}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
				c.Request.Header.Set("User-Agent", "claude-cli/2.1.318")
				c.Request.Header.Set("anthropic-beta", tc.incoming)
				if tc.policyDrop {
					c.Set(betaPolicyFilterSetKey, map[string]struct{}{claudeToolProvenanceBeta: {}})
				}
				account := newAnthropicAPIKeyAccountForTest()
				if tc.override != nil {
					account.Credentials[credKeyHeaderOverrideEnabled] = true
					account.Credentials[credKeyHeaderOverrides] = map[string]any{"anthropic-beta": *tc.override}
				}
				if route == "oauth" || route == "oauth-mimic" || route == "oauth-count" || route == "oauth-count-mimic" {
					account.Type = AccountTypeOAuth
				}
				body := []byte(claudeProvenanceBody)
				var req *http.Request
				var err error
				switch route {
				case "oauth", "oauth-mimic":
					req, _, err = svc.buildUpstreamRequest(context.Background(), c, account, body, "synthetic", "oauth", "claude-sonnet-4-6", false, route == "oauth-mimic")
				case "oauth-count", "oauth-count-mimic":
					req, _, err = svc.buildCountTokensRequest(context.Background(), c, account, body, "synthetic", "oauth", "claude-sonnet-4-6", route == "oauth-count-mimic")
				case "apikey":
					req, _, err = svc.buildUpstreamRequest(context.Background(), c, account, body, "synthetic", "apikey", "claude-sonnet-4-6", false, false)
				case "passthrough":
					req, _, err = svc.buildUpstreamRequestAnthropicAPIKeyPassthrough(context.Background(), c, account, body, "synthetic")
				case "passthrough-count":
					req, err = svc.buildCountTokensRequestAnthropicAPIKeyPassthrough(context.Background(), c, account, body, "synthetic")
				case "native":
					account.Platform = PlatformOpenAI
					req, _, err = (&OpenAIGatewayService{}).buildNativeAnthropicUpstreamRequest(context.Background(), c, account, body, "synthetic", "https://api.anthropic.com/v1/messages")
				}
				require.NoError(t, err)
				wire, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				require.NoError(t, req.Body.Close())
				finalBeta := getHeaderRaw(req.Header, "anthropic-beta")
				keep := anthropicBetaTokensContains(finalBeta, claudeToolProvenanceBeta)
				if route == "oauth-mimic" || tc.incoming == "" && account.Type == AccountTypeOAuth {
					require.False(t, keep, "must not introduce a beta absent from the established policy")
				}
				if tc.override != nil && account.Type == AccountTypeAPIKey {
					require.Equal(t, anthropicBetaTokensContains(*tc.override, claudeToolProvenanceBeta), keep, "account override must determine final capability")
				}
				if tc.policyDrop && (route == "oauth" || route == "oauth-count" || route == "apikey") {
					require.False(t, keep)
				}
				for _, path := range []string{"tools.0.ownership_metadata", "messages.2.content.1.tool.definition.ownership_metadata", "messages.3.content.0.tool.definition.ownership_metadata"} {
					require.Equal(t, keep, gjson.GetBytes(wire, path).Exists(), path)
				}
				require.NotContains(t, string(wire), "PRIVATE")
				require.Equal(t, "SIGNED-BYTES-UNCHANGED", gjson.GetBytes(wire, "messages.1.content.0.signature").String())
				require.Equal(t, gjson.GetBytes(body, "messages.1.content.1.input").Raw, gjson.GetBytes(wire, "messages.1.content.1.input").Raw)
			})
		}
	}
}

func stringPtrForProvenance(s string) *string { return &s }
