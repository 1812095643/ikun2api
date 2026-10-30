package middleware

import (
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
	"net/http"
	"strings"
)

func UpgradeMaintenance() gin.HandlerFunc {
	return func(c *gin.Context) {
		p := c.Request.URL.Path
		if strings.HasPrefix(p, "/api/v1/admin/system/") || p == "/health" {
			c.Next()
			return
		}
		if !service.EnterUpgradeRequest() {
			c.Header("Retry-After", "10")
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"code": 503, "message": "系统正在更新，请稍后重试", "reason": "UPGRADE_MAINTENANCE"})
			return
		}
		defer service.LeaveUpgradeRequest()
		c.Next()
	}
}
