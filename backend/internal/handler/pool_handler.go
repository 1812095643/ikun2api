package handler

import (
	"github.com/MACOS-DO/sub4api/internal/pkg/pagination"
	"github.com/MACOS-DO/sub4api/internal/pkg/usagestats"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/MACOS-DO/sub4api/internal/pkg/response"
	"github.com/MACOS-DO/sub4api/internal/pkg/timezone"
	middleware2 "github.com/MACOS-DO/sub4api/internal/server/middleware"
	"github.com/MACOS-DO/sub4api/internal/service"
)

// PoolHandler 提供「实时号池」的用户端只读接口。
//
// 与 admin.PoolHandler 的关系：
//   - 同一份底层数据（账号表 + usage_logs），同一套聚合服务。
//   - 差异在脱敏：这里的每个查询都被强制钉上 subject.UserID，
//     用户拿不到别人的账号、别人的用量，也拿不到账号在全站的消耗。
//   - 显式 user_id 越界返回 403；账号查询始终限定本人用量，没有本人记录就返回空集。
type PoolHandler struct {
	poolService  *service.AccountPoolService
	usageService *service.UsageService
}

// NewPoolHandler 创建用户端号池 handler。
func NewPoolHandler(poolService *service.AccountPoolService, usageService *service.UsageService) *PoolHandler {
	return &PoolHandler{
		poolService:  poolService,
		usageService: usageService,
	}
}

// Overview 返回当前用户自己的号池概览（脱敏）。
// GET /api/v1/pool/overview
func (h *PoolHandler) Overview(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	if !userPoolValidateUserID(c, subject.UserID) {
		return
	}
	overview, err := h.poolService.OverviewForUser(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"account_count": overview.AccountCount, "active_count": overview.ActiveCount, "generated_at": overview.GeneratedAt})
}

// Accounts 返回当前用户用过的账号列表（脱敏 + 本人今日用量）。
// GET /api/v1/pool/accounts
func (h *PoolHandler) Accounts(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	if !userPoolValidateUserID(c, subject.UserID) {
		return
	}
	page, pageSize := response.ParsePagination(c)

	filter := service.PoolAccountFilter{
		Page:     page,
		PageSize: pageSize,
		Platform: strings.TrimSpace(c.Query("platform")),
		Type:     strings.TrimSpace(c.Query("type")),
		Status:   strings.TrimSpace(c.Query("status")),
		Search:   strings.TrimSpace(c.Query("search")),
	}
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 {
		filter.PageSize = 20
	}
	if filter.PageSize > 200 {
		filter.PageSize = 200
	}

	result, err := h.poolService.ListAccountsForUser(c.Request.Context(), subject.UserID, filter)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Paginated(c, service.PoolPublicAccounts(result.Items), result.Total, result.Page, result.PageSize)
}

// AccountRequests 返回当前用户在指定账号上的近期请求流水。
// GET /api/v1/pool/accounts/:id/requests
func (h *PoolHandler) AccountRequests(c *gin.Context) { h.userDetailsDisabled(c) }
func (h *PoolHandler) Usage(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	if !userPoolValidateUserID(c, subject.UserID) {
		return
	}
	groupIDs, err := h.poolService.UserPoolVisibleGroupIDs(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	start, end, ok := userPoolParseTimeRange(c)
	if !ok {
		return
	}
	page, size := response.ParsePagination(c)
	if size > 100 {
		size = 100
	}
	filters := usagestats.UsageLogFilters{UserID: subject.UserID, GroupIDs: groupIDs, StartTime: start, EndTime: end, Model: strings.TrimSpace(c.Query("model"))}
	records, result, err := h.usageService.ListWithFilters(c.Request.Context(), pagination.PaginationParams{Page: page, PageSize: size, SortBy: "created_at", SortOrder: "desc"}, filters)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	type record struct {
		Name       string    `json:"name"`
		Model      string    `json:"model"`
		CreatedAt  time.Time `json:"created_at"`
		DurationMs *int      `json:"duration_ms"`
		Stream     bool      `json:"stream"`
	}
	items := make([]record, 0, len(records))
	for _, item := range records {
		name := "—"
		if item.AccountNameSnapshot != nil {
			name = service.MaskAccountName(*item.AccountNameSnapshot)
		} else if item.Account != nil {
			name = service.MaskAccountName(item.Account.Name)
		}
		model := item.RequestedModel
		if model == "" {
			model = item.Model
		}
		items = append(items, record{Name: name, Model: model, CreatedAt: item.CreatedAt, DurationMs: item.DurationMs, Stream: item.Stream})
	}
	c.Header("Cache-Control", "no-store")
	response.Paginated(c, items, result.Total, result.Page, result.PageSize)
}
func (h *PoolHandler) userDetailsDisabled(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	if !userPoolValidateUserID(c, subject.UserID) {
		return
	}
	c.Header("Cache-Control", "no-store")
	response.ErrorWithDetails(c, 403, "用户号池仅提供账号列表，不开放逐账号明细", "POOL_DETAILS_DISABLED", nil)
}

func userPoolValidateUserID(c *gin.Context, userID int64) bool {
	raw := strings.TrimSpace(c.Query("user_id"))
	if raw == "" {
		return true
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		response.BadRequest(c, "Invalid user_id")
		return false
	}
	if value != userID {
		response.Forbidden(c, "Not authorized to view other users' pool records")
		return false
	}
	return true
}

// userPoolParseTimeRange 解析 start_time / end_time；缺省最近 24 小时。
func userPoolParseTimeRange(c *gin.Context) (*time.Time, *time.Time, bool) {
	now := timezone.Now()
	end := now
	start := now.Add(-24 * time.Hour)

	if raw := strings.TrimSpace(c.Query("end_time")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			response.BadRequest(c, "Invalid end_time")
			return nil, nil, false
		}
		end = parsed
	}
	if raw := strings.TrimSpace(c.Query("start_time")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			response.BadRequest(c, "Invalid start_time")
			return nil, nil, false
		}
		start = parsed
	}
	if start.After(end) {
		response.BadRequest(c, "start_time must be before end_time")
		return nil, nil, false
	}
	return &start, &end, true
}
