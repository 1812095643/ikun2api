package routes

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// RegisterCommonRoutes 注册通用路由（健康检查、状态等）
func RegisterCommonRoutes(r *gin.Engine) {
	r.Use(func(c *gin.Context) {
		if c.Request.URL.Path == "/share/pool" {
			c.Header("Cache-Control", "no-store")
			c.Header("Referrer-Policy", "no-referrer")
			c.Header("X-Robots-Tag", "noindex, noarchive")
		}
		c.Next()
	})
	// 健康检查
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Setup status endpoint (always returns needs_setup: false in normal mode)
	// This is used by the frontend to detect when the service has restarted after setup
	r.GET("/setup/status", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"data": gin.H{
				"needs_setup": false,
				"step":        "completed",
			},
		})
	})
}
