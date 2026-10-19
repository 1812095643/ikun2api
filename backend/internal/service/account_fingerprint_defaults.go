package service

// 新建 Claude 订阅账号默认使用本机实采 TLS 模板；显式关闭仍然有效。
// 复用原有 Codex 种子生成逻辑，避免改变设备身份与会话隔离规则。
func prepareAccountFingerprintExtraForCreate(platform, accountType string, extra map[string]any) map[string]any {
	prepared := prepareCodexFingerprintExtraForCreate(platform, accountType, extra)
	if platform != PlatformAnthropic || (accountType != AccountTypeOAuth && accountType != AccountTypeSetupToken) {
		return prepared
	}
	if _, configured := prepared["enable_tls_fingerprint"]; configured {
		return prepared
	}
	if prepared == nil {
		prepared = make(map[string]any)
	}
	prepared["enable_tls_fingerprint"] = true
	return prepared
}
