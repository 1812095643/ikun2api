package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MACOS-DO/sub4api/internal/config"
	"github.com/MACOS-DO/sub4api/internal/pkg/claude"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/sjson"
)

// withCLIVersionResolverForTest 注入解析器并在用例结束时还原（置 nil），避免用例间污染。
func withCLIVersionResolverForTest(t *testing.T, resolver func() string) {
	t.Helper()
	claude.SetCLIVersionResolver(resolver)
	t.Cleanup(func() { claude.SetCLIVersionResolver(nil) })
}

// 运行期版本继续用于已有账户指纹，出站隐私策略始终移除 billing 与内容哈希。
func TestBuildOAuthMimicRequest_UnreviewedVersionCannotOverrideProfile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const upgraded = "9.9.9"
	withCLIVersionResolverForTest(t, func() string { return upgraded })

	for _, endpoint := range []string{"messages", "count_tokens"} {
		t.Run(endpoint, func(t *testing.T) {
			resetGatewayForwardingSettingsCacheForTest(t)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			body := []byte(`{"model":"claude-haiku-4-5","system":[{"type":"text","text":""}],"messages":[{"role":"user","content":"hello world"}]}`)
			billing, err := buildBillingAttributionText(body, "2.1.81")
			require.NoError(t, err)
			body, err = sjson.SetBytes(body, "system.0.text", billing)
			require.NoError(t, err)

			svc := &GatewayService{cfg: &config.Config{}}
			account := &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth}

			var req *http.Request
			var wireBody []byte
			if endpoint == "messages" {
				req, wireBody, err = svc.buildUpstreamRequest(context.Background(), c, account,
					body, "test-token", "oauth", "claude-haiku-4-5", false, true)
			} else {
				req, wireBody, err = svc.buildCountTokensRequest(context.Background(), c, account,
					body, "test-token", "oauth", "claude-haiku-4-5", true)
			}
			require.NoError(t, err)

			wantUA := "claude-cli/" + claude.DefaultProfileVersion + " (external, cli)"
			outboundUA := getHeaderRaw(req.Header, "User-Agent")
			require.Equal(t, wantUA, outboundUA)

			require.NotContains(t, string(wireBody), "x-anthropic-billing-header:")
			require.NotContains(t, string(wireBody), "cc_version=")
			require.Equal(t, claude.DefaultProfileVersion, ExtractCLIVersion(outboundUA))
		})
	}
}
