// Package adminapi is exposed only on a permission-restricted Unix socket.
// The gateway verifies its existing administrator session before proxying here.
package adminapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/channels"
	"github.com/MACOS-DO/sub4api/internal/monitoring/detectors"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/incidents"
	"github.com/MACOS-DO/sub4api/internal/monitoring/notify"
	"github.com/MACOS-DO/sub4api/internal/monitoring/store"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type API struct {
	Store      *store.Store
	Manager    *incidents.Manager
	Transports map[string]bool // static fallback when Channels is nil (tests)
	Channels   *channels.Registry
	Names      NameResolver // optional; nil shows IDs only
	Build      string
	cache      pageCache
}

func (a *API) transports(r *http.Request) map[string]bool {
	if a.Channels != nil {
		return a.Channels.Transports(r.Context())
	}
	if a.Transports == nil {
		return map[string]bool{}
	}
	return a.Transports
}

var actorPattern = regexp.MustCompile(`^admin:[1-9][0-9]*$`)

func write(w http.ResponseWriter, status int, data any, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	code := status
	if status < 400 {
		code = 0
	}
	json.NewEncoder(w).Encode(map[string]any{"code": code, "message": message, "data": data})
}
func failure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrConflict):
		write(w, 409, nil, "配置或事件版本已变化，请刷新后重试")
	case errors.Is(err, store.ErrNotFound):
		write(w, 404, nil, "事件不存在")
	default:
		write(w, 503, nil, "本地监控状态暂不可用")
	}
}
func decode(w http.ResponseWriter, r *http.Request, dest any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		write(w, 400, nil, "请求格式不正确或超出大小限制")
		return false
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		write(w, 400, nil, "只允许一个JSON对象")
		return false
	}
	return true
}
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/summary", a.cached("summary", a.summary))
	mux.HandleFunc("GET /v1/config", func(w http.ResponseWriter, r *http.Request) {
		c, e := a.Store.Config(r.Context())
		if e != nil {
			failure(w, e)
			return
		}
		write(w, 200, c, "success")
	})
	mux.HandleFunc("PUT /v1/config", a.config)
	mux.HandleFunc("GET /v1/incidents/{id}", a.detail)
	mux.HandleFunc("POST /v1/incidents/{id}/control", a.control)
	mux.HandleFunc("GET /v1/incidents/{id}/preview", a.preview)
	mux.HandleFunc("GET /v1/live", a.cached("live", a.live))
	mux.HandleFunc("GET /v1/channels", a.channelsView)
	mux.HandleFunc("PUT /v1/channels", a.channelsSave)
	mux.HandleFunc("POST /v1/channels/feishu/chats", a.feishuChats)
	mux.HandleFunc("POST /v1/channels/test", a.channelTest)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !actorPattern.MatchString(r.Header.Get("X-Ops-Actor")) {
			write(w, 401, nil, "仅接受已认证的管理员代理请求")
			return
		}
		// Any change made from the page must show on the next refresh.
		if r.Method != http.MethodGet {
			a.cache.clear()
		}
		mux.ServeHTTP(w, r)
	})
}
func (a *API) summary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg, e := a.Store.Config(ctx)
	if e != nil {
		failure(w, e)
		return
	}
	rows, e := a.Store.PageIncidents(ctx, time.Now())
	if e != nil {
		failure(w, e)
		return
	}
	cursors, e := a.Store.Cursors(ctx)
	if e != nil {
		failure(w, e)
		return
	}
	deliveries, e := a.Store.DeliveryLog(ctx)
	if e != nil {
		failure(w, e)
		return
	}
	capabilities := []domain.Capability{}
	if e = a.Store.State(ctx, "capabilities:gateway", &capabilities); e != nil {
		failure(w, e)
		return
	}
	gaps := []string{"钱包独立金额流水尚未提供，不能证明所有钱包扣费金额均已对账。", "WS/二进制逐帧进度尚不支持。", "部分粘性调度未提供全池快照；无候选统计未包含全部被过滤账号ID时，不能归因到某次代理隔离。", "任务事实最多保留90天；超期未闭合任务需要人工核对。", "同机监控无法在整机或本机出网失效时保证通知。"}
	gateway, pg := false, false
	for _, c := range cursors {
		if c.Source == "gateway" {
			gateway = true
		}
		if c.Source == "postgres.errors" {
			pg = true
		}
	}
	if !gateway {
		gaps = append(gaps, "网关观测源未配置或尚未采集，实时请求异常覆盖未知。")
	}
	if !pg {
		gaps = append(gaps, "只读业务库来源未配置或尚未采集，账务和额度覆盖未知。")
	}
	if cfg.BalanceLow == nil && cfg.SubscriptionLow == nil {
		gaps = append(gaps, "低额度预警阈值未配置，Q02未启用。")
	}
	rows = listIncidents(rows, time.Now())
	refs := refSet{}
	for _, i := range rows {
		refs.addIncident(i)
	}
	write(w, 200, map[string]any{"names": a.names(ctx, refs), "build": a.Build, "config": cfg, "incidents": rows, "deliveries": deliveries, "sources": cursors, "capabilities": capabilities, "rules": pageRules(), "coverage_gaps": gaps, "transports": a.transports(r), "list_limit": 1000, "checked_at": time.Now().UTC()}, "success")
}
func (a *API) config(w http.ResponseWriter, r *http.Request) {
	var c domain.Config
	if !decode(w, r, &c) {
		return
	}
	c.Actor = r.Header.Get("X-Ops-Actor")
	if e := c.Validate(); e != nil {
		write(w, 400, nil, e.Error())
		return
	}
	ids := []string{}
	for id := range c.Rules {
		ids = append(ids, id)
	}
	for _, o := range c.OwnerOverrides {
		ids = append(ids, o.Rule)
	}
	for _, h := range c.LegacyHandoffs {
		ids = append(ids, h.MonitorRules...)
	}
	for _, id := range ids {
		if detectors.DefinitionFor(id).Title == "" {
			write(w, 400, nil, "未知规则ID")
			return
		}
	}
	if !c.Shadow {
		if len(c.FeishuTargets)+len(c.EmailTargets) == 0 {
			write(w, 400, nil, "正式通知必须配置收件目标")
			return
		}
		t := a.transports(r)
		if len(c.FeishuTargets) > 0 && !t["feishu"] || len(c.EmailTargets) > 0 && !t["email"] {
			write(w, 400, nil, "通知通道凭据尚未配置")
			return
		}
	}
	if e := a.Manager.Configure(r.Context(), c, c.Version); e != nil {
		failure(w, e)
		return
	}
	saved, e := a.Store.Config(r.Context())
	if e != nil {
		failure(w, e)
		return
	}
	write(w, 200, saved, "success")
}
func (a *API) detail(w http.ResponseWriter, r *http.Request) {
	i, e := a.Store.Incident(r.Context(), r.PathValue("id"))
	if e != nil {
		failure(w, e)
		return
	}
	if i == nil {
		failure(w, store.ErrNotFound)
		return
	}
	history, e := a.Store.History(r.Context(), i.ID)
	if e != nil {
		failure(w, e)
		return
	}
	children, e := a.Store.Children(r.Context(), i.ID)
	if e != nil {
		failure(w, e)
		return
	}
	ctx := r.Context()
	users, e := a.Store.ImpactValues(ctx, i.ID, "user", 200)
	if e != nil {
		failure(w, e)
		return
	}
	accounts, e := a.Store.ImpactValues(ctx, i.ID, "account", 200)
	if e != nil {
		failure(w, e)
		return
	}
	hits, e := a.Store.EvidenceHits(ctx, i.ID, time.Now().Add(-24*time.Hour))
	if e != nil {
		failure(w, e)
		return
	}
	refs := refSet{}
	refs.addIncident(*i)
	for _, c := range children {
		refs.addIncident(c)
	}
	toIDs := func(kind string, values []string) []int64 {
		ids := []int64{}
		for _, v := range values {
			if id, err := strconv.ParseInt(v, 10, 64); err == nil && id > 0 {
				ids = append(ids, id)
				refs.add(kind, id)
			}
		}
		return ids
	}
	impact := map[string]any{"users": toIDs("user", users), "accounts": toIDs("account", accounts)}
	write(w, 200, map[string]any{"incident": i, "history": history, "children": children, "impact": impact, "hits": hits, "names": a.names(ctx, refs)}, "success")
}
func (a *API) control(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Action    string    `json:"action"`
		Revision  int       `json:"revision"`
		RequestID string    `json:"idempotency_key"`
		Until     time.Time `json:"until"`
		Reason    string    `json:"reason"`
	}
	if !decode(w, r, &c) {
		return
	}
	e := a.Manager.Control(r.Context(), r.PathValue("id"), c.Action, r.Header.Get("X-Ops-Actor"), c.RequestID, c.Revision, c.Until, c.Reason, time.Now().UTC())
	if e != nil {
		if errors.Is(e, store.ErrConflict) || errors.Is(e, store.ErrNotFound) {
			failure(w, e)
		} else {
			write(w, 400, nil, "操作参数不正确；静默或关闭需填写原因，静默最长7天")
		}
		return
	}
	a.detail(w, r)
}
func (a *API) preview(w http.ResponseWriter, r *http.Request) {
	i, e := a.Store.Incident(r.Context(), r.PathValue("id"))
	if e != nil {
		failure(w, e)
		return
	}
	if i == nil {
		failure(w, store.ErrNotFound)
		return
	}
	cfg, e := a.Store.Config(r.Context())
	if e != nil {
		failure(w, e)
		return
	}
	card, e := notify.Card(*i, cfg.DetailBaseURL)
	if e != nil {
		failure(w, e)
		return
	}
	write(w, 200, map[string]any{"card": json.RawMessage(card), "email": notify.Plain(*i, cfg.DetailBaseURL), "sent": false}, "success")
}

func (a *API) needChannels(w http.ResponseWriter) bool {
	if a.Channels == nil {
		write(w, 503, nil, "通知通道设置未启用")
		return false
	}
	return true
}
func (a *API) channelsView(w http.ResponseWriter, r *http.Request) {
	if !a.needChannels(w) {
		return
	}
	v, e := a.Channels.View(r.Context())
	if e != nil {
		failure(w, e)
		return
	}
	write(w, 200, v, "success")
}

// channelsSave verifies new Feishu credentials with the platform before saving.
func (a *API) channelsSave(w http.ResponseWriter, r *http.Request) {
	if !a.needChannels(w) {
		return
	}
	var in channels.Settings
	if !decode(w, r, &in) {
		return
	}
	next, e := a.Channels.Merge(r.Context(), in)
	if e != nil {
		write(w, 400, nil, e.Error())
		return
	}
	cfg, e := a.Store.Config(r.Context())
	if e != nil {
		failure(w, e)
		return
	}
	if !cfg.Shadow && (len(cfg.FeishuTargets) > 0 && !next.Feishu.Configured() || len(cfg.EmailTargets) > 0 && !next.Email.Configured()) {
		write(w, 400, nil, "已配置了收件目标的通道不能清空；请先在通知设置里移除收件目标或切换影子模式")
		return
	}
	cur, _, e := a.Channels.Load(r.Context())
	if e != nil {
		failure(w, e)
		return
	}
	if next.Feishu.Configured() && next.Feishu != cur.Feishu {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		if _, e = a.Channels.FeishuClient(next.Feishu).BotInfo(ctx); e != nil {
			write(w, 400, nil, "飞书验证失败："+channels.Describe(e))
			return
		}
	}
	if e = a.Channels.Save(r.Context(), next, r.Header.Get("X-Ops-Actor"), time.Now().UTC()); e != nil {
		write(w, 400, nil, e.Error())
		return
	}
	a.channelsView(w, r)
}
func (a *API) feishuChats(w http.ResponseWriter, r *http.Request) {
	if !a.needChannels(w) {
		return
	}
	f, s, e := a.Channels.FeishuNow(r.Context())
	if e != nil {
		failure(w, e)
		return
	}
	if !s.Feishu.Configured() {
		write(w, 400, nil, "请先填写并保存飞书 App ID 和 App Secret")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	bot, e := f.BotInfo(ctx)
	if e != nil {
		write(w, 400, nil, channels.Describe(e))
		return
	}
	chats, e := f.Chats(ctx)
	if e != nil {
		write(w, 400, nil, channels.Describe(e)+"（拉取群列表需要开通「获取群组信息」权限 im:chat:readonly）")
		return
	}
	write(w, 200, map[string]any{"bot_name": bot, "chats": chats}, "success")
}
func (a *API) channelTest(w http.ResponseWriter, r *http.Request) {
	if !a.needChannels(w) {
		return
	}
	var in struct {
		Channel string `json:"channel"`
		Target  string `json:"target"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Target = strings.TrimSpace(in.Target)
	probe := domain.DefaultConfig()
	switch in.Channel {
	case "feishu":
		probe.FeishuTargets = []string{in.Target}
	case "email":
		probe.EmailTargets = []string{in.Target}
	default:
		write(w, 400, nil, "未知通道")
		return
	}
	if e := probe.Validate(); e != nil {
		write(w, 400, nil, "收件目标格式不正确")
		return
	}
	s, _, e := a.Channels.Load(r.Context())
	if e != nil {
		failure(w, e)
		return
	}
	cfg, e := a.Store.Config(r.Context())
	if e != nil {
		failure(w, e)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	now := time.Now().UTC()
	d := notify.TestDelivery(fmt.Sprintf("test-%d", now.UnixNano()), in.Channel, in.Target, now)
	if in.Channel == "feishu" {
		if !s.Feishu.Configured() {
			write(w, 400, nil, "请先填写并保存飞书 App ID 和 App Secret")
			return
		}
		_, e = a.Channels.FeishuClient(s.Feishu).Send(ctx, d, "", cfg.DetailBaseURL)
	} else {
		if !s.Email.Configured() {
			write(w, 400, nil, "请先填写并保存 SMTP 设置")
			return
		}
		m := channels.SMTPFor(s.Email)
		m.Diagnose = true
		_, e = m.Send(ctx, d, "", cfg.DetailBaseURL)
	}
	if e != nil {
		write(w, 400, nil, channels.Describe(e))
		return
	}
	write(w, 200, map[string]any{"sent": true}, "测试通知已发送")
}

// pageRule tells the page which rules close themselves after a quiet stretch.
type pageRule struct {
	detectors.Definition
	QuietClose        bool `json:"quiet_close"`
	QuietCloseSeconds int  `json:"quiet_close_seconds"`
}

func pageRules() []pageRule {
	out := make([]pageRule, 0, len(detectors.Catalog))
	for _, d := range detectors.Catalog {
		out = append(out, pageRule{Definition: d, QuietClose: incidents.QuietCloses(d.ID), QuietCloseSeconds: int(domain.QuietCloseAfter(d.ID).Seconds())})
	}
	return out
}

// listIncidents keeps every open incident and the 200 most recent closed ones,
// without the per-incident threshold bookkeeping the page never shows.
func listIncidents(rows []domain.Incident, now time.Time) []domain.Incident {
	out := make([]domain.Incident, 0, len(rows))
	closed := 0
	for _, i := range rows {
		if i.Status == "resolved" || i.Status == "superseded" {
			if closed >= 200 || now.Sub(i.LastSeen) > 3*24*time.Hour {
				continue
			}
			closed++
		}
		i.RecentHits = nil
		out = append(out, i)
	}
	return out
}
