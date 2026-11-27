package admin

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MACOS-DO/sub4api/internal/monitoring/detectors"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/notify"
	monitorruntime "github.com/MACOS-DO/sub4api/internal/monitoring/runtime"
	"github.com/MACOS-DO/sub4api/internal/monitoring/store"
	"github.com/MACOS-DO/sub4api/internal/pkg/response"
	"github.com/MACOS-DO/sub4api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func (h *OpsHandler) requireMonitor(c *gin.Context) *monitorruntime.Native {
	if h.opsService == nil || h.opsService.Monitor() == nil {
		response.Error(c, 503, "运维监控暂不可用")
		return nil
	}
	if err := h.opsService.RequireMonitoringEnabled(c.Request.Context()); err != nil {
		response.ErrorFrom(c, err)
		return nil
	}
	c.Header("Cache-Control", "no-store")
	return h.opsService.Monitor()
}
func monitorActor(c *gin.Context) string {
	s, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || s.UserID <= 0 {
		return ""
	}
	return fmt.Sprint(s.UserID)
}
func monitorBind(c *gin.Context, dest any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10)
	if c.ShouldBindJSON(dest) != nil {
		response.BadRequest(c, "请检查填写内容和请求大小")
		return false
	}
	return true
}
func monitorWriteError(c *gin.Context, err error) {
	if errors.Is(err, store.ErrConflict) {
		response.Error(c, 409, "设置已被修改，请刷新后重新保存")
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		response.Error(c, 404, "这条事件已不存在或超过保留期")
		return
	}
	response.BadRequest(c, err.Error())
}

func (h *OpsHandler) ListMonitorIncidents(c *gin.Context) {
	n := h.requireMonitor(c)
	if n == nil {
		return
	}
	start, end, err := parseOpsTimeRange(c, "24h")
	if err != nil {
		response.BadRequest(c, "请选择有效时间范围")
		return
	}
	f := store.IncidentFilter{Start: start, End: end, Platform: c.Query("platform"), Model: c.Query("model"), Status: c.Query("status"), Severity: c.Query("severity"), Rule: c.Query("rule"), BeforeID: c.Query("before_id")}
	if len(f.Model) > 256 || len(f.Platform) > 64 || len(f.Rule) > 64 || len(f.Status) > 32 || len(f.BeforeID) > 128 {
		response.BadRequest(c, "筛选条件过长")
		return
	}
	for name, dest := range map[string]*int64{"group_id": &f.GroupID, "account_id": &f.AccountID, "before_time": &f.BeforeTime} {
		if raw := c.Query(name); raw != "" {
			v, e := strconv.ParseInt(raw, 10, 64)
			if e != nil || v < 0 {
				response.BadRequest(c, "请检查筛选条件")
				return
			}
			*dest = v
		}
	}
	f.Limit = 30
	if raw := c.Query("limit"); raw != "" {
		f.Limit, err = strconv.Atoi(raw)
		if err != nil || f.Limit < 1 || f.Limit > 100 {
			response.BadRequest(c, "每页数量应为 1 至 100")
			return
		}
	}
	page, err := n.Store.ListIncidents(c.Request.Context(), f)
	if err != nil {
		response.Error(c, 500, "事件暂时无法读取，请重试")
		return
	}
	response.Success(c, page)
}

func (h *OpsHandler) GetMonitorIncident(c *gin.Context) {
	n := h.requireMonitor(c)
	if n == nil {
		return
	}
	id := c.Param("id")
	if len(id) > 128 {
		response.BadRequest(c, "事件编号过长")
		return
	}
	i, err := n.Store.Incident(c.Request.Context(), id)
	if err != nil {
		response.Error(c, 500, "事件暂时无法读取，请重试")
		return
	}
	if i == nil {
		response.Error(c, 404, "这条事件已不存在或超过保留期")
		return
	}
	outcomes, err := n.Store.IncidentContext(c.Request.Context(), i)
	if err != nil {
		response.Error(c, 500, "事件关联信息暂时无法读取，请重试")
		return
	}
	history, err := n.Store.IncidentHistory(c.Request.Context(), i.ID, 0, 30, "operations")
	if err != nil {
		response.Error(c, 500, "事件历史暂时无法读取")
		return
	}
	deliveries, err := n.Store.DeliveryPage(c.Request.Context(), i.ID, "", "", 100)
	if err != nil {
		response.Error(c, 500, "发送记录暂时无法读取")
		return
	}
	constraint := "unknown"
	if cfg, configErr := n.Store.Config(c.Request.Context()); configErr == nil {
		constraint = domain.CurrentNotificationConstraint(*i, cfg, time.Now())
	}
	response.Success(c, gin.H{"incident": i, "history": history.Items, "history_has_more": history.HasMore, "history_before_id": history.BeforeID, "outcomes": outcomes, "deliveries": deliveries, "notification_constraint": constraint})
}

// 关联请求、账号和历史均使用索引游标分页，不在打开详情时拉取全站数据。
func (h *OpsHandler) ListMonitorIncidentRecords(c *gin.Context) {
	n := h.requireMonitor(c)
	if n == nil {
		return
	}
	id, after := c.Param("id"), c.Query("after_id")
	if len(id) > 128 || len(after) > 256 {
		response.BadRequest(c, "筛选条件过长")
		return
	}
	limit := 30
	if value := c.Query("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 100 {
			response.BadRequest(c, "每页数量应为 1 至 100")
			return
		}
		limit = parsed
	}
	var result any
	var err error
	switch c.Param("records") {
	case "requests":
		result, err = n.Store.IncidentRequests(c.Request.Context(), id, after, limit)
	case "accounts":
		result, err = n.Store.IncidentAccounts(c.Request.Context(), id, after, limit)
	case "history":
		kind := c.Query("kind")
		if kind != "" && kind != "operations" && kind != "observations" {
			response.BadRequest(c, "请选择有效的历史类型")
			return
		}
		var before int64
		if value := c.Query("before_id"); value != "" {
			before, err = strconv.ParseInt(value, 10, 64)
			if err != nil || before < 0 {
				response.BadRequest(c, "请刷新后重新翻页")
				return
			}
		}
		result, err = n.Store.IncidentHistory(c.Request.Context(), id, before, limit, kind)
	default:
		response.Error(c, 404, "未找到这类事件记录")
		return
	}
	if err != nil {
		response.Error(c, 500, "关联记录暂时无法读取，请重试")
		return
	}
	response.Success(c, result)
}

func (h *OpsHandler) ControlMonitorIncident(c *gin.Context) {
	n := h.requireMonitor(c)
	if n == nil {
		return
	}
	actor := monitorActor(c)
	if actor == "" {
		response.Unauthorized(c, "请重新登录后操作")
		return
	}
	var input struct {
		Action    string    `json:"action"`
		Revision  int       `json:"revision"`
		Until     time.Time `json:"until"`
		Reason    string    `json:"reason"`
		RequestID string    `json:"request_id"`
	}
	if !monitorBind(c, &input) {
		return
	}
	if _, err := uuid.Parse(input.RequestID); err != nil {
		response.BadRequest(c, "请刷新后重新操作")
		return
	}
	if err := n.Manager.Control(c.Request.Context(), c.Param("id"), input.Action, actor, input.RequestID, input.Revision, input.Until, input.Reason, time.Now().UTC()); err != nil {
		monitorWriteError(c, err)
		return
	}
	middleware.SetAuditAction(c, "ops.incident.control")
	response.Success(c, gin.H{"saved": true})
}

func (h *OpsHandler) GetMonitorConfig(c *gin.Context) {
	n := h.requireMonitor(c)
	if n == nil {
		return
	}
	cfg, err := n.Store.Config(c.Request.Context())
	if err != nil {
		response.Error(c, 500, "监控设置暂时无法读取")
		return
	}
	rules := make(map[string]domain.RuleConfig, len(detectors.Catalog))
	for _, d := range detectors.Catalog {
		rules[d.ID] = cfg.Rule(d.ID)
	}
	cfg.Rules = rules
	response.Success(c, gin.H{"config": cfg, "rules": detectors.Catalog})
}

func (h *OpsHandler) UpdateMonitorConfig(c *gin.Context) {
	n := h.requireMonitor(c)
	if n == nil {
		return
	}
	actor := monitorActor(c)
	if actor == "" {
		response.Unauthorized(c, "请重新登录后保存")
		return
	}
	var input domain.Config
	if !monitorBind(c, &input) {
		return
	}
	input.Actor = actor
	input.RequestID = uuid.NewString()
	if err := n.Manager.Configure(c.Request.Context(), input, input.Version); err != nil {
		monitorWriteError(c, err)
		return
	}
	middleware.SetAuditAction(c, "ops.monitor.configure")
	h.GetMonitorConfig(c)
}

func (h *OpsHandler) GetMonitorHealth(c *gin.Context) {
	n := h.requireMonitor(c)
	if n == nil {
		return
	}
	data, err := n.Snapshot(c.Request.Context())
	if err != nil {
		response.Error(c, 503, "采集状态暂时无法读取")
		return
	}
	response.Success(c, data)
}

func (h *OpsHandler) ListNotificationChannels(c *gin.Context) {
	n := h.requireMonitor(c)
	if n == nil {
		return
	}
	channels, err := n.Store.Channels(c.Request.Context())
	if err != nil {
		response.Error(c, 500, "通知渠道暂时无法读取")
		return
	}
	targets, err := n.Store.Targets(c.Request.Context(), false)
	if err != nil {
		response.Error(c, 500, "接收对象暂时无法读取")
		return
	}
	response.Success(c, gin.H{"channels": channels, "targets": targets, "kinds": domain.ChannelKinds})
}

func (h *OpsHandler) SaveNotificationChannel(c *gin.Context) {
	n := h.requireMonitor(c)
	if n == nil {
		return
	}
	actor := monitorActor(c)
	if actor == "" {
		response.Unauthorized(c, "请重新登录后保存")
		return
	}
	var input domain.ChannelInput
	if !monitorBind(c, &input) {
		return
	}
	input.ID = c.Param("id")
	if input.Kind == "email" && len(input.Secrets) > 0 {
		response.BadRequest(c, "邮件使用系统 SMTP 配置")
		return
	}
	if raw := input.Secrets["webhook"]; raw != "" {
		if err := h.opsService.ValidateMonitorEndpoint(c.Request.Context(), input.Kind, raw); err != nil {
			monitorWriteError(c, err)
			return
		}
	}
	if err := n.Store.SaveChannel(c.Request.Context(), input, actor, n.Worker.Cipher); err != nil {
		monitorWriteError(c, err)
		return
	}
	middleware.SetAuditAction(c, "ops.notification.channel.save")
	h.ListNotificationChannels(c)
}

func (h *OpsHandler) TestNotificationChannel(c *gin.Context) {
	n := h.requireMonitor(c)
	if n == nil {
		return
	}
	actor := monitorActor(c)
	if actor == "" {
		response.Unauthorized(c, "请重新登录后测试")
		return
	}
	var input struct {
		TargetID string `json:"target_id"`
	}
	if !monitorBind(c, &input) {
		return
	}
	if err := n.Store.Audit(c.Request.Context(), actor, "notification.test", c.Param("id"), input.TargetID); err != nil {
		response.Error(c, 500, "操作暂时无法登记，请重试")
		return
	}
	result, err := n.Worker.Test(c.Request.Context(), c.Param("id"), input.TargetID)
	middleware.SetAuditAction(c, "ops.notification.test")
	if err != nil {
		var f *notify.Failure
		if errors.As(err, &f) {
			response.Success(c, gin.H{"state": result, "error_class": f.Class, "message": f.Error()})
			return
		}
		response.Error(c, 500, "测试结果暂时无法读取，请检查发送记录")
		return
	}
	response.Success(c, gin.H{"state": result})
}

func (h *OpsHandler) ListNotificationDeliveries(c *gin.Context) {
	n := h.requireMonitor(c)
	if n == nil {
		return
	}
	id, channel, state := strings.TrimSpace(c.Query("incident_id")), strings.TrimSpace(c.Query("channel")), strings.TrimSpace(c.Query("state"))
	if len(id) > 128 || len(channel) > 128 || len(state) > 32 {
		response.BadRequest(c, "筛选条件过长")
		return
	}
	rows, err := n.Store.DeliveryPage(c.Request.Context(), id, channel, state, 100)
	if err != nil {
		response.Error(c, 500, "发送记录暂时无法读取")
		return
	}
	response.Success(c, rows)
}
