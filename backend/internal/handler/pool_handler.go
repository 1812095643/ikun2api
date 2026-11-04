package handler

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/MACOS-DO/sub4api/internal/handler/dto"
	"github.com/MACOS-DO/sub4api/internal/pkg/pagination"
	"github.com/MACOS-DO/sub4api/internal/pkg/response"
	"github.com/MACOS-DO/sub4api/internal/pkg/timezone"
	"github.com/MACOS-DO/sub4api/internal/pkg/usagestats"
	middleware2 "github.com/MACOS-DO/sub4api/internal/server/middleware"
	"github.com/MACOS-DO/sub4api/internal/service"
)

// PoolHandler 提供「实时号池」的用户端只读接口。
//
// 与 admin.PoolHandler 的关系：
//   - 同一份底层数据（账号表 + usage_logs），同一套聚合服务。
//   - 差异在脱敏：这里的每个查询都被强制钉上 subject.UserID，
//     用户拿不到别人的账号、别人的用量，也拿不到账号在全站的消耗。
//   - 交叉校验：任何显式传入的 user_id / account_id 都必须落在本人范围内，
//     越界直接 403，而不是静默忽略（静默忽略会让越权尝试看起来"正常"）。
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
	overview, err := h.poolService.OverviewForUser(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, overview)
}

// Accounts 返回当前用户用过的账号列表（脱敏 + 本人今日用量）。
// GET /api/v1/pool/accounts
func (h *PoolHandler) Accounts(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
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
	response.Paginated(c, result.Items, result.Total, result.Page, result.PageSize)
}

// AccountRequests 返回当前用户在指定账号上的近期请求流水。
// GET /api/v1/pool/accounts/:id/requests
func (h *PoolHandler) AccountRequests(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	accountID, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "Invalid account id")
		return
	}

	params := pagination.PaginationParams{Page: 1, PageSize: 20, SortBy: "created_at", SortOrder: "desc"}
	if page, pageSize := response.ParsePagination(c); page > 0 && pageSize > 0 {
		params.Page = page
		params.PageSize = pageSize
	}

	startTime, endTime, ok := userPoolParseTimeRange(c)
	if !ok {
		return
	}

	filters := usagestats.UsageLogFilters{
		UserID:    subject.UserID,
		AccountID: accountID,
		StartTime: startTime,
		EndTime:   endTime,
	}
	if model := strings.TrimSpace(c.Query("model")); model != "" {
		filters.Model = model
	}

	records, result, err := h.usageService.ListWithFilters(c.Request.Context(), params, filters)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	items := make([]*dto.UsageLog, 0, len(records))
	for i := range records {
		// 用户端用 UsageLog（不含 upstream_request_id / 渠道 / 账号倍率等管理端字段）。
		items = append(items, dto.UsageLogFromService(&records[i]))
	}
	response.Paginated(c, items, result.Total, result.Page, result.PageSize)
}

// Usage 返回当前用户自己的号池使用记录（脱敏）。
// GET /api/v1/pool/usage
func (h *PoolHandler) Usage(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	params := pagination.PaginationParams{Page: 1, PageSize: 20, SortBy: "created_at", SortOrder: "desc"}
	if page, pageSize := response.ParsePagination(c); page > 0 && pageSize > 0 {
		params.Page = page
		params.PageSize = pageSize
	}

	startTime, endTime, ok := userPoolParseTimeRange(c)
	if !ok {
		return
	}

	// 强制钉上本人的 user_id，前端传什么都覆盖不掉。
	filters := usagestats.UsageLogFilters{
		UserID:    subject.UserID,
		StartTime: startTime,
		EndTime:   endTime,
	}
	if raw := strings.TrimSpace(c.Query("account_id")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 {
			response.BadRequest(c, "Invalid account_id")
			return
		}
		filters.AccountID = value
	}
	if raw := strings.TrimSpace(c.Query("api_key_id")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 {
			response.BadRequest(c, "Invalid api_key_id")
			return
		}
		filters.APIKeyID = value
	}
	if raw := strings.TrimSpace(c.Query("user_id")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 {
			response.BadRequest(c, "Invalid user_id")
			return
		}
		// 只允许查自己；越界明确拒绝，不静默忽略。
		if value != subject.UserID {
			response.Forbidden(c, "Not authorized to view other users' pool records")
			return
		}
	}
	if model := strings.TrimSpace(c.Query("model")); model != "" {
		filters.Model = model
	}
	if requestID := strings.TrimSpace(c.Query("request_id")); requestID != "" {
		filters.RequestID = requestID
	}

	records, result, err := h.usageService.ListWithFilters(c.Request.Context(), params, filters)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	items := make([]*dto.UsageLog, 0, len(records))
	for i := range records {
		items = append(items, dto.UsageLogFromService(&records[i]))
	}
	response.Paginated(c, items, result.Total, result.Page, result.PageSize)
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
