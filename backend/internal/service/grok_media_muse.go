package service

import (
	"context"
	"log/slog"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// This compatibility rule is deliberately limited to the observed Muse relay.
// Do not infer a provider from an editable account name, video URL or response model.
func isMuseVideoRelayAccount(account *Account) bool {
	if account == nil || account.Platform != PlatformGrok || account.Type != AccountTypeAPIKey {
		return false
	}
	baseURL, err := url.Parse(strings.TrimSpace(account.GetGrokMediaBaseURL()))
	return err == nil && strings.EqualFold(baseURL.Scheme, "https") &&
		strings.EqualFold(baseURL.Hostname(), "ai.windking.shop") &&
		(baseURL.Port() == "" || baseURL.Port() == "443") && baseURL.User == nil &&
		baseURL.RawQuery == "" && baseURL.Fragment == ""
}

// Muse uses completed instead of the official done enum. Only a tenant-scoped
// create snapshot for muse-video can enable this alias; all other providers and
// models retain ExtractGrokVideoBillingFromStatusBody's existing done-only rule.
func (s *OpenAIGatewayService) extractMuseVideoCompletedBilling(
	ctx context.Context, c *gin.Context, account *Account, body []byte, requestID string,
) *OpenAIForwardResult {
	if s == nil || c == nil || !isMuseVideoRelayAccount(account) || !gjson.ValidBytes(body) ||
		!strings.EqualFold(strings.TrimSpace(gjson.GetBytes(body, "status").String()), "completed") {
		return nil
	}
	videoURL := gjson.GetBytes(body, "video.url")
	if videoURL.Type != gjson.String || strings.TrimSpace(videoURL.String()) == "" {
		return nil
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return nil
	}
	if responseID := extractGrokMediaVideoRequestID(body); responseID != "" && responseID != requestID {
		return nil
	}
	value, _ := c.Get("api_key")
	apiKey, ok := value.(*APIKey)
	if !ok || apiKey == nil || apiKey.UserID <= 0 || apiKey.ID <= 0 {
		return nil
	}
	pending, err := s.LoadGrokVideoPendingBilling(ctx, requestID, apiKey.UserID, apiKey.ID)
	if err != nil || pending == nil {
		slog.Warn("Muse video completion billing skipped", "request_id", requestID, "reason", "create_snapshot_unavailable")
		return nil
	}
	museModel := false
	for _, model := range []string{pending.Model, pending.BillingModel, pending.UpstreamModel, pending.OriginalModel} {
		if strings.EqualFold(strings.TrimSpace(model), "muse-video") {
			museModel = true
			break
		}
	}
	if !museModel {
		return nil
	}
	// Normalize a separate billing copy only. Preserve the upstream status returned
	// to the client and reuse the existing duration/model extraction and claim path.
	normalized, err := sjson.SetBytes(body, "status", "done")
	if err != nil {
		return nil
	}
	return ExtractGrokVideoBillingFromStatusBody(normalized, pending, requestID)
}
