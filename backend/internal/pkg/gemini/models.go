// Package gemini provides minimal fallback model metadata for Gemini native endpoints.
// It is used when upstream model listing is unavailable (e.g. OAuth token missing AI Studio scopes).
package gemini

import "strings"

type Model struct {
	Name                       string   `json:"name"`
	DisplayName                string   `json:"displayName,omitempty"`
	Description                string   `json:"description,omitempty"`
	InputTokenLimit            int      `json:"inputTokenLimit,omitempty"`
	OutputTokenLimit           int      `json:"outputTokenLimit,omitempty"`
	SupportedGenerationMethods []string `json:"supportedGenerationMethods,omitempty"`
}

type ModelsListResponse struct {
	Models []Model `json:"models"`
}

func DefaultModels() []Model {
	// 2026-11-24 对齐官方目录；当前同步协议不声明 long-decode 的 1M 输出能力。
	// https://aistudio.google.com/docs/models
	methods := []string{"generateContent", "streamGenerateContent"}
	return []Model{
		{Name: "models/gemini-4-neon", DisplayName: "Gemini 4 Neon", InputTokenLimit: 1048576, OutputTokenLimit: 65536, SupportedGenerationMethods: methods},
		{Name: "models/gemini-4-hydrogen", DisplayName: "Gemini 4 Hydrogen", InputTokenLimit: 1048576, OutputTokenLimit: 65536, SupportedGenerationMethods: methods},
		{Name: "models/gemini-nano-banana-2.1", DisplayName: "Nano Banana 2.1", InputTokenLimit: 131072, OutputTokenLimit: 32768, SupportedGenerationMethods: methods},
		{Name: "models/gemini-3.1-flash-image", SupportedGenerationMethods: methods},
		{Name: "models/gemini-3.1-flash-image-preview", SupportedGenerationMethods: methods},
		{Name: "models/gemini-3.1-flash-lite-image", SupportedGenerationMethods: methods},
		{Name: "models/gemini-3-pro-image", SupportedGenerationMethods: methods},
		{Name: "models/gemini-3-pro-image-preview", SupportedGenerationMethods: methods},
		{Name: "models/gemini-3-flash", SupportedGenerationMethods: methods},
		{Name: "models/gemini-3-flash-preview", SupportedGenerationMethods: methods},
		{Name: "models/gemini-3.1-pro-preview", SupportedGenerationMethods: methods},
		{Name: "models/gemini-3.1-pro-preview-customtools", SupportedGenerationMethods: methods},
		{Name: "models/gemini-3.1-flash-lite", SupportedGenerationMethods: methods},
		{Name: "models/gemini-3.1-flash-lite-preview", SupportedGenerationMethods: methods},
		{Name: "models/gemini-3.5-flash", SupportedGenerationMethods: methods},
		{Name: "models/gemini-3.5-flash-lite", SupportedGenerationMethods: methods},
		{Name: "models/gemini-3.6-flash", SupportedGenerationMethods: methods},
		{Name: "models/gemini-3.7-flash", SupportedGenerationMethods: methods},
		{Name: "models/gemini-3.8-flash", SupportedGenerationMethods: methods},
		{Name: "models/gemini-4-argon", DisplayName: "Gemini 4 Argon", InputTokenLimit: 1048576, OutputTokenLimit: 65536, SupportedGenerationMethods: methods},
		{Name: "models/gemini-4-carbon", DisplayName: "Gemini 4 Carbon", InputTokenLimit: 1048576, OutputTokenLimit: 65536, SupportedGenerationMethods: methods},
	}
}

func HasFallbackModel(model string) bool {
	trimmed := strings.TrimSpace(model)
	if trimmed == "" {
		return false
	}
	if !strings.HasPrefix(trimmed, "models/") {
		trimmed = "models/" + trimmed
	}
	for _, model := range DefaultModels() {
		if model.Name == trimmed {
			return true
		}
	}
	return false
}

func FallbackModelsList() ModelsListResponse {
	return ModelsListResponse{Models: DefaultModels()}
}

func FallbackModel(model string) Model {
	model = strings.TrimSpace(model)
	for _, known := range DefaultModels() {
		if known.Name == "models/"+strings.TrimPrefix(model, "models/") {
			return known
		}
	}
	methods := []string{"generateContent", "streamGenerateContent"}
	if model == "" {
		return Model{Name: "models/unknown", SupportedGenerationMethods: methods}
	}
	if len(model) >= 7 && model[:7] == "models/" {
		return Model{Name: model, SupportedGenerationMethods: methods}
	}
	return Model{Name: "models/" + model, SupportedGenerationMethods: methods}
}
