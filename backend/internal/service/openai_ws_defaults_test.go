package service

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestOpenAIOAuthWSDefaults(t *testing.T) {
	for _, tc := range []struct {
		name     string
		extra    map[string]any
		expected string
	}{
		{name: "missing", expected: OpenAIWSIngressModeCtxPool},
		{name: "unrelated", extra: map[string]any{"codex_fingerprint_mode": "device"}, expected: OpenAIWSIngressModeCtxPool},
		{name: "explicit off", extra: map[string]any{"openai_oauth_responses_websockets_v2_mode": "off"}, expected: OpenAIWSIngressModeOff},
		{name: "legacy false", extra: map[string]any{"openai_ws_enabled": false}, expected: OpenAIWSIngressModeOff},
		{name: "passthrough", extra: map[string]any{"openai_oauth_responses_websockets_v2_mode": "passthrough"}, expected: OpenAIWSIngressModePassthrough},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := prepareOpenAIOAuthWSExtraForCreate(PlatformOpenAI, AccountTypeOAuth, tc.extra)
			require.Equal(t, tc.expected, result["openai_oauth_responses_websockets_v2_mode"])
			require.Equal(t, tc.expected != OpenAIWSIngressModeOff, result["openai_oauth_responses_websockets_v2_enabled"])
			if tc.extra != nil {
				require.NotContains(t, tc.extra, "openai_oauth_responses_websockets_v2_enabled")
			}
		})
	}
	require.Nil(t, prepareOpenAIOAuthWSExtraForCreate(PlatformOpenAI, AccountTypeAPIKey, nil))
	require.Nil(t, prepareOpenAIOAuthWSExtraForCreate(PlatformAnthropic, AccountTypeOAuth, nil))
}
