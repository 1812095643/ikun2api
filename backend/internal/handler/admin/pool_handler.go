package admin

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
	"github.com/MACOS-DO/sub4api/internal/service"
)

// PoolHandler 提供「实时号池」管理端只读接口。
//
// 职责边界：
//   - 只读：不提供改号、改配置的入口，避免与账号管理页形成两套写入路径。
//   - 请求流水直接复用 UsageService，保证与「使用记录」页同一份数据、同一套过滤语义。
type PoolHandler struct {
	poolService  *service.AccountPoolService
	usageService *service.UsageService
}

// NewPoolHandler 创建号池 handler。
func NewPoolHandler(poolService *service.AccountPoolService, usageService *service.UsageService) *PoolHandler {
	return &PoolHandler{
		poolService:  poolService,
		usageService: usageService,
	}
}

// Overview 返回号池概览。
func (h *PoolHandler) Overview(c *gin.Context) {
	overview, err := h.poolService.Overview(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, overview)
}

func (h *PoolHandler) DisplaySettings(c *gin.Context) {
	settings, err := h.poolService.GetPoolDisplaySettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}
func (h *PoolHandler) SaveDisplaySettings(c *gin.Context) {
	var settings service.PoolDisplaySettings
	if err := c.ShouldBindJSON(&settings); err != nil {
		response.BadRequest(c, "Invalid pool display settings")
		return
	}
	result, err := h.poolService.SavePoolDisplaySettings(c.Request.Context(), settings)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// Accounts 返回号池账号列表（含今日用量、并发占用）。
func (h *PoolHandler) Accounts(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)

	filter := service.PoolAccountFilter{
		Page:     page,
		PageSize: pageSize,
		Platform: strings.TrimSpace(c.Query("platform")),
		Type:     strings.TrimSpace(c.Query("type")),
		Status:   strings.TrimSpace(c.Query("status")),
		Search:   strings.TrimSpace(c.Query("search")),
	}
	if groupIDStr := strings.TrimSpace(c.Query("group_id")); groupIDStr != "" {
		groupID, err := strconv.ParseInt(groupIDStr, 10, 64)
		if err != nil || groupID < 0 {
			response.BadRequest(c, "Invalid group_id")
			return
		}
		filter.GroupID = groupID
	}

	result, err := h.poolService.ListAccounts(c.Request.Context(), filter)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, result.Items, result.Total, result.Page, result.PageSize)
}

// AccountRequests 返回单个账号的近期请求流水（管理员口径）。
func (h *PoolHandler) AccountRequests(c *gin.Context) {
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

	startTime, endTime, ok := poolParseTimeRange(c)
	if !ok {
		return
	}

	filters := usagestats.UsageLogFilters{
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

	items := make([]*dto.AdminUsageLog, 0, len(records))
	for i := range records {
		items = append(items, dto.UsageLogFromServiceAdmin(&records[i]))
	}
	response.Paginated(c, items, result.Total, result.Page, result.PageSize)
}

// Usage 返回号池口径的使用记录：与「使用记录」页同源，额外带上写入时固化的
// 账号名与平台快照，保证账号改名或被删除后历史记录仍可读。
func (h *PoolHandler) Usage(c *gin.Context) {
	params := pagination.PaginationParams{Page: 1, PageSize: 20, SortBy: "created_at", SortOrder: "desc"}
	if page, pageSize := response.ParsePagination(c); page > 0 && pageSize > 0 {
		params.Page = page
		params.PageSize = pageSize
	}

	startTime, endTime, ok := poolParseTimeRange(c)
	if !ok {
		return
	}

	filters := usagestats.UsageLogFilters{
		StartTime: startTime,
		EndTime:   endTime,
	}
	if raw := strings.TrimSpace(c.Query("user_id")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 {
			response.BadRequest(c, "Invalid user_id")
			return
		}
		filters.UserID = value
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

	items := make([]*dto.AdminUsageLog, 0, len(records))
	for i := range records {
		items = append(items, dto.UsageLogFromServiceAdmin(&records[i]))
	}
	response.Paginated(c, items, result.Total, result.Page, result.PageSize)
}

// poolParseTimeRange 解析 start_time / end_time 查询参数；缺省为最近 24 小时。
func poolParseTimeRange(c *gin.Context) (*time.Time, *time.Time, bool) {
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
