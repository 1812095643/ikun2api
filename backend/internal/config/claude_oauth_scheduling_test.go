package config

import (
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestClaudeOAuthSchedulingConfigValidation(t *testing.T) {
	valid := GatewaySchedulingConfig{FallbackWaitAnyAccount: true, StickyExpectedHoldDefault: 10 * time.Second, NewSessionQuotaSoftPercent: 85, StickyCacheGraceMinBytes: 200000, StickyCacheGraceLargeBytes: 800000}
	require.NoError(t, validateClaudeOAuthScheduling(valid))
	for _, tc := range []struct {
		name  string
		alter func(*GatewaySchedulingConfig)
	}{
		{"negative quota", func(c *GatewaySchedulingConfig) { c.NewSessionQuotaSoftPercent = -1 }},
		{"quota over 100", func(c *GatewaySchedulingConfig) { c.NewSessionQuotaSoftPercent = 101 }},
		{"negative grace", func(c *GatewaySchedulingConfig) { c.StickyCacheGrace = -time.Second }},
		{"negative warm window", func(c *GatewaySchedulingConfig) { c.StickyCacheWarmWindow = -time.Second }},
		{"no default hold", func(c *GatewaySchedulingConfig) { c.StickyExpectedHoldDefault = 0 }},
		{"unordered sizes", func(c *GatewaySchedulingConfig) { c.StickyCacheGraceLargeBytes = 100000 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := valid
			tc.alter(&cfg)
			require.Error(t, validateClaudeOAuthScheduling(cfg))
			cfg.FallbackWaitAnyAccount = false
			require.NoError(t, validateClaudeOAuthScheduling(cfg))
		})
	}
}
