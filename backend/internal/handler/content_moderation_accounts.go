package handler

import (
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
)

func writeAccountContentModerationError(c *gin.Context, protocol string, decision *service.ContentModerationDecision) {
	if c == nil || decision == nil {
		return
	}
	status := contentModerationStatus(decision)
	service.SetOpsLocalErrorType(c, "content_policy_violation", "content_policy_violation")
	if c.Writer.Written() {
		(&GatewayHandler{}).handleStreamingAwareErrorWithCode(c, status, "content_policy_violation", "content_policy_violation", decision.Message, true)
		return
	}
	if protocol == service.ContentModerationProtocolGemini {
		googleErrorWithType(c, status, "content_policy_violation", "", decision.Message)
		return
	}
	payload := gin.H{"error": gin.H{"type": "content_policy_violation", "code": "content_policy_violation", "message": decision.Message}}
	if protocol == service.ContentModerationProtocolAnthropicMessages {
		payload["type"] = "error"
	}
	c.JSON(status, payload)
}
