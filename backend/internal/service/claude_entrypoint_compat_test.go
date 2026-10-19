package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/config"
	"github.com/MACOS-DO/sub4api/internal/pkg/claude"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 撤掉本地采样入口白名单后，旧开关也不能再次阻断原有的 CLI、SDK、桌面和兼容入口。
func TestClaudeOriginalEntrypointsRemainAccepted(t *testing.T) {
	t.Setenv("SUB6_CLAUDE_CAPTURED_IDENTITY_ENABLED", "true")
	claude.SetCLIVersionResolver(func() string { return "2.1.299" })
	t.Cleanup(func() { claude.SetCLIVersionResolver(nil) })
	for _, endpoint := range []string{"messages", "count_tokens"} {
		for _, scenario := range []struct {
			name, userAgent, entry, operatingSystem string
			mimic                                   bool
		}{
			{name: "interactive_cli", userAgent: "claude-cli/2.1.299 (external, cli)", entry: "cli", operatingSystem: "Linux"},
			{name: "native_sdk", userAgent: "claude-cli/2.1.299 (external, sdk-cli)", entry: "sdk-cli", operatingSystem: "Linux"},
			{name: "desktop_entry", userAgent: "claude-cli/2.1.299 (external, claude-desktop-3p, agent-sdk/0.3.299)", entry: "cli", operatingSystem: "Linux"},
			{name: "windows_cli", userAgent: "claude-cli/2.1.299 (external, cli)", entry: "cli", operatingSystem: "Windows"},
			{name: "original_compatibility", userAgent: "python-httpx/0.28.1", entry: "cli", operatingSystem: "", mimic: true},
		} {
			t.Run(endpoint+"/"+scenario.name, func(t *testing.T) {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
				c.Request.Header.Set("User-Agent", scenario.userAgent)
				setHeaderRaw(c.Request.Header, "X-Stainless-OS", scenario.operatingSystem)
				body, err := json.Marshal(map[string]any{
					"model": "claude-haiku-5-5", "max_tokens": 16,
					"system":   []any{map[string]string{"type": "text", "text": "x-anthropic-billing-header: cc_version=2.1.299.abc; cc_entrypoint=" + scenario.entry + ";"}},
					"messages": []any{map[string]string{"role": "user", "content": "offline compatibility check"}},
				})
				require.NoError(t, err)
				fp := defaultFingerprint()
				fp.ClientID, fp.UpdatedAt = "stable-existing-device", time.Now().Unix()
				cache := &stubIdentityCache{fingerprint: &fp}
				gateway := &GatewayService{identityService: NewIdentityService(cache)}
				account := &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth}
				var req *http.Request
				if endpoint == "messages" {
					req, _, err = gateway.buildUpstreamRequest(context.Background(), c, account, body, "offline-only", "oauth", "claude-haiku-5-5", false, scenario.mimic)
				} else {
					req, _, err = gateway.buildCountTokensRequest(context.Background(), c, account, body, "offline-only", "oauth", "claude-haiku-5-5", scenario.mimic)
				}
				require.NoError(t, err)
				require.NotNil(t, req)
				require.Equal(t, "Bearer offline-only", getHeaderRaw(req.Header, "authorization"))
				require.Equal(t, "2.1.299", ExtractCLIVersion(getHeaderRaw(req.Header, "User-Agent")))
				require.Equal(t, "stable-existing-device", cache.fingerprint.ClientID)
			})
		}
	}
}

func TestClaudeEntrypointCleanupKeepsAPIKeyPath(t *testing.T) {
	t.Setenv("SUB6_CLAUDE_CAPTURED_IDENTITY_ENABLED", "true")
	cache := &stubIdentityCache{}
	gateway := &GatewayService{identityService: NewIdentityService(cache), cfg: &config.Config{}}
	account := &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeAPIKey}
	body := []byte(`{"model":"claude-haiku-5-5","max_tokens":16,"messages":[{"role":"user","content":"offline check"}]}`)
	for _, countTokens := range []bool{false, true} {
		var req *http.Request
		var err error
		if countTokens {
			req, _, err = gateway.buildCountTokensRequest(context.Background(), nil, account, body, "offline-api-key", "apikey", "claude-haiku-5-5", false)
		} else {
			req, _, err = gateway.buildUpstreamRequest(context.Background(), nil, account, body, "offline-api-key", "apikey", "claude-haiku-5-5", false, false)
		}
		require.NoError(t, err)
		require.Equal(t, "offline-api-key", getHeaderRaw(req.Header, "x-api-key"))
	}
	require.Zero(t, cache.setCalls)
}
