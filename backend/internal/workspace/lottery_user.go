package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/response"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

func (h *Handler) listLotteries(c *gin.Context) {
	a, err := h.identity(c)
	if err != nil {
		fail(c, err)
		return
	}
	ctx := c.Request.Context()
	key, err := h.lotteryKey(ctx)
	if err != nil {
		fail(c, err)
		return
	}
	page, size := pageNumber(c.Query("page"), 1, 100000), pageNumber(c.Query("page_size"), 12, 50)
	filter := " WHERE status IN ('open','drawn')"
	if a.Admin {
		filter = " WHERE true"
	}
	status := c.Query("status")
	args := []any{}
	if status != "" {
		if status != "draft" && status != "open" && status != "drawn" && status != "cancelled" {
			fail(c, badRequest("活动状态无法识别。"))
			return
		}
		args = append(args, status)
		filter += " AND status=$1"
	}
	var total int
	if err = h.db.QueryRowContext(ctx, "SELECT count(*) FROM workspace_lotteries"+filter, args...).Scan(&total); err != nil {
		fail(c, err)
		return
	}
	args = append(args, size, (page-1)*size)
	rows, err := h.db.QueryContext(ctx, "SELECT "+lotteryColumns+" FROM workspace_lotteries"+filter+" ORDER BY id DESC LIMIT $"+strconv.Itoa(len(args)-1)+" OFFSET $"+strconv.Itoa(len(args)), args...)
	if err != nil {
		fail(c, err)
		return
	}
	items := []lotteryActivity{}
	for rows.Next() {
		item, e := scanLottery(rows)
		if e != nil {
			rows.Close()
			fail(c, e)
			return
		}
		if !lotterySignatureOK(item.Signature, lotterySpecSignature(key, item)) {
			rows.Close()
			fail(c, &requestError{409, "活动记录校验未通过，请联系管理员。"})
			return
		}
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		fail(c, err)
		return
	}
	for i := range items {
		if err = h.fillLottery(ctx, &items[i], a, false); err != nil {
			fail(c, err)
			return
		}
	}
	response.Success(c, gin.H{"items": items, "total": total, "page": page, "page_size": size, "server_time": time.Now()})
}

func (h *Handler) getLottery(c *gin.Context) {
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
	ctx := c.Request.Context()
	key, err := h.lotteryKey(ctx)
	if err != nil {
		fail(c, err)
		return
	}
	item, err := h.loadLottery(ctx, h.db, id, false, key)
	if err != nil {
		fail(c, err)
		return
	}
	if !a.Admin && item.Status == "draft" {
		fail(c, notFound())
		return
	}
	if err = h.fillLottery(ctx, &item, a, true); err != nil {
		fail(c, err)
		return
	}
	response.Success(c, item)
}

func (h *Handler) fillLottery(ctx context.Context, item *lotteryActivity, a actor, detail bool) error {
	if err := h.db.QueryRowContext(ctx, `SELECT count(*),COALESCE(bool_or(user_id=$2),false) FROM workspace_lottery_entries WHERE lottery_id=$1`, item.ID, a.ID).Scan(&item.EntryCount, &item.Joined); err != nil {
		return err
	}
	if item.Status == "open" && !item.Joined {
		_, eligible, err := h.lotteryEligibility(ctx, h.db, item.Spec, a.ID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		item.Eligible = eligible
		if !eligible {
			item.EligibilityMessage = "暂未满足报名条件"
		}
	}
	win, err := h.loadLotteryWin(ctx, h.db, 0, item.ID, a.ID, false)
	if err == nil {
		item.MyWin = &win
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if detail && item.Status == "drawn" {
		rows, err := h.db.QueryContext(ctx, lotteryWinSQL+` WHERE w.lottery_id=$1 ORDER BY w.id LIMIT 100`, item.ID)
		if err != nil {
			return err
		}
		item.Winners = []lotteryWin{}
		for rows.Next() {
			w, e := scanLotteryWin(rows)
			if e != nil {
				rows.Close()
				return e
			}
			if !a.Admin {
				w.ID = 0
				w.UserID = 0
				w.UserEmail = ""
				w.UserName = maskLotteryName(w.UserName)
				w.RedeemID = nil
				w.BalanceAfter = nil
			}
			item.Winners = append(item.Winners, w)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	if detail && a.Admin {
		events, err := h.lotteryEvents(ctx, item.ID)
		if err != nil {
			return err
		}
		item.Events = events
	}
	return nil
}
func maskLotteryName(name string) string {
	r := []rune(name)
	if len(r) == 0 {
		return "幸运用户"
	}
	return string(r[0]) + "***"
}

func (h *Handler) joinLottery(c *gin.Context) {
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
	var payload struct{}
	if err = lotteryBody(c, &payload); err != nil {
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
	item, err := h.loadLottery(ctx, tx, id, true, key)
	if err != nil {
		fail(c, err)
		return
	}
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM workspace_lottery_entries WHERE lottery_id=$1 AND user_id=$2)`, id, a.ID).Scan(&exists); err != nil {
		fail(c, err)
		return
	}
	if exists {
		response.Success(c, gin.H{"joined": true, "already_joined": true})
		return
	}
	if item.Status != "open" {
		fail(c, badRequest("这场活动已停止报名。"))
		return
	}
	if time.Now().Before(item.Spec.StartAt) {
		fail(c, badRequest("报名尚未开始。"))
		return
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM workspace_lottery_entries WHERE lottery_id=$1`, id).Scan(&count); err != nil {
		fail(c, err)
		return
	}
	if lotteryDue(item, count, time.Now()) || (item.Spec.Capacity > 0 && count >= item.Spec.Capacity) {
		fail(c, badRequest("报名已截止，请等待开奖结果。"))
		return
	}
	evidence, eligible, err := h.lotteryEligibility(ctx, tx, item.Spec, a.ID)
	if err != nil {
		fail(c, err)
		return
	}
	if !eligible {
		fail(c, &requestError{403, "暂未满足报名条件。"})
		return
	}
	raw, _ := json.Marshal(evidence)
	signature := lotterySign(key, "entry", []any{id, a.ID, evidence})
	if _, err = tx.ExecContext(ctx, `INSERT INTO workspace_lottery_entries(lottery_id,user_id,evidence,signature) VALUES($1,$2,$3,$4)`, id, a.ID, string(raw), signature); err != nil {
		fail(c, err)
		return
	}
	if err = lotteryAudit(ctx, tx, id, &a.ID, "join", gin.H{}); err != nil {
		fail(c, err)
		return
	}
	if err = tx.Commit(); err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"joined": true, "entry_count": count + 1, "draw_pending": lotteryDue(item, count+1, time.Now())})
}

const lotteryWinSQL = `SELECT w.id,w.lottery_id,w.entry_id,w.user_id,l.spec->>'title',u.email,u.username,
w.prize_key,w.amount::text,w.signature,w.created_at,w.claimed_at,w.redeem_id,w.balance_after,
(SELECT count(*) FROM workspace_lottery_winners cw WHERE cw.user_id=w.user_id)
FROM workspace_lottery_winners w JOIN workspace_lotteries l ON l.id=w.lottery_id JOIN users u ON u.id=w.user_id`

func scanLotteryWin(row rowScanner) (lotteryWin, error) {
	w := lotteryWin{}
	err := row.Scan(&w.ID, &w.LotteryID, &w.EntryID, &w.UserID, &w.Title, &w.UserEmail, &w.UserName, &w.PrizeKey, &w.Amount, &w.Signature, &w.CreatedAt, &w.ClaimedAt, &w.RedeemID, &w.BalanceAfter, &w.WinCount)
	if err == nil {
		amount, e := decimal.NewFromString(w.Amount)
		if e != nil {
			return w, e
		}
		w.Amount = amount.String()
	}
	return w, err
}
func (h *Handler) loadLotteryWin(ctx context.Context, db lotteryDB, id, lotteryID, userID int64, lock bool) (lotteryWin, error) {
	suffix := ""
	if lock {
		suffix = " FOR UPDATE OF w"
	}
	return scanLotteryWin(db.QueryRowContext(ctx, lotteryWinSQL+` WHERE ($1::bigint=0 OR w.id=$1) AND ($2::bigint=0 OR w.lottery_id=$2) AND w.user_id=$3`+suffix, id, lotteryID, userID))
}

func (h *Handler) listLotteryWins(c *gin.Context) {
	a, err := h.identity(c)
	if err != nil {
		fail(c, err)
		return
	}
	page, size := pageNumber(c.Query("page"), 1, 1000000), pageNumber(c.Query("page_size"), 20, 100)
	ctx := c.Request.Context()
	rows, err := h.db.QueryContext(ctx, lotteryWinSQL+` WHERE w.user_id=$1 ORDER BY w.id DESC LIMIT $2 OFFSET $3`, a.ID, size, (page-1)*size)
	if err != nil {
		fail(c, err)
		return
	}
	defer rows.Close()
	items := []lotteryWin{}
	for rows.Next() {
		w, e := scanLotteryWin(rows)
		if e != nil {
			fail(c, e)
			return
		}
		items = append(items, w)
	}
	if err = rows.Err(); err != nil {
		fail(c, err)
		return
	}
	var total int
	var amount string
	if err = h.db.QueryRowContext(ctx, `SELECT count(*),COALESCE(SUM(amount),0)::text FROM workspace_lottery_winners WHERE user_id=$1`, a.ID).Scan(&total, &amount); err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"items": items, "total": total, "total_amount": amount, "page": page, "page_size": size})
}

func (h *Handler) verifyLotteryWin(ctx context.Context, db lotteryDB, w lotteryWin, key []byte) error {
	item, err := h.loadLottery(ctx, db, w.LotteryID, false, key)
	if err != nil {
		return err
	}
	if item.Status != "drawn" || !lotterySignatureOK(w.Signature, lotteryWinSignature(key, w)) {
		return &requestError{409, "中奖记录校验未通过，请联系管理员。"}
	}
	matched := false
	for _, p := range item.Spec.Prizes {
		if p.Key == w.PrizeKey && p.Amount == w.Amount {
			matched = true
			break
		}
	}
	if !matched {
		return &requestError{409, "奖金与已发布的活动规则不一致。"}
	}
	return nil
}
func (h *Handler) getLotteryWin(c *gin.Context) {
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
	ctx := c.Request.Context()
	key, err := h.lotteryKey(ctx)
	if err != nil {
		fail(c, err)
		return
	}
	w, err := h.loadLotteryWin(ctx, h.db, id, 0, a.ID, false)
	if err == nil {
		err = h.verifyLotteryWin(ctx, h.db, w, key)
	}
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, w)
}

func (h *Handler) claimLotteryWin(c *gin.Context) {
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
		fail(c, badRequest("请确认领取奖金。"))
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
	w, err := h.loadLotteryWin(ctx, tx, id, 0, a.ID, true)
	if err != nil {
		fail(c, err)
		return
	}
	if err = h.verifyLotteryWin(ctx, tx, w, key); err != nil {
		fail(c, err)
		return
	}
	if w.ClaimedAt == nil {
		// 用户只提交中奖记录编号；金额、获奖人和流水编号始终从服务端已签名记录读取。
		code := fmt.Sprintf("LOTTERY-%d", w.ID)
		redeem, balance, e := service.CreditAdminBalanceTx(ctx, tx, a.ID, w.Amount, code, fmt.Sprintf("抽奖奖励 #%d · %s · %s", w.LotteryID, w.Title, lotteryPrizeNames[w.PrizeKey]), "lottery")
		if e != nil {
			fail(c, e)
			return
		}
		if _, err = tx.ExecContext(ctx, `UPDATE workspace_lottery_winners SET claimed_at=NOW(),redeem_id=$2,balance_after=$3 WHERE id=$1`, id, redeem, balance); err != nil {
			fail(c, err)
			return
		}
		if err = lotteryAudit(ctx, tx, w.LotteryID, &a.ID, "claim", gin.H{"win_id": id, "redeem_id": redeem, "amount": w.Amount}); err != nil {
			fail(c, err)
			return
		}
	}
	if err = tx.Commit(); err != nil {
		fail(c, err)
		return
	}
	cacheCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if invalidator, ok := h.admin.(interface{ InvalidateBalanceAfterCredit(context.Context, int64) }); ok {
		invalidator.InvalidateBalanceAfterCredit(cacheCtx, a.ID)
	}
	w, err = h.loadLotteryWin(ctx, h.db, id, 0, a.ID, false)
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, w)
}
