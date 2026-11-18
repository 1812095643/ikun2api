package middleware

import (
	"net/http"
	"strings"
	"time"

	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
)

func IssueBridgePass(signer *service.BridgePassSigner) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		key, ok := GetAPIKeyFromContext(c)
		if !ok || key == nil {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		ticket, expires := signer.Issue(key.ID, key.Key, time.Now())
		if ticket == "" {
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return
		}
		c.JSON(http.StatusOK, gin.H{"ticket": ticket, "expires_at": expires.Unix(), "expires_in": int(service.BridgePassTTL / time.Second)})
	}
}

func BridgeGroupAccess() gin.HandlerFunc {
	return func(c *gin.Context) {
		key, _ := GetAPIKeyFromContext(c)
		if key != nil {
			if code := service.BridgeGroupDenied(c.Request.Context(), key.Group); code != "" {
				if attempt, _ := c.Request.Context().Value(smartRouteAttemptKey{}).(*smartRouteAttempt); attempt != nil {
					attempt.skipped = true
					attempt.bridgeDenied = true
				}
				service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalPolicyDenied)
				if strings.Contains(c.Request.URL.Path, "/v1beta/") {
					GoogleErrorWriter(c, http.StatusForbidden, service.BridgeRequiredMessage)
				} else {
					c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"type": "error", "error": gin.H{
						"type": "permission_error", "code": code, "message": service.BridgeRequiredMessage,
					}})
				}
				c.Abort()
				return
			}
		}
		c.Next()
	}
}
