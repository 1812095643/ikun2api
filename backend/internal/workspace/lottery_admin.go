package workspace

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

func lotteryBody(c *gin.Context, target any) error {
	c.Request.Body = httpMaxLotteryBody(c)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return badRequest("提交内容无法读取，请检查填写的字段。")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return badRequest("一次只能提交一份内容。")
	}
	return nil
}
func httpMaxLotteryBody(c *gin.Context) io.ReadCloser {
	return http.MaxBytesReader(c.Writer, c.Request.Body, 64*1024)
}

func (h *Handler) previewLottery(c *gin.Context) {
	var s lotterySpec
	if err := lotteryBody(c, &s); err != nil {
		fail(c, err)
		return
	}
	if err := normalizeLotterySpec(&s); err != nil {
		fail(c, err)
		return
	}
	count, samples, err := h.lotteryPreview(c.Request.Context(), s)
	if err != nil {
		fail(c, err)
		return
	}
	total, prizes := lotteryTotals(s)
	response.Success(c, gin.H{"eligible_count": count, "samples": samples, "prize_total": total, "prize_count": prizes, "spec": s})
}

func (h *Handler) saveLottery(c *gin.Context) {
	a, err := h.identity(c)
	if err != nil {
		fail(c, err)
		return
	}
	var spec lotterySpec
	if err = lotteryBody(c, &spec); err != nil {
		fail(c, err)
		return
	}
	if err = normalizeLotterySpec(&spec); err != nil {
		fail(c, err)
		return
	}
	ctx := c.Request.Context()
	key, err := h.lotteryKey(ctx)
	if err != nil {
		fail(c, err)
		return
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		fail(c, err)
		return
	}
	defer tx.Rollback()
	item := lotteryActivity{Spec: spec, CreatedBy: a.ID, Status: "draft"}
	if c.Param("id") != "" {
		id, e := positiveID(c)
		if e != nil {
			fail(c, e)
			return
		}
		item, e = h.loadLottery(ctx, tx, id, true, key)
		if e != nil {
			fail(c, e)
			return
		}
		if item.Status != "draft" {
			fail(c, badRequest("已发布的活动不能修改规则，请创建新活动。"))
			return
		}
		item.Spec = spec
	} else {
		raw, _ := json.Marshal(spec)
		if err = tx.QueryRowContext(ctx, `INSERT INTO workspace_lotteries(spec,signature,created_by) VALUES($1,'',$2) RETURNING id`, string(raw), a.ID).Scan(&item.ID); err != nil {
			fail(c, err)
			return
		}
	}
	raw, _ := json.Marshal(spec)
	item.Signature = lotterySpecSignature(key, item)
	if _, err = tx.ExecContext(ctx, `UPDATE workspace_lotteries SET spec=$1,signature=$2 WHERE id=$3`, string(raw), item.Signature, item.ID); err != nil {
		fail(c, err)
		return
	}
	if err = lotteryAudit(ctx, tx, item.ID, &a.ID, "save", gin.H{"spec": spec}); err != nil {
		fail(c, err)
		return
	}
	if err = tx.Commit(); err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"id": item.ID, "status": "draft"})
}

func (h *Handler) publishLottery(c *gin.Context) { h.lotteryTransition(c, "publish") }
func (h *Handler) cancelLottery(c *gin.Context)  { h.lotteryTransition(c, "cancel") }
func (h *Handler) lotteryTransition(c *gin.Context, action string) {
	a, err := h.identity(c)
	if err != nil {
		fail(c, err)
		return
	}
	id, err := positiveID(c)
	if err != nil {
		fail(c, err)
		return
	}
	var payload struct {
		Confirm bool `json:"confirm"`
	}
	if err = lotteryBody(c, &payload); err != nil {
		fail(c, err)
		return
	}
	if !payload.Confirm {
		fail(c, badRequest("请确认本次操作。"))
		return
	}
	ctx := c.Request.Context()
	key, err := h.lotteryKey(ctx)
	if err != nil {
		fail(c, err)
		return
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		fail(c, err)
		return
	}
	defer tx.Rollback()
	item, err := h.loadLottery(ctx, tx, id, true, key)
	if err != nil {
		fail(c, err)
		return
	}
	if (action == "publish" && item.Status == "open") || (action == "cancel" && item.Status == "cancelled") {
		response.Success(c, gin.H{"status": item.Status})
		return
	}
	if action == "publish" {
		if item.Status != "draft" {
			fail(c, badRequest("仅草稿可以发布。"))
			return
		}
		if item.Spec.DrawAt != nil && !item.Spec.DrawAt.After(time.Now()) {
			fail(c, badRequest("开奖时间已过，请先修改草稿。"))
			return
		}
		if _, err = tx.ExecContext(ctx, `UPDATE workspace_lotteries SET status='open',published_at=NOW() WHERE id=$1`, id); err != nil {
			fail(c, err)
			return
		}
	} else {
		if item.Status != "draft" && item.Status != "open" {
			fail(c, badRequest("已开奖活动无法取消。"))
			return
		}
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM workspace_lottery_entries WHERE lottery_id=$1`, id).Scan(&count); err != nil {
			fail(c, err)
			return
		}
		if lotteryDue(item, count, time.Now()) {
			fail(c, badRequest("活动已达到开奖条件，请等待系统开奖。"))
			return
		}
		if _, err = tx.ExecContext(ctx, `UPDATE workspace_lotteries SET status='cancelled' WHERE id=$1`, id); err != nil {
			fail(c, err)
			return
		}
	}
	if err = lotteryAudit(ctx, tx, id, &a.ID, action, gin.H{}); err != nil {
		fail(c, err)
		return
	}
	if err = tx.Commit(); err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"success": true})
}

func (h *Handler) lotteryEvents(ctx context.Context, id int64) ([]lotteryEvent, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT id,action,actor_id,detail,created_at FROM workspace_lottery_events WHERE lottery_id=$1 ORDER BY id DESC LIMIT 200`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []lotteryEvent{}
	for rows.Next() {
		var e lotteryEvent
		if err = rows.Scan(&e.ID, &e.Action, &e.ActorID, &e.Detail, &e.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, e)
	}
	return items, rows.Err()
}
