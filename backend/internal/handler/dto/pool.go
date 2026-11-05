package dto

import (
	"time"

	"github.com/MACOS-DO/sub4api/internal/service"
)

// UserPoolUsageLog exposes only the current user's request display fields.
// Account, key, group identifiers, credentials and billing fields are not included.
type UserPoolUsageLog struct {
	Name                  string    `json:"name"`
	Model                 string    `json:"model"`
	ReasoningEffort       *string   `json:"reasoning_effort"`
	InboundEndpoint       *string   `json:"inbound_endpoint"`
	GroupName             string    `json:"group_name"`
	CreatedAt             time.Time `json:"created_at"`
	DurationMs            *int      `json:"duration_ms"`
	FirstTokenMs          *int      `json:"first_token_ms"`
	Stream                bool      `json:"stream"`
	InputTokens           int       `json:"input_tokens"`
	OutputTokens          int       `json:"output_tokens"`
	CacheReadTokens       int       `json:"cache_read_tokens"`
	CacheCreationTokens   int       `json:"cache_creation_tokens"`
	CacheCreation5mTokens int       `json:"cache_creation_5m_tokens"`
	CacheCreation1hTokens int       `json:"cache_creation_1h_tokens"`
	ImageInputTokens      int       `json:"image_input_tokens"`
	ImageOutputTokens     int       `json:"image_output_tokens"`
}

func UserPoolUsageLogFromService(item *service.UsageLog) UserPoolUsageLog {
	name := service.PoolAccountEmailLabel(item.Account, item.AccountNameSnapshot)
	model := item.RequestedModel
	if model == "" {
		model = item.Model
	}
	groupName := ""
	if item.Group != nil {
		groupName = item.Group.Name
	}
	return UserPoolUsageLog{
		Name: name, Model: model, GroupName: groupName,
		ReasoningEffort: userFacingReasoningEffort(item), InboundEndpoint: item.InboundEndpoint,
		CreatedAt: item.CreatedAt, DurationMs: item.DurationMs, FirstTokenMs: item.FirstTokenMs,
		Stream: item.Stream, InputTokens: item.InputTokens, OutputTokens: item.OutputTokens,
		CacheReadTokens: item.CacheReadTokens, CacheCreationTokens: item.CacheCreationTokens,
		CacheCreation5mTokens: item.CacheCreation5mTokens, CacheCreation1hTokens: item.CacheCreation1hTokens,
		ImageInputTokens: item.ImageInputTokens, ImageOutputTokens: item.ImageOutputTokens,
	}
}
