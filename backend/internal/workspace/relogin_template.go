package workspace

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/MACOS-DO/sub4api/internal/service"
)

type accountTemplateExtra struct {
	CodexFingerprintMode      string `json:"codex_fingerprint_mode"`
	LongContextBillingEnabled bool   `json:"openai_long_context_billing_enabled"`
	OpenAIWSMode              string `json:"openai_oauth_responses_websockets_v2_mode"`
	OpenAIWSEnabled           bool   `json:"openai_oauth_responses_websockets_v2_enabled"`
}

func defaultReloginConfig() reloginConfig {
	load := 10000
	return reloginConfig{
		Template: accountTemplate{
			GroupIDs: []int64{}, Concurrency: 100, LoadFactor: &load, Priority: 1,
			RateMultiplier: 1, AutoPauseOnExpired: true, ModelMapping: map[string]string{},
			Extra: accountTemplateExtra{CodexFingerprintMode: "full", LongContextBillingEnabled: true,
				OpenAIWSMode: service.OpenAIWSIngressModeCtxPool, OpenAIWSEnabled: true},
		},
		QuotaInterval: 300, QuotaThreshold: 20, Recipients: []string{},
	}
}

func normalizeReloginTemplate(t *accountTemplate) error {
	if t.LoadFactor != nil && (*t.LoadFactor < 1 || *t.LoadFactor > 10000) {
		return badRequest("负载系数应为 1 至 10000。")
	}
	switch t.Extra.CodexFingerprintMode {
	case "off", "device", "session", "full":
	default:
		return badRequest("请选择有效的收敛档位。")
	}
	switch t.Extra.OpenAIWSMode {
	case service.OpenAIWSIngressModeOff, service.OpenAIWSIngressModeCtxPool,
		service.OpenAIWSIngressModePassthrough, service.OpenAIWSIngressModeHTTPBridge:
	default:
		return badRequest("请选择有效的 WebSocket 模式。")
	}
	t.Extra.OpenAIWSEnabled = t.Extra.OpenAIWSMode != service.OpenAIWSIngressModeOff
	if t.ModelMapping == nil {
		t.ModelMapping = map[string]string{}
	}
	for from, to := range t.ModelMapping {
		if strings.TrimSpace(from) == "" || strings.TrimSpace(to) == "" {
			return badRequest("模型映射的来源和目标均不能为空。")
		}
	}
	return nil
}

func hasCompleteReloginTemplate(raw json.RawMessage) bool {
	var s struct {
		Template map[string]json.RawMessage `json:"template"`
	}
	if json.Unmarshal(raw, &s) != nil {
		return false
	}
	for _, key := range []string{"load_factor", "auto_pause_on_expired", "extra", "model_mapping"} {
		if _, ok := s.Template[key]; !ok {
			return false
		}
	}
	return true
}

// Recover only template settings from the encrypted migration archive. Login
// credentials and historical results are never materialized into a response.
func (h *Handler) legacyReloginTemplate(ctx context.Context) (json.RawMessage, error) {
	var envelope struct {
		Ciphertext string `json:"ciphertext"`
	}
	if err := h.getRuntime(ctx, "migration43_legacy_relogin_archive", &envelope); err != nil {
		return nil, err
	}
	if envelope.Ciphertext == "" {
		return nil, nil
	}
	plaintext, err := h.encryptor.Decrypt(envelope.Ciphertext)
	if err != nil {
		return nil, err
	}
	var archive struct {
		Runtime []struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		} `json:"runtime"`
	}
	if err := json.Unmarshal([]byte(plaintext), &archive); err != nil {
		return nil, err
	}
	for _, row := range archive.Runtime {
		if row.Key == "account_template" {
			return json.RawMessage(row.Value), nil
		}
	}
	return nil, nil
}

func (t accountTemplate) createInput(email string, credentials map[string]any) *service.CreateAccountInput {
	copyCredentials := make(map[string]any, len(credentials)+1)
	for key, value := range credentials {
		copyCredentials[key] = value
	}
	credentials = copyCredentials
	if len(t.ModelMapping) > 0 {
		mapping := make(map[string]any, len(t.ModelMapping))
		for from, to := range t.ModelMapping {
			mapping[from] = to
		}
		credentials["model_mapping"] = mapping
	}
	note := "由原生失效重登创建"
	return &service.CreateAccountInput{
		Name: email, Notes: &note, Platform: "openai", Type: "oauth", Credentials: credentials,
		ProxyID: t.ProxyID, Concurrency: t.Concurrency, LoadFactor: t.LoadFactor,
		Priority: t.Priority, RateMultiplier: &t.RateMultiplier, GroupIDs: t.GroupIDs,
		AutoPauseOnExpired: &t.AutoPauseOnExpired, SkipDefaultGroupBind: len(t.GroupIDs) == 0,
		Extra: map[string]any{
			"codex_fingerprint_mode":                       t.Extra.CodexFingerprintMode,
			"openai_long_context_billing_enabled":          t.Extra.LongContextBillingEnabled,
			"openai_oauth_responses_websockets_v2_mode":    t.Extra.OpenAIWSMode,
			"openai_oauth_responses_websockets_v2_enabled": t.Extra.OpenAIWSMode != service.OpenAIWSIngressModeOff,
		},
	}
}
