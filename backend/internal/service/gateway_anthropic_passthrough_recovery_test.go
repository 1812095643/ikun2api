//go:build unit

package service

import (
	"context"
	"github.com/MACOS-DO/sub4api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"testing"
	"time"
)

func TestGatewayService_AnthropicAPIKeyPassthrough_RepairsThinkingSignature(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &thinkingRecoveryUpstream{}
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
	settings := &signatureBudgetSettings{}
	svc := &GatewayService{
		cfg:                  cfg,
		responseHeaderFilter: compileResponseHeaderFilter(cfg),
		httpUpstream:         upstream,
		rateLimitService:     &RateLimitService{},
		deferredService:      &DeferredService{},
		settingService:       NewSettingService(settings, cfg),
	}
	account := newAnthropicAPIKeyAccountForTest()
	body := []byte(thinkingRecoveryFixture)
	c, _ := newThinkingRecoveryContext(body, "passthrough-session", 31)
	result, err := svc.forwardAnthropicAPIKeyPassthrough(context.Background(), c, account, body, "claude-opus-5-5", "claude-opus-5-5", false, time.Now())
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 2)
	require.Contains(t, string(upstream.bodies[0]), "rejected-signature")
	require.NotContains(t, string(upstream.bodies[1]), "rejected-signature")
	require.Equal(t, "adaptive", gjson.GetBytes(upstream.bodies[1], "thinking.type").String())

	continued, err := sjson.SetRaw(string(body), "messages.-1", `{"role":"user","content":"Continue"}`)
	require.NoError(t, err)
	c2, _ := newThinkingRecoveryContext([]byte(continued), "passthrough-session", 31)
	_, err = svc.forwardAnthropicAPIKeyPassthrough(context.Background(), c2, account, []byte(continued), "claude-opus-5-5", "claude-opus-5-5", false, time.Now())
	require.NoError(t, err)
	require.Len(t, upstream.bodies, 3)
	require.NotContains(t, string(upstream.bodies[2]), "rejected-signature")
}
