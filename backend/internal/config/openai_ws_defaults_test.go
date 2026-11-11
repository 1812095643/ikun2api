package config

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestLoadOpenAIWSPluginReplacementDefaults(t *testing.T) {
	resetViperWithJWTSecret(t)
	t.Setenv("GATEWAY_OPENAI_WS_PREWARM_GENERATE_ENABLED", "")
	cfg, err := Load()
	require.NoError(t, err)
	require.True(t, cfg.Gateway.OpenAIWS.Enabled)
	require.True(t, cfg.Gateway.OpenAIWS.OAuthEnabled)
	require.True(t, cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2)
	require.True(t, cfg.Gateway.OpenAIWS.PrewarmGenerateEnabled)
	require.True(t, cfg.Gateway.OpenAIWS.SearchInitEnabled)
	require.False(t, cfg.Gateway.OpenAIWS.ForceHTTP)
	t.Setenv("GATEWAY_OPENAI_WS_PREWARM_GENERATE_ENABLED", "false")
	cfg, err = Load()
	require.NoError(t, err)
	require.False(t, cfg.Gateway.OpenAIWS.PrewarmGenerateEnabled)
}
