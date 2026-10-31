package routes

import (
	"strings"

	"github.com/MACOS-DO/sub4api/internal/handler"
	"github.com/MACOS-DO/sub4api/internal/pkg/claude"
	"github.com/MACOS-DO/sub4api/internal/server/middleware"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
)

// RegisterClaudeTelemetryRoutes 单独终止遥测请求，不进入模型调度、计费或正文审计。
func RegisterClaudeTelemetryRoutes(r *gin.Engine, h *handler.GatewayHandler, apiKeyAuth middleware.APIKeyAuthMiddleware) {
	limit := middleware.RequestBodyLimit(service.MaxClaudeTelemetryBodyBytes)
	for _, prefix := range []string{"", "/v1"} {
		r.POST(prefix+"/api/event_logging/*path", limit, func(c *gin.Context) {
			path := c.Param("path")
			if path != "/batch" && path != "/v2/batch" {
				h.DiscardClaudeTelemetry(c)
				return
			}
			// 官方客户端存在无认证遥测：只能丢弃，不能按载荷里的账号字段选择上游账号。
			if strings.TrimSpace(c.GetHeader("Authorization")) == "" && strings.TrimSpace(c.GetHeader("x-api-key")) == "" {
				h.DiscardClaudeTelemetry(c)
				return
			}
			if apiKeyAuth == nil {
				h.DiscardClaudeTelemetry(c)
				return
			}
			apiKeyAuth(c)
			if !c.IsAborted() {
				h.ClaudeTelemetryBatch(c)
			}
		})
	}
	for _, path := range claude.DiscardedTelemetryPaths() {
		r.POST(path, limit, h.DiscardClaudeTelemetry)
	}
}
