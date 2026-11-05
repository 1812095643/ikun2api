package handler

import (
	"github.com/MACOS-DO/sub4api/internal/pkg/response"
	"github.com/MACOS-DO/sub4api/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

func (h *PoolHandler) CreateShare(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	link, err := h.poolService.CreateShareLink(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, link)
}
func (h *PoolHandler) RevokeShare(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	if err := h.poolService.RevokeShareLink(c.Request.Context(), subject.UserID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"revoked": true})
}

// This authentication is registered only on the single read-only share route.
// It must never be used as general JWT or API-key authentication.
func (h *PoolHandler) ShareAuth(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Robots-Tag", "noindex, noarchive")
	userID, err := h.poolService.ResolveShareLink(c.Request.Context(), c.GetHeader("X-Pool-Share-Token"))
	if err != nil {
		response.ErrorFrom(c, err)
		c.Abort()
		return
	}
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: userID})
	c.Next()
}
