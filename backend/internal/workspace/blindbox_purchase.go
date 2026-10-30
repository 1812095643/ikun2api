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
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

const blindboxUsageSQL = `SELECT COALESCE(SUM(actual_cost),0)::text FROM usage_logs WHERE user_id=$1 AND actual_cost>0 AND created_at >= $2 AND created_at < $3`

func blindboxQuote(key []byte, day string, s blindboxSpec) string {
	return lotterySign(key, "blindbox-purchase-quote-v1", []any{day, s})
}

func (h *Handler) purchaseBlindbox(ctx context.Context, userID int64, requestKey, quote string) (blindboxEntry, bool, error) {
	key, err := h.lotteryKey(ctx)
	if err != nil {
		return blindboxEntry{}, false, err
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return blindboxEntry{}, false, err
	}
	defer tx.Rollback()
	prior, err := scanBlindboxEntry(tx.QueryRowContext(ctx, `SELECT `+blindboxEntryColumns+blindboxEntryFrom+` WHERE e.user_id=$1 AND e.request_key=$2`, userID, requestKey))
	if err == nil {
		if err = verifyBlindboxEntry(prior, key); err != nil {
			return prior, false, err
		}
		return prior, true, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return blindboxEntry{}, false, err
	}
	s, err := loadBlindboxSettings(ctx, tx, true)
	if err != nil {
		return blindboxEntry{}, false, err
	}
	if !s.Enabled {
		return blindboxEntry{}, false, &requestError{403, "每日盲盒暂未开放。"}
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return blindboxEntry{}, false, err
	}
	day := checkinDay(now)
	deadline, err := blindboxDeadline(day, s.blindboxSpec)
	if err != nil {
		return blindboxEntry{}, false, err
	}
	r, err := loadBlindboxRound(ctx, tx, 0, day, key, false)
	if errors.Is(err, sql.ErrNoRows) {
		if !now.Before(deadline) {
			return blindboxEntry{}, false, badRequest("今日报名已截止，明天再来。")
		}
		var id int64
		if err = tx.QueryRowContext(ctx, `SELECT nextval(pg_get_serial_sequence('workspace_blindbox_rounds','id'))`).Scan(&id); err != nil {
			return blindboxEntry{}, false, err
		}
		r = blindboxRound{ID: id, Date: day, Spec: s.blindboxSpec, DrawAt: deadline}
		raw, _ := json.Marshal(r.Spec)
		if _, err = tx.ExecContext(ctx, `INSERT INTO workspace_blindbox_rounds(id,business_date,spec,signature,draw_at) VALUES($1,$2::date,$3,$4,$5) ON CONFLICT(business_date) DO NOTHING`, id, day, string(raw), blindboxRoundSign(key, r), deadline); err != nil {
			return blindboxEntry{}, false, err
		}
	} else if err != nil {
		return blindboxEntry{}, false, err
	}
	r, err = loadBlindboxRound(ctx, tx, 0, day, key, true)
	if err != nil {
		return blindboxEntry{}, false, err
	}
	// 所有购买和开奖统一先锁场次、再锁用户；同一日的不同设备请求串行校验额度和余额。
	var balance string
	if err = tx.QueryRowContext(ctx, `SELECT balance::text FROM users WHERE id=$1 AND status='active' AND deleted_at IS NULL FOR UPDATE`, userID).Scan(&balance); err != nil {
		return blindboxEntry{}, false, err
	}
	prior, err = scanBlindboxEntry(tx.QueryRowContext(ctx, `SELECT `+blindboxEntryColumns+blindboxEntryFrom+` WHERE e.user_id=$1 AND e.request_key=$2`, userID, requestKey))
	if err == nil {
		if err = verifyBlindboxEntry(prior, key); err != nil {
			return prior, false, err
		}
		return prior, true, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return blindboxEntry{}, false, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return blindboxEntry{}, false, err
	}
	if checkinDay(now) != day || !now.Before(r.DrawAt) || r.Status != "open" {
		return blindboxEntry{}, false, badRequest("本场报名已截止，请刷新查看开奖结果。")
	}
	if !lotterySignatureOK(quote, blindboxQuote(key, day, r.Spec)) {
		return blindboxEntry{}, false, &requestError{409, "本场规则已更新，请刷新后再参与。"}
	}
	if r.Count >= blindboxCapacity {
		return blindboxEntry{}, false, badRequest("本场份数已满。")
	}
	var checked bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM workspace_checkins WHERE user_id=$1 AND checkin_day=$2::date)`, userID, day).Scan(&checked); err != nil {
		return blindboxEntry{}, false, err
	}
	if !checked {
		return blindboxEntry{}, false, &requestError{403, "请先完成今日签到。"}
	}
	start, _ := time.ParseInLocation("2006-01-02", day, lotteryLocation)
	var usage string
	if err = tx.QueryRowContext(ctx, blindboxUsageSQL, userID, start, start.AddDate(0, 0, 1)).Scan(&usage); err != nil {
		return blindboxEntry{}, false, err
	}
	limit, err := blindboxAllowance(usage)
	if err != nil {
		return blindboxEntry{}, false, err
	}
	var bought int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM workspace_blindbox_entries WHERE round_id=$1 AND user_id=$2`, r.ID, userID).Scan(&bought); err != nil {
		return blindboxEntry{}, false, err
	}
	if bought >= limit {
		return blindboxEntry{}, false, &requestError{403, "今日可购次数已用完，使用额度增加后可再参与。"}
	}
	paid, _ := decimal.NewFromString(r.Spec.Price)
	available, err := decimal.NewFromString(balance)
	if err != nil {
		return blindboxEntry{}, false, err
	}
	if available.LessThan(paid) {
		return blindboxEntry{}, false, badRequest("余额不足，请充值后再参与。")
	}
	var id, debitID int64
	if err = tx.QueryRowContext(ctx, `SELECT nextval(pg_get_serial_sequence('workspace_blindbox_entries','id'))`).Scan(&id); err != nil {
		return blindboxEntry{}, false, err
	}
	var balanceAfter string
	if err = tx.QueryRowContext(ctx, `UPDATE users SET balance=balance-$1::numeric,updated_at=NOW() WHERE id=$2 AND balance >= $1::numeric RETURNING balance::text`, r.Spec.Price, userID).Scan(&balanceAfter); err != nil {
		return blindboxEntry{}, false, err
	}
	// 独立负数流水，既不计入模型消费解锁次数，也不计入充值或邀请返利。
	if err = tx.QueryRowContext(ctx, `INSERT INTO redeem_codes(code,type,value,status,used_by,used_at,notes,credit_source) VALUES($1,'admin_balance',-$2::numeric,'used',$3,NOW(),$4,'gift') RETURNING id`, "BBUY-"+strconv.FormatInt(id, 36), r.Spec.Price, userID, fmt.Sprintf("[盲盒购买] %s 第 %d 份", day, bought+1)).Scan(&debitID); err != nil {
		return blindboxEntry{}, false, err
	}
	usageValue, _ := decimal.NewFromString(usage)
	e := blindboxEntry{ID: id, RoundID: r.ID, UserID: userID, Date: day, Ordinal: bought + 1, Paid: r.Spec.Price, Usage: usageValue.String(), DebitID: debitID}
	if _, err = tx.ExecContext(ctx, `INSERT INTO workspace_blindbox_entries(id,round_id,user_id,request_key,paid,usage_amount,ordinal,debit_id,signature) VALUES($1,$2,$3,$4,$5::numeric,$6::numeric,$7,$8,$9)`, id, r.ID, userID, requestKey, e.Paid, e.Usage, e.Ordinal, debitID, blindboxEntrySign(key, e)); err != nil {
		return e, false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE workspace_blindbox_rounds SET entry_count=entry_count+1,total_paid=total_paid+$2::numeric WHERE id=$1`, r.ID, e.Paid); err != nil {
		return e, false, err
	}
	e, err = scanBlindboxEntry(tx.QueryRowContext(ctx, `SELECT `+blindboxEntryColumns+blindboxEntryFrom+` WHERE e.id=$1`, id))
	if err != nil {
		return e, false, err
	}
	return e, false, tx.Commit()
}

func (h *Handler) buyBlindbox(c *gin.Context) {
	a, err := h.identity(c)
	if err != nil {
		fail(c, err)
		return
	}
	var payload struct {
		RequestKey string `json:"request_key"`
		Quote      string `json:"quote"`
	}
	if err = decodeCheckinBody(c, &payload); err != nil {
		fail(c, err)
		return
	}
	if !blindboxRequestPattern.MatchString(payload.RequestKey) || len(payload.Quote) != 64 {
		fail(c, badRequest("请刷新页面后重新参与。"))
		return
	}
	e, duplicate, err := h.purchaseBlindbox(c.Request.Context(), a.ID, payload.RequestKey, payload.Quote)
	if err != nil {
		fail(c, err)
		return
	}
	invalidateCheckinBalance(h, c.Request.Context(), a.ID)
	response.Success(c, gin.H{"entry": e, "already_purchased": duplicate})
}
