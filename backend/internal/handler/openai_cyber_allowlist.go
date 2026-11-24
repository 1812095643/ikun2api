package handler

import (
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *OpenAIGatewayHandler) cyberPolicyLogOnly(c *gin.Context, apiKey *service.APIKey) bool {
	if h != nil && c != nil && c.Request != nil && h.contentModerationService.IsAccountAuditExempt(c.Request.Context(), c.GetInt64(opsAccountIDKey)) {
		return true
	}
	return h != nil && c != nil && c.Request != nil && h.gatewayService.CyberPolicyLogOnly(c.Request.Context(), apiKey)
}

// Both HTTP and WebSocket admission skip existing blocks for trusted users.
func (h *OpenAIGatewayHandler) findBlockedCyberSessionForAPIKey(c *gin.Context, apiKey *service.APIKey, body []byte) string {
	if apiKey == nil || h.cyberPolicyLogOnly(c, apiKey) {
		return ""
	}
	return findBlockedCyberSessionKey(c.Request.Context(), h.gatewayService, apiKey.ID, c, body)
}
