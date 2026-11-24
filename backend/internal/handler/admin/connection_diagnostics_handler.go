package admin

import (
	"strconv"
	"strings"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/response"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func (h *OpsHandler) ListConnectionTraces(c *gin.Context) {
	if h.opsService == nil {
		response.Error(c, 503, "连接诊断暂不可用")
		return
	}
	start, end, err := parseOpsTimeRange(c, "1h")
	if err != nil {
		response.BadRequest(c, "请选择有效时间范围")
		return
	}
	f := service.ConnectionTraceFilter{Start: start, End: end, Model: strings.TrimSpace(c.Query("model")), RequestID: strings.TrimSpace(c.Query("request_id")), State: c.Query("state"), Transport: c.Query("transport")}
	if len(f.Model) > 256 || len(f.RequestID) > 128 {
		response.BadRequest(c, "筛选内容过长")
		return
	}
	for name, target := range map[string]*int64{"user_id": &f.UserID, "api_key_id": &f.APIKeyID, "account_id": &f.AccountID, "group_id": &f.GroupID} {
		if raw := c.Query(name); raw != "" {
			value, e := strconv.ParseInt(raw, 10, 64)
			if e != nil || value <= 0 {
				response.BadRequest(c, "请选择有效的用户、密钥、账号或分组")
				return
			}
			*target = value
		}
	}
	if raw := c.Query("limit"); raw != "" {
		f.Limit, err = strconv.Atoi(raw)
		if err != nil || f.Limit < 1 || f.Limit > 100 {
			response.BadRequest(c, "每页数量应为 1 至 100")
			return
		}
	}
	if raw := c.Query("cursor"); raw != "" {
		f.BeforeTime, f.BeforeID, err = service.DecodeConnectionCursor(raw)
		if err != nil {
			response.BadRequest(c, "分页已失效，请重新查询")
			return
		}
	}
	page, err := h.opsService.ListConnectionTraces(c.Request.Context(), f)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, page)
}

func (h *OpsHandler) GetConnectionTrace(c *gin.Context) {
	if h.opsService == nil {
		response.Error(c, 503, "连接诊断暂不可用")
		return
	}
	id := c.Param("trace_id")
	if _, err := uuid.Parse(id); err != nil {
		response.BadRequest(c, "请选择有效记录")
		return
	}
	at, err := time.Parse(time.RFC3339Nano, c.Query("started_at"))
	if err != nil {
		response.BadRequest(c, "缺少记录时间，请刷新后重试")
		return
	}
	row, err := h.opsService.GetConnectionTrace(c.Request.Context(), at, id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, row)
}
