package workspace

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/response"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

type checkinRecord struct {
	ID              int64     `json:"id"`
	Day             string    `json:"day"`
	Amount          string    `json:"amount"`
	ConsecutiveDays int       `json:"consecutive_days"`
	Mode            string    `json:"mode"`
	CreatedAt       time.Time `json:"created_at"`
}

const checkinRecordColumns = `id,checkin_day::text,amount::text,consecutive_days,reward_mode,created_at`

func scanCheckinRecord(row rowScanner) (checkinRecord, error) {
	var item checkinRecord
	err := row.Scan(&item.ID, &item.Day, &item.Amount, &item.ConsecutiveDays, &item.Mode, &item.CreatedAt)
	if err == nil {
		amount, parseErr := decimal.NewFromString(item.Amount)
		if parseErr != nil {
			return item, parseErr
		}
		item.Amount = amount.String()
	}
	return item, err
}

func checkinDay(now time.Time) string { return now.In(lotteryLocation).Format("2006-01-02") }

func checkinStreak(last checkinRecord, today string) int {
	day, err := time.ParseInLocation("2006-01-02", today, lotteryLocation)
	if err != nil {
		return 0
	}
	if last.Day == today || last.Day == day.AddDate(0, 0, -1).Format("2006-01-02") {
		return last.ConsecutiveDays
	}
	return 0
}

// 签到、余额和充值流水在同一事务完成。先锁定用户，再读取数据库当前日期，防止并发及跨日等待重复领款。
func (h *Handler) creditCheckin(ctx context.Context, userID int64) (checkinRecord, bool, error) {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return checkinRecord{}, false, err
	}
	defer tx.Rollback()
	var lockedID int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 AND status='active' AND deleted_at IS NULL FOR UPDATE`, userID).Scan(&lockedID); err != nil {
		return checkinRecord{}, false, err
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return checkinRecord{}, false, err
	}
	today := checkinDay(now)
	item, err := scanCheckinRecord(tx.QueryRowContext(ctx, `SELECT `+checkinRecordColumns+` FROM workspace_checkins WHERE user_id=$1 AND checkin_day=$2::date`, userID, today))
	if err == nil {
		return item, true, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return checkinRecord{}, false, err
	}
	s, err := loadCheckinSettings(ctx, tx, true)
	if err != nil {
		return checkinRecord{}, false, err
	}
	if !s.Enabled {
		return checkinRecord{}, false, &requestError{403, "每日签到暂未开放。"}
	}
	last, err := scanCheckinRecord(tx.QueryRowContext(ctx, `SELECT `+checkinRecordColumns+` FROM workspace_checkins WHERE user_id=$1 ORDER BY checkin_day DESC LIMIT 1`, userID))
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return checkinRecord{}, false, err
	}
	streak := checkinStreak(last, today) + 1
	amount, minimum, maximum, err := checkinReward(s)
	if err != nil {
		return checkinRecord{}, false, err
	}
	// 签到是赠送余额，沿用现有赠送来源，因此不会计入充值抽奖资格或邀请返利。
	code := fmt.Sprintf("CHECKIN-%s-%s", strconv.FormatInt(userID, 36), now.In(lotteryLocation).Format("20060102"))
	redeemID, balance, err := service.CreditAdminBalanceTx(ctx, tx, userID, amount, code, "[赠送] 每日签到 "+today, "gift")
	if err != nil {
		return checkinRecord{}, false, err
	}
	item, err = scanCheckinRecord(tx.QueryRowContext(ctx, `INSERT INTO workspace_checkins(user_id,checkin_day,amount,consecutive_days,reward_mode,reward_min,reward_max,redeem_id,balance_after,created_at)
		VALUES($1,$2::date,$3::numeric,$4,$5,$6::numeric,$7::numeric,$8,$9,clock_timestamp()) RETURNING `+checkinRecordColumns,
		userID, today, amount, streak, s.Mode, minimum, maximum, redeemID, balance))
	if err != nil {
		return checkinRecord{}, false, err
	}
	return item, false, tx.Commit()
}

func (h *Handler) submitCheckin(c *gin.Context) {
	a, err := h.identity(c)
	if err != nil {
		fail(c, err)
		return
	}
	// 不接受用户、金额、日期等客户端参数，领取依据完全由当前登录身份和服务端规则决定。
	if err = decodeCheckinBody(c, &struct{}{}); err != nil {
		fail(c, err)
		return
	}
	item, duplicate, err := h.creditCheckin(c.Request.Context(), a.ID)
	if err != nil {
		fail(c, err)
		return
	}
	invalidateCheckinBalance(h, c.Request.Context(), a.ID)
	response.Success(c, gin.H{"record": item, "already_checked_in": duplicate})
}

func (h *Handler) getCheckin(c *gin.Context) {
	a, err := h.identity(c)
	if err != nil {
		fail(c, err)
		return
	}
	ctx := c.Request.Context()
	tx, err := h.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		fail(c, err)
		return
	}
	defer tx.Rollback()
	s, err := loadCheckinSettings(ctx, tx, false)
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
	var total int
	var totalAmount string
	if err = tx.QueryRowContext(ctx, `SELECT count(*),COALESCE(SUM(amount),0)::text FROM workspace_checkins WHERE user_id=$1`, a.ID).Scan(&total, &totalAmount); err != nil {
		fail(c, err)
		return
	}
	last, err := scanCheckinRecord(tx.QueryRowContext(ctx, `SELECT `+checkinRecordColumns+` FROM workspace_checkins WHERE user_id=$1 ORDER BY checkin_day DESC LIMIT 1`, a.ID))
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		fail(c, err)
		return
	}
	page, size := pageNumber(c.Query("page"), 1, 100000), pageNumber(c.Query("page_size"), 20, 100)
	rows, err := tx.QueryContext(ctx, `SELECT `+checkinRecordColumns+` FROM workspace_checkins WHERE user_id=$1 ORDER BY checkin_day DESC LIMIT $2 OFFSET $3`, a.ID, size, (page-1)*size)
	if err != nil {
		fail(c, err)
		return
	}
	items := []checkinRecord{}
	for rows.Next() {
		item, scanErr := scanCheckinRecord(rows)
		if scanErr != nil {
			rows.Close()
			fail(c, scanErr)
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
	month := c.DefaultQuery("month", today[:7])
	monthStart, monthErr := time.ParseInLocation("2006-01", month, lotteryLocation)
	if monthErr != nil || len(month) != 7 {
		fail(c, badRequest("请选择有效月份。"))
		return
	}
	monthRows, err := tx.QueryContext(ctx, `SELECT `+checkinRecordColumns+` FROM workspace_checkins WHERE user_id=$1 AND checkin_day >= $2::date AND checkin_day < $3::date ORDER BY checkin_day`, a.ID, monthStart.Format("2006-01-02"), monthStart.AddDate(0, 1, 0).Format("2006-01-02"))
	if err != nil {
		fail(c, err)
		return
	}
	monthItems := []checkinRecord{}
	for monthRows.Next() {
		item, readErr := scanCheckinRecord(monthRows)
		if readErr != nil {
			monthRows.Close()
			fail(c, readErr)
			return
		}
		monthItems = append(monthItems, item)
	}
	err = monthRows.Err()
	monthRows.Close()
	if err != nil {
		fail(c, err)
		return
	}
	if err = tx.Commit(); err != nil {
		fail(c, err)
		return
	}
	totalValue, err := decimal.NewFromString(totalAmount)
	if err != nil {
		fail(c, err)
		return
	}
	day, _ := time.ParseInLocation("2006-01-02", today, lotteryLocation)
	todayAmount := "0"
	if last.Day == today {
		todayAmount = last.Amount
	}
	response.Success(c, gin.H{
		"settings": s, "today": today, "server_time": now, "next_reset": day.AddDate(0, 0, 1),
		"checked_in": last.Day == today, "today_amount": todayAmount,
		"summary": gin.H{"total_days": total, "consecutive_days": checkinStreak(last, today), "total_amount": totalValue.String()},
		"items":   items, "total": total, "page": page, "page_size": size,
		"month": month, "month_items": monthItems,
	})
}
