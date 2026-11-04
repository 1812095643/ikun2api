package admin

import (
	"github.com/MACOS-DO/sub4api/internal/pkg/response"
	middleware2 "github.com/MACOS-DO/sub4api/internal/server/middleware"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

func universalAdministrator(c *gin.Context) (int64, bool) {
	c.Header("Cache-Control", "no-store")
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "请先登录")
		return 0, false
	}
	method := c.GetString("auth_method")
	if method != service.AuditAuthMethodJWT && method != service.AuditAuthMethodPasskey {
		response.Forbidden(c, "通用订阅管理需要管理员网页登录会话")
		return 0, false
	}
	return subject.UserID, true
}
func universalPage(c *gin.Context) (int, int) {
	p, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	if p < 1 {
		p = 1
	}
	if size < 1 || size > 200 {
		size = 50
	}
	return p, size
}
func (h *SubscriptionHandler) UniversalList(c *gin.Context) {
	if _, ok := universalAdministrator(c); !ok {
		return
	}
	p, size := universalPage(c)
	userID, _ := strconv.ParseInt(c.Query("user_id"), 10, 64)
	if userID < 0 {
		response.BadRequest(c, "用户编号无效")
		return
	}
	items, total, err := h.subscriptionService.Universal().List(c.Request.Context(), userID, (p-1)*size, size)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"items": items, "total": total, "page": p, "page_size": size})
}
func (h *SubscriptionHandler) UniversalGrant(c *gin.Context) {
	actorID, ok := universalAdministrator(c)
	if !ok {
		return
	}
	var in struct {
		UserID      int64  `json:"user_id"`
		PlanID      int64  `json:"plan_id"`
		OperationID string `json:"operation_id"`
		Notes       string `json:"notes"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16384)
	if c.ShouldBindJSON(&in) != nil {
		response.BadRequest(c, "请检查发放资料")
		return
	}
	if len(in.OperationID) < 8 || len(in.OperationID) > 120 {
		response.BadRequest(c, "操作编号无效，请重新打开发放窗口")
		return
	}
	svc := h.subscriptionService.Universal()
	plan, err := svc.Plan(c.Request.Context(), in.PlanID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	out, err := svc.Grant(c.Request.Context(), in.UserID, service.UniversalSnapshot(plan), nil, &actorID, "admin:"+in.OperationID, in.Notes)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}
func (h *SubscriptionHandler) UniversalManage(c *gin.Context) {
	actorID, ok := universalAdministrator(c)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "订阅编号无效")
		return
	}
	var in struct {
		Version int64  `json:"version"`
		Status  string `json:"status"`
		Reason  string `json:"reason"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16384)
	if c.ShouldBindJSON(&in) != nil {
		response.BadRequest(c, "请填写操作原因")
		return
	}
	if err = h.subscriptionService.Universal().Manage(c.Request.Context(), id, in.Version, actorID, in.Status, in.Reason); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"success": true})
}
func (h *SubscriptionHandler) UniversalRecords(c *gin.Context) {
	if _, ok := universalAdministrator(c); !ok {
		return
	}
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if id <= 0 {
		response.BadRequest(c, "订阅编号无效")
		return
	}
	out, err := h.subscriptionService.Universal().Records(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}
func (h *SubscriptionHandler) UniversalResolveHold(c *gin.Context) {
	actorID, ok := universalAdministrator(c)
	if !ok {
		return
	}
	var in struct {
		Reason string `json:"reason"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16384)
	if c.ShouldBindJSON(&in) != nil {
		response.BadRequest(c, "请填写核对原因")
		return
	}
	if err := h.subscriptionService.Universal().ResolveHold(c.Request.Context(), c.Param("hold"), actorID, in.Reason); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"success": true})
}

func (h *SubscriptionHandler) UniversalAdjust(c *gin.Context) {
	actorID, ok := universalAdministrator(c)
	if !ok {
		return
	}
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var in service.UniversalSubscriptionAdjustment
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16384)
	if c.ShouldBindJSON(&in) != nil {
		response.BadRequest(c, "请检查调整内容")
		return
	}
	if err := h.subscriptionService.Universal().Adjust(c.Request.Context(), id, actorID, in); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"success": true})
}
