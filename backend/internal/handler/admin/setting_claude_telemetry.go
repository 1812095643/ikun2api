package admin

import (
	"github.com/MACOS-DO/sub4api/internal/pkg/response"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *SettingHandler) GetClaudeTelemetrySettings(c *gin.Context) {
	settings, err := h.settingService.GetClaudeTelemetrySettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}

func (h *SettingHandler) UpdateClaudeTelemetrySettings(c *gin.Context) {
	// Pointer fields distinguish an explicit false from an omitted field.
	var req struct {
		Enabled              *bool `json:"enabled" binding:"required"`
		FlushIntervalSeconds *int  `json:"flush_interval_seconds" binding:"required"`
		MinSendGapSeconds    *int  `json:"min_send_gap_seconds" binding:"required"`
		TimeoutSeconds       *int  `json:"timeout_seconds" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请提供完整的遥测设置")
		return
	}
	settings := service.ClaudeTelemetrySettings{Enabled: *req.Enabled, FlushIntervalSeconds: *req.FlushIntervalSeconds, MinSendGapSeconds: *req.MinSendGapSeconds, TimeoutSeconds: *req.TimeoutSeconds}
	if err := settings.Validate(); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if err := h.settingService.SetClaudeTelemetrySettings(c.Request.Context(), settings); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}
