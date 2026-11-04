package handler

import (
	"github.com/MACOS-DO/sub4api/internal/pkg/response"
	middleware2 "github.com/MACOS-DO/sub4api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"strconv"
)

func (h *SubscriptionHandler) UniversalList(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "请先登录")
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 50
	}
	items, total, err := h.subscriptionService.Universal().List(c.Request.Context(), subject.UserID, (page-1)*size, size)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	for i := range items {
		items[i].UserEmail = ""
		items[i].UserName = ""
	}
	response.Success(c, gin.H{"items": items, "total": total, "page": page, "page_size": size})
}
