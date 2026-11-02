package workspace

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/response"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *Handler) getUpstreamModels(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	g, err := h.modelGroup(c.Request.Context(), c.Param("id"))
	if err != nil {
		fail(c, err)
		return
	}
	r, err := h.loadModelRecord(c.Request.Context(), g.ID)
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, publicModelRecord(r, g))
}
func (h *Handler) saveUpstreamModels(c *gin.Context) {
	var req struct {
		Revision int64               `json:"revision"`
		Config   upstreamModelConfig `json:"config"`
	}
	if err := body(c, &req, 16<<10); err != nil {
		fail(c, err)
		return
	}
	if err := normalizeModelConfig(&req.Config); err != nil {
		fail(c, err)
		return
	}
	g, err := h.modelGroup(c.Request.Context(), c.Param("id"))
	if err != nil {
		fail(c, err)
		return
	}
	raw, _ := json.Marshal(req.Config)
	res, err := h.db.ExecContext(c.Request.Context(), `UPDATE workspace_upstream_models SET config=$2,revision=revision+1,next_check_at=NOW() WHERE upstream_id=$1 AND revision=$3`, g.ID, string(raw), req.Revision)
	if err != nil {
		fail(c, err)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		fail(c, &requestError{409, "模型目录或设置已变化，请刷新后重试。"})
		return
	}
	response.Success(c, gin.H{"saved": true})
}
func (h *Handler) refreshUpstreamModels(c *gin.Context) {
	id := c.Param("id")
	groups, err := h.discoverUpstreamBalances(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	if err = h.syncUpstreamBalances(c.Request.Context(), groups); err != nil {
		fail(c, err)
		return
	}
	found := id == ""
	count := int64(0)
	for _, g := range groups {
		if id != "" && g.ID != id {
			continue
		}
		found = true
		r, e := h.db.ExecContext(c.Request.Context(), `UPDATE workspace_upstream_models SET refresh_requested=TRUE,requested_at=NOW()
 WHERE upstream_id=$1 AND refresh_requested=FALSE AND (lease_until IS NULL OR lease_until<NOW()) AND (requested_at IS NULL OR requested_at<NOW()-INTERVAL '30 seconds')`, g.ID)
		if e != nil {
			fail(c, e)
			return
		}
		n, _ := r.RowsAffected()
		count += n
	}
	if !found {
		fail(c, notFound())
		return
	}
	response.Accepted(c, gin.H{"queued": count})
}

type upstreamModelAction struct {
	Revision          int64    `json:"revision"`
	Action            string   `json:"action"`
	AccountID         int64    `json:"account_id"`
	Added             []string `json:"added"`
	Removed           []string `json:"removed"`
	InitializeMapping bool     `json:"initialize_mapping"`
}

func validateModelAction(req upstreamModelAction, state upstreamModelAccountState) error {
	if req.Action != "apply" && req.Action != "ignore" && req.Action != "restore" {
		return badRequest("模型变更操作无效。")
	}
	if len(req.Added)+len(req.Removed) == 0 || len(req.Added)+len(req.Removed) > modelCatalogLimit*2 {
		return badRequest("请选择要处理的模型。")
	}
	allowed := pendingModelChange(state)
	if req.Action == "restore" {
		allowed.Added = state.IgnoredAdded
		allowed.Removed = state.IgnoredRemoved
	}
	if len(modelDifference(req.Added, allowed.Added))+len(modelDifference(req.Removed, allowed.Removed)) > 0 {
		return &requestError{409, "所选模型不在当前差异中，请刷新后重试。"}
	}
	if req.Action == "apply" && (state.Error != "" || state.CheckedAt == nil || time.Since(*state.CheckedAt) > 24*time.Hour) {
		return badRequest("请先成功检测模型目录，再应用变更。")
	}
	return nil
}
func (h *Handler) actUpstreamModels(c *gin.Context) {
	var req upstreamModelAction
	if err := body(c, &req, 2<<20); err != nil {
		fail(c, err)
		return
	}
	who, err := h.identity(c)
	if err != nil {
		fail(c, err)
		return
	}
	if err = h.applyUpstreamModelAction(c.Request.Context(), c.Param("id"), who.ID, req); err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"saved": true})
}
func (h *Handler) applyUpstreamModelAction(ctx context.Context, id string, actorID int64, req upstreamModelAction) error {
	if req.Revision < 1 || req.AccountID < 1 {
		return badRequest("请选择有效的账号和目录版本。")
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r, err := scanModelRecord(tx.QueryRowContext(ctx, "SELECT "+modelRecordColumns+" FROM workspace_upstream_models WHERE upstream_id=$1 FOR UPDATE", id))
	if err != nil {
		return err
	}
	if r.Revision != req.Revision {
		return &requestError{409, "模型目录已变化，请刷新后重新选择。"}
	}
	// Lock the live account and only update model_mapping, never write a stale credentials object.
	var a service.Account
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT id,name,platform,type,credentials,proxy_id FROM accounts WHERE id=$1 AND type='apikey' AND deleted_at IS NULL FOR UPDATE`, req.AccountID).Scan(&a.ID, &a.Name, &a.Platform, &a.Type, &raw, &a.ProxyID)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(raw, &a.Credentials); err != nil {
		return err
	}
	groups := upstreamBalanceGroups([]service.Account{a})
	valid := false
	for _, g := range groups {
		if g.ID == id {
			valid = true
		}
	}
	if !valid {
		return notFound()
	}
	index := -1
	for i, s := range r.Accounts {
		if s.AccountID == a.ID {
			index = i
			break
		}
	}
	live := upstreamBalanceAccount{ID: a.ID, Platform: a.Platform, key: strings.TrimSpace(a.GetCredential("api_key")), proxyID: a.ProxyID}
	if index < 0 || r.Accounts[index].Source != modelSource(live) {
		return &requestError{409, "账号地址或凭据已变化，请重新检测。"}
	}
	state := r.Accounts[index]
	if err = validateModelAction(req, state); err != nil {
		return err
	}
	change := upstreamModelChange{AccountID: a.ID, Added: sortedModelSet(req.Added), Removed: sortedModelSet(req.Removed)}
	switch req.Action {
	case "apply":
		if len(groups) != 1 {
			return badRequest("该账号使用多个上游地址，请到账号管理调整共享的模型映射。")
		}
		if a.IsOpenAIPassthroughEnabled() {
			return badRequest("透传账号由上游决定模型，请使用忽略或到账号管理切换模式。")
		}
		mapping, e := applyModelMapping(a.Credentials["model_mapping"], state, change, req.InitializeMapping)
		if e != nil {
			return e
		}
		mappingJSON, _ := json.Marshal(mapping)
		_, err = tx.ExecContext(ctx, `UPDATE accounts SET credentials=jsonb_set(COALESCE(credentials,'{}'::jsonb),'{model_mapping}',$2::jsonb,true),updated_at=NOW() WHERE id=$1`, a.ID, string(mappingJSON))
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO scheduler_outbox(event_type,account_id,group_id,payload) VALUES($1,$2,NULL,NULL)`, service.SchedulerOutboxEventAccountChanged, a.ID)
		if err != nil {
			return err
		}
		state.Baseline = sortedModelSet(append(modelDifference(state.Baseline, change.Removed), change.Added...))
	case "ignore":
		state.IgnoredAdded = sortedModelSet(append(state.IgnoredAdded, change.Added...))
		state.IgnoredRemoved = sortedModelSet(append(state.IgnoredRemoved, change.Removed...))
	case "restore":
		state.IgnoredAdded = modelDifference(state.IgnoredAdded, change.Added)
		state.IgnoredRemoved = modelDifference(state.IgnoredRemoved, change.Removed)
	}
	r.Accounts[index] = state
	addModelHistory(&r, req.Action, actorID, []upstreamModelChange{change})
	states, _ := json.Marshal(r.Accounts)
	history, _ := json.Marshal(r.History)
	_, err = tx.ExecContext(ctx, `UPDATE workspace_upstream_models SET state=$2,history=$3,revision=revision+1 WHERE upstream_id=$1`, id, string(states), string(history))
	if err != nil {
		return err
	}
	return tx.Commit()
}
