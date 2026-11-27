package middleware

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
)

type bridgePassRequestKey struct{}

type bridgePassRequest struct {
	signer *service.BridgePassSigner
	ticket string
}

func captureBridgePass(c *gin.Context, signer *service.BridgePassSigner) {
	values := c.Request.Header.Values(service.BridgePassHeader)
	if len(values) == 0 {
		return
	}
	pass := bridgePassRequest{signer: signer}
	if len(values) == 1 {
		pass.ticket = values[0]
	}
	// 入口只暂存并移除票据；凭据解析由正式鉴权负责，避免两条链路判定不一致。
	c.Request.Header.Del(service.BridgePassHeader)
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), bridgePassRequestKey{}, pass))
}

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
			if pass, ok := c.Request.Context().Value(bridgePassRequestKey{}).(bridgePassRequest); ok {
				// 智能路由可能切换分组，但票据始终绑定当前已认证的 Key，不重新猜测请求凭据。
				state := pass.signer.Verify(pass.ticket, key.ID, key.Key, time.Now())
				c.Request = c.Request.WithContext(service.WithBridgePassState(c.Request.Context(), state))
			}
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
