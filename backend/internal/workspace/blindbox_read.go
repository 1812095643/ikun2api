package workspace

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/response"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

type blindboxRank struct {
	Name   string `json:"name"`
	Count  int    `json:"count"`
	Paid   string `json:"paid"`
	Reward string `json:"reward,omitempty"`
	Profit string `json:"profit,omitempty"`
	Mine   bool   `json:"mine"`
}

func blindboxPublicName(name string, id int64) string {
	name = strings.TrimSpace(name)
	if name == "" || strings.Contains(name, "@") {
		return "用户 " + strconv.FormatInt(id, 36)
	}
	runes := []rune(name)
	if len(runes) > 10 {
		return string(runes[:10]) + "…"
	}
	return name
}

func (h *Handler) getBlindbox(c *gin.Context) {
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
	tx, err := h.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		fail(c, err)
		return
	}
	defer tx.Rollback()
	s, err := loadBlindboxSettings(ctx, tx, false)
	if err != nil {
		fail(c, err)
		return
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		fail(c, err)
		return
	}
	today := checkinDay(now)
	day := c.DefaultQuery("day", today)
	if _, err = time.ParseInLocation("2006-01-02", day, lotteryLocation); err != nil || day > today {
		fail(c, badRequest("请选择今天或之前的日期。"))
		return
	}
	r, err := loadBlindboxRound(ctx, tx, 0, day, key, false)
	if errors.Is(err, sql.ErrNoRows) {
		deadline, _ := blindboxDeadline(day, s.blindboxSpec)
		r = blindboxRound{Date: day, Spec: s.blindboxSpec, DrawAt: deadline, Status: "open", TotalPaid: "0", TotalAwarded: "0"}
	} else if err != nil {
		fail(c, err)
		return
	}
	plan, err := makeBlindboxPlan(r.Count, r.Spec)
	if err != nil {
		fail(c, err)
		return
	}
	var usage string
	var checked bool
	var bought int
	start, _ := time.ParseInLocation("2006-01-02", today, lotteryLocation)
	if err = tx.QueryRowContext(ctx, blindboxUsageSQL, a.ID, start, start.AddDate(0, 0, 1)).Scan(&usage); err != nil {
		fail(c, err)
		return
	}
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM workspace_checkins WHERE user_id=$1 AND checkin_day=$2::date)`, a.ID, today).Scan(&checked); err != nil {
		fail(c, err)
		return
	}
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM workspace_blindbox_entries e JOIN workspace_blindbox_rounds r ON r.id=e.round_id WHERE e.user_id=$1 AND r.business_date=$2::date`, a.ID, today).Scan(&bought); err != nil {
		fail(c, err)
		return
	}
	allowance, err := blindboxAllowance(usage)
	if err != nil {
		fail(c, err)
		return
	}
	var participants int
	if err = tx.QueryRowContext(ctx, `SELECT count(DISTINCT user_id) FROM workspace_blindbox_entries WHERE round_id=$1`, r.ID).Scan(&participants); err != nil {
		fail(c, err)
		return
	}
	rows, err := tx.QueryContext(ctx, `SELECT e.user_id,u.username,count(*),SUM(e.paid)::text FROM workspace_blindbox_entries e JOIN users u ON u.id=e.user_id WHERE round_id=$1 GROUP BY e.user_id,u.username ORDER BY count(*) DESC,MIN(e.id) LIMIT 20`, r.ID)
	if err != nil {
		fail(c, err)
		return
	}
	ranking := []blindboxRank{}
	for rows.Next() {
		var item blindboxRank
		var userID int64
		var name string
		if err = rows.Scan(&userID, &name, &item.Count, &item.Paid); err != nil {
			break
		}
		item.Name = blindboxPublicName(name, userID)
		item.Mine = userID == a.ID
		ranking = append(ranking, item)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		fail(c, err)
		return
	}
	profitID := r.ID
	profitDate := day
	if r.Status != "settled" {
		err = tx.QueryRowContext(ctx, `SELECT id,business_date::text FROM workspace_blindbox_rounds WHERE status='settled' ORDER BY business_date DESC LIMIT 1`).Scan(&profitID, &profitDate)
		if errors.Is(err, sql.ErrNoRows) {
			profitID = 0
			profitDate = ""
		} else if err != nil {
			fail(c, err)
			return
		}
	}
	rows, err = tx.QueryContext(ctx, `SELECT e.user_id,u.username,count(*),SUM(e.paid)::text,SUM(e.reward)::text,(SUM(e.reward)-SUM(e.paid))::text FROM workspace_blindbox_entries e JOIN users u ON u.id=e.user_id JOIN workspace_blindbox_rounds r ON r.id=e.round_id WHERE e.round_id=$1 AND r.status='settled' GROUP BY e.user_id,u.username HAVING SUM(e.reward)>SUM(e.paid) ORDER BY SUM(e.reward)-SUM(e.paid) DESC,MIN(e.id) LIMIT 20`, profitID)
	if err != nil {
		fail(c, err)
		return
	}
	winners := []blindboxRank{}
	for rows.Next() {
		var item blindboxRank
		var userID int64
		var name string
		if err = rows.Scan(&userID, &name, &item.Count, &item.Paid, &item.Reward, &item.Profit); err != nil {
			break
		}
		item.Name = blindboxPublicName(name, userID)
		item.Mine = userID == a.ID
		winners = append(winners, item)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		fail(c, err)
		return
	}
	if err = tx.Commit(); err != nil {
		fail(c, err)
		return
	}
	reason := ""
	switch {
	case !s.Enabled:
		reason = "每日盲盒暂未开放"
	case day != today || r.Status != "open" || !now.Before(r.DrawAt):
		reason = "本场已截止"
	case !checked:
		reason = "先完成今日签到"
	case allowance == 0:
		reason = "今日尚无有效使用"
	case bought >= allowance:
		reason = "今日可购次数已用完"
	case r.Count >= blindboxCapacity:
		reason = "本场份数已满"
	}
	response.Success(c, gin.H{"enabled": s.Enabled, "server_time": now, "round": r, "plan": plan, "participants": participants, "ranking": ranking, "winners": winners, "winners_date": profitDate, "quote": blindboxQuote(key, day, r.Spec), "eligibility": gin.H{"checked_in": checked, "usage_amount": usage, "limit": allowance, "bought": bought, "remaining": max(0, allowance-bought), "can_buy": reason == "", "reason": reason}})
}

func (h *Handler) listBlindboxEntries(c *gin.Context) {
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
	where := " WHERE e.user_id=$1"
	args := []any{a.ID}
	if a.Admin {
		where = " WHERE true"
		args = []any{}
	}
	if raw := c.Query("round_id"); raw != "" {
		id, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || id < 1 {
			fail(c, badRequest("请检查场次编号。"))
			return
		}
		args = append(args, id)
		where += " AND e.round_id=$" + strconv.Itoa(len(args))
	}
	page, size := pageNumber(c.Query("page"), 1, 100000), pageNumber(c.Query("page_size"), 20, 100)
	var total int
	if err = h.db.QueryRowContext(ctx, `SELECT count(*)`+blindboxEntryFrom+where, args...).Scan(&total); err != nil {
		fail(c, err)
		return
	}
	args = append(args, size, (page-1)*size)
	rows, err := h.db.QueryContext(ctx, `SELECT `+blindboxEntryColumns+blindboxEntryFrom+where+` ORDER BY e.id DESC LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		fail(c, err)
		return
	}
	defer rows.Close()
	items := []blindboxEntry{}
	for rows.Next() {
		item, readErr := scanBlindboxEntry(rows)
		if readErr == nil {
			readErr = verifyBlindboxEntry(item, key)
		}
		if readErr != nil {
			fail(c, readErr)
			return
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"items": items, "total": total, "page": page, "page_size": size})
}

func (h *Handler) openBlindbox(c *gin.Context) {
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
	if err = decodeCheckinBody(c, &struct{}{}); err != nil {
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
	e, err := scanBlindboxEntry(tx.QueryRowContext(ctx, `SELECT `+blindboxEntryColumns+blindboxEntryFrom+` WHERE e.id=$1 AND e.user_id=$2 FOR UPDATE OF e`, id, a.ID))
	if err == nil {
		err = verifyBlindboxEntry(e, key)
	}
	if err != nil {
		fail(c, err)
		return
	}
	if e.Status != "settled" || e.Reward == nil {
		fail(c, badRequest("还没到开奖时间，请稍后再来。"))
		return
	}
	if err = tx.QueryRowContext(ctx, `UPDATE workspace_blindbox_entries SET opened_at=COALESCE(opened_at,clock_timestamp()) WHERE id=$1 RETURNING opened_at`, id).Scan(&e.OpenedAt); err != nil {
		fail(c, err)
		return
	}
	if err = tx.Commit(); err != nil {
		fail(c, err)
		return
	}
	profit := decimal.RequireFromString(*e.Reward).Sub(decimal.RequireFromString(e.Paid)).String()
	response.Success(c, gin.H{"entry": e, "profit": profit})
}
