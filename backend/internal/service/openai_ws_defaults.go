package service

// New OAuth accounts use the native WS pool after the plugin was integrated.
// Explicit per-account choices, including legacy false/off values, are retained.
func prepareOpenAIOAuthWSExtraForCreate(platform, accountType string, extra map[string]any) map[string]any {
	if platform != PlatformOpenAI || accountType != AccountTypeOAuth {
		return extra
	}
	account := &Account{Platform: platform, Type: accountType, Extra: extra}
	mode := account.ResolveOpenAIResponsesWebSocketV2Mode(OpenAIWSIngressModeCtxPool)
	next := make(map[string]any, len(extra)+2)
	for key, value := range extra {
		next[key] = value
	}
	next["openai_oauth_responses_websockets_v2_mode"] = mode
	next["openai_oauth_responses_websockets_v2_enabled"] = mode != OpenAIWSIngressModeOff
	return next
}
