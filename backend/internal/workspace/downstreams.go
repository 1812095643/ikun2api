package workspace

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/response"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/shopspring/decimal"
)

func validDownstreamRate(v float64) bool {
	if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 || v > 100000 {
		return false
	}
	value, err := decimal.NewFromString(strconv.FormatFloat(v, 'f', -1, 64))
	return err == nil && value.Exponent() >= -4
}

func validateDownstreamGroups(ctx context.Context, tx *sql.Tx, ids map[int64]bool) error {
	if len(ids) == 0 {
		return nil
	}
	keys := make([]int64, 0, len(ids))
	for id := range ids {
		keys = append(keys, id)
	}
	var count int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM groups WHERE id=ANY($1) AND deleted_at IS NULL`, pq.Array(keys)).Scan(&count)
	if err != nil {
		return err
	}
	if count != len(keys) {
		return badRequest("部分分组已删除，请刷新后重试")
	}
	return nil
}

type downstreamApply struct {
	UserIDs      []int64            `json:"user_ids"`
	IsDownstream *bool              `json:"is_downstream"`
	GroupRates   map[int64]*float64 `json:"group_rates"`
	Key          string             `json:"operation_key"`
}

func normalizeDownstreamApply(req *downstreamApply) error {
	if len(req.UserIDs) == 0 || len(req.UserIDs) > 200 {
		return badRequest("每次选择 1–200 个用户")
	}
	seen := map[int64]bool{}
	for _, id := range req.UserIDs {
		if id <= 0 || seen[id] {
			return badRequest("用户列表无效")
		}
		seen[id] = true
	}
	sort.Slice(req.UserIDs, func(i, j int) bool { return req.UserIDs[i] < req.UserIDs[j] })
	key, err := uuid.Parse(req.Key)
	if err != nil || key == uuid.Nil || key.String() != req.Key {
		return badRequest("请刷新后重试")
	}
	if req.IsDownstream == nil && len(req.GroupRates) == 0 {
		return badRequest("请选择操作")
	}
	if len(req.GroupRates) > 1000 {
		return badRequest("分组过多")
	}
	for id, rate := range req.GroupRates {
		if id <= 0 || (rate != nil && !validDownstreamRate(*rate)) {
			return badRequest("分组倍率无效")
		}
	}
	if req.IsDownstream != nil && !*req.IsDownstream && len(req.GroupRates) > 0 {
		return badRequest("移出下游时不能同时调价")
	}
	return nil
}
func (h *Handler) applyDownstreamChanges(c *gin.Context) {
	var req downstreamApply
	if err := body(c, &req, 128<<10); err != nil {
		fail(c, err)
		return
	}
	if err := normalizeDownstreamApply(&req); err != nil {
		fail(c, err)
		return
	}
	actor, err := h.identity(c)
	if err != nil {
		fail(c, err)
		return
	}
	ctx := c.Request.Context()
	affected, err := h.applyDownstream(ctx, actor.ID, req)
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"affected": affected})
}
func (h *Handler) applyDownstream(ctx context.Context, actorID int64, req downstreamApply) (int, error) {
	raw, _ := json.Marshal(req)
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	// The advisory transaction lock also serializes retries with different target users.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,73))`, req.Key); err != nil {
		return 0, err
	}
	var previousDigest string
	var previousActor int64
	var affected int
	err = tx.QueryRowContext(ctx, `SELECT actor_id,payload_digest,affected FROM admin_downstream_operations WHERE operation_key=$1`, req.Key).Scan(&previousActor, &previousDigest, &affected)
	if err == nil {
		if previousActor != actorID || previousDigest != digest {
			return 0, &requestError{409, "操作内容已变化，请刷新后重试"}
		}
		return affected, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	rates := req.GroupRates

	ids := map[int64]bool{}
	for id := range rates {
		ids[id] = true
	}
	if err = validateDownstreamGroups(ctx, tx, ids); err != nil {
		return 0, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,is_downstream FROM users WHERE id=ANY($1) AND deleted_at IS NULL ORDER BY id FOR UPDATE`, pq.Array(req.UserIDs))
	if err != nil {
		return 0, err
	}
	count := 0
	eligible := true
	for rows.Next() {
		var id int64
		var downstream bool
		if err = rows.Scan(&id, &downstream); err != nil {
			break
		}
		count++
		if !downstream && (req.IsDownstream == nil || !*req.IsDownstream) {
			eligible = false
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return 0, err
	}
	if count != len(req.UserIDs) || !eligible {
		return 0, badRequest("部分用户已删除或不再是下游，请刷新后重试")
	}
	for _, id := range req.UserIDs {
		if _, err = tx.ExecContext(ctx, `UPDATE users SET is_downstream=COALESCE($2,is_downstream),updated_at=NOW() WHERE id=$1`, id, req.IsDownstream); err != nil {
			return 0, err
		}
		for groupID, rate := range rates {
			if rate == nil {
				if _, err = tx.ExecContext(ctx, `UPDATE user_group_rate_multipliers SET rate_multiplier=NULL,updated_at=NOW() WHERE user_id=$1 AND group_id=$2`, id, groupID); err != nil {
					return 0, err
				}
				if _, err = tx.ExecContext(ctx, `DELETE FROM user_group_rate_multipliers WHERE user_id=$1 AND group_id=$2 AND rate_multiplier IS NULL AND rpm_override IS NULL`, id, groupID); err != nil {
					return 0, err
				}
			} else {
				if _, err = tx.ExecContext(ctx, `INSERT INTO user_group_rate_multipliers(user_id,group_id,rate_multiplier,created_at,updated_at) VALUES($1,$2,$3,NOW(),NOW()) ON CONFLICT(user_id,group_id) DO UPDATE SET rate_multiplier=EXCLUDED.rate_multiplier,updated_at=NOW()`, id, groupID, *rate); err != nil {
					return 0, err
				}
			}
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO admin_downstream_operations(operation_key,actor_id,payload_digest,affected) VALUES($1,$2,$3,$4)`, req.Key, actorID, digest, count); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	service.InvalidateUserGroupRateCache()
	if invalidator, ok := h.admin.(interface{ InvalidateBalanceAfterCredit(context.Context, int64) }); ok {
		cacheCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		for _, id := range req.UserIDs {
			invalidator.InvalidateBalanceAfterCredit(cacheCtx, id)
		}
	}
	return count, nil
}
func downstreamRange(c *gin.Context) (time.Time, time.Time, error) {
	end := time.Now()
	start := end.AddDate(0, 0, -30)
	var err error
	if raw := c.Query("from"); raw != "" {
		start, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			return start, end, badRequest("开始时间无效")
		}
	}
	if raw := c.Query("to"); raw != "" {
		end, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			return start, end, badRequest("结束时间无效")
		}
	}
	if !start.Before(end) || end.Sub(start) > 366*24*time.Hour {
		return start, end, badRequest("请选择一年以内的统计区间")
	}
	return start, end, nil
}

type downstreamGroupRate struct {
	GroupID   int64   `json:"group_id"`
	GroupName string  `json:"group_name"`
	Rate      float64 `json:"rate"`
}

type downstreamUser struct {
	GroupRates  []downstreamGroupRate `json:"group_rates"`
	ID          int64                 `json:"id"`
	Email       string                `json:"email"`
	Username    string                `json:"username"`
	Status      string                `json:"status"`
	Balance     string                `json:"balance"`
	Outstanding string                `json:"outstanding"`
	Requests    int64                 `json:"requests"`
	Tokens      int64                 `json:"tokens"`
	Cost        string                `json:"cost"`
	LastUsed    *time.Time            `json:"last_used_at"`
}

func (h *Handler) listDownstreams(c *gin.Context) {
	from, to, err := downstreamRange(c)
	if err != nil {
		fail(c, err)
		return
	}
	search := strings.TrimSpace(c.Query("search"))
	if len(search) > 300 {
		fail(c, badRequest("搜索内容过长"))
		return
	}

	page, size := pageNumber(c.Query("page"), 1, 100000), pageNumber(c.Query("page_size"), 20, 100)
	ctx := c.Request.Context()
	var total int
	var balance string
	const filter = ` FROM users u WHERE u.is_downstream AND u.deleted_at IS NULL AND ($1='' OR u.email ILIKE '%'||$1||'%' OR u.username ILIKE '%'||$1||'%')`
	if err = h.db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(balance),0)::text`+filter, search).Scan(&total, &balance); err != nil {
		fail(c, err)
		return
	}
	rows, err := h.db.QueryContext(ctx, `WITH selected AS (SELECT u.id,u.email,u.username,u.status,u.balance`+filter+` ORDER BY u.id DESC LIMIT $2 OFFSET $3)
 SELECT s.id,s.email,s.username,s.status,s.balance::text,
 COALESCE((SELECT SUM(CASE WHEN kind='credit' THEN amount ELSE -amount END) FROM admin_receivable_entries WHERE user_id=s.id),0)::text,
 x.requests,x.tokens,x.cost::text,x.last_used,
 COALESCE((SELECT jsonb_agg(jsonb_build_object('group_id',r.group_id,'group_name',g.name,'rate',r.rate_multiplier) ORDER BY g.name,r.group_id)
 FROM user_group_rate_multipliers r JOIN groups g ON g.id=r.group_id WHERE r.user_id=s.id AND r.rate_multiplier IS NOT NULL AND g.deleted_at IS NULL),'[]'::jsonb)
 FROM selected s CROSS JOIN LATERAL(
 SELECT COUNT(*) requests,COALESCE(SUM(input_tokens::bigint+output_tokens+cache_creation_tokens+cache_read_tokens),0)::bigint tokens,COALESCE(SUM(actual_cost),0) cost,MAX(created_at) last_used FROM usage_logs WHERE user_id=s.id AND created_at >=$4 AND created_at <$5) x ORDER BY s.id DESC`, search, size, (page-1)*size, from, to)
	if err != nil {
		fail(c, err)
		return
	}
	defer rows.Close()
	items := []downstreamUser{}
	for rows.Next() {
		var d downstreamUser
		var rates []byte
		if err = rows.Scan(&d.ID, &d.Email, &d.Username, &d.Status, &d.Balance, &d.Outstanding, &d.Requests, &d.Tokens, &d.Cost, &d.LastUsed, &rates); err != nil {
			fail(c, err)
			return
		}
		if err = json.Unmarshal(rates, &d.GroupRates); err != nil {
			fail(c, err)
			return
		}
		items = append(items, d)
	}
	if err = rows.Err(); err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"items": items, "total": total, "page": page, "page_size": size, "summary": gin.H{"balance": balance}, "from": from, "to": to})
}
func (h *Handler) downstreamUsage(c *gin.Context) {
	id, err := positiveID(c)
	if err != nil {
		fail(c, err)
		return
	}
	from, to, err := downstreamRange(c)
	if err != nil {
		fail(c, err)
		return
	}
	var exists bool
	if err = h.db.QueryRowContext(c.Request.Context(), `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND is_downstream AND deleted_at IS NULL)`, id).Scan(&exists); err != nil {
		fail(c, err)
		return
	}
	if !exists {
		fail(c, notFound())
		return
	}
	rows, err := h.db.QueryContext(c.Request.Context(), `SELECT COALESCE(g.id,0),COALESCE(g.name,'未分组'),l.model,COUNT(*),COALESCE(SUM(l.input_tokens::bigint+l.output_tokens+l.cache_creation_tokens+l.cache_read_tokens),0)::bigint,COALESCE(SUM(l.actual_cost),0)::text FROM usage_logs l LEFT JOIN groups g ON g.id=l.group_id WHERE l.user_id=$1 AND l.created_at >=$2 AND l.created_at <$3 GROUP BY g.id,g.name,l.model ORDER BY SUM(l.actual_cost) DESC,l.model LIMIT 500`, id, from, to)
	if err != nil {
		fail(c, err)
		return
	}
	defer rows.Close()
	items := []gin.H{}
	for rows.Next() {
		var group int64
		var groupName, model, cost string
		var requests, tokens int64
		if err = rows.Scan(&group, &groupName, &model, &requests, &tokens, &cost); err != nil {
			fail(c, err)
			return
		}
		items = append(items, gin.H{"group_id": group, "group_name": groupName, "model": model, "requests": requests, "tokens": tokens, "cost": cost})
	}
	if err = rows.Err(); err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"items": items, "from": from, "to": to, "user_id": strconv.FormatInt(id, 10)})
}
