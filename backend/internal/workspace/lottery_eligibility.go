package workspace

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// 充值按已入账余额统计：支付订单与其兑换流水去重，赠送、奖金和来源不明的旧流水均不计入。
const lotteryEvidenceSQL = `SELECT u.id,u.balance::text,u.created_at,
COALESCE((SELECT SUM(actual_cost) FROM usage_logs WHERE user_id=u.id AND created_at >= $2 AND created_at < $3),0)::text,
COALESCE((SELECT SUM(actual_cost) FROM usage_logs WHERE user_id=u.id),0)::text,
(COALESCE((SELECT SUM(value) FROM redeem_codes r WHERE r.used_by=u.id AND r.status='used' AND r.value>0
 AND r.type IN ('balance','admin_balance') AND r.credit_source IN ('manual','redeem') AND r.used_at >= $2 AND r.used_at < $3
 AND NOT EXISTS(SELECT 1 FROM payment_orders po WHERE po.recharge_code=r.code)),0)
 + COALESCE((SELECT SUM(amount) FROM payment_orders WHERE user_id=u.id AND order_type='balance' AND status='COMPLETED' AND paid_at >= $2 AND paid_at < $3),0))::text,
(COALESCE((SELECT SUM(value) FROM redeem_codes r WHERE r.used_by=u.id AND r.status='used' AND r.value>0
 AND r.type IN ('balance','admin_balance') AND r.credit_source IN ('manual','redeem')
 AND NOT EXISTS(SELECT 1 FROM payment_orders po WHERE po.recharge_code=r.code)),0)
 + COALESCE((SELECT SUM(amount) FROM payment_orders WHERE user_id=u.id AND order_type='balance' AND status='COMPLETED'),0))::text
FROM users u WHERE u.deleted_at IS NULL AND u.status='active'`

func lotteryDateBounds(s lotterySpec) (time.Time, time.Time) {
	start, _ := time.ParseInLocation("2006-01-02", s.BusinessDate, lotteryLocation)
	return start, start.AddDate(0, 0, 1)
}
func scanLotteryEvidence(row rowScanner, s lotterySpec) (lotteryEvidence, error) {
	e := lotteryEvidence{BusinessDate: s.BusinessDate}
	err := row.Scan(&e.UserID, &e.Balance, &e.RegisteredAt, &e.SpendToday, &e.SpendTotal, &e.RechargeToday, &e.RechargeTotal)
	return e, err
}
func lotteryMatches(s lotterySpec, e lotteryEvidence) bool {
	if len(s.Conditions) == 0 {
		return true
	}
	for _, c := range s.Conditions {
		match := false
		switch c.Kind {
		case "registered_before", "registered_after":
			date, _ := time.ParseInLocation("2006-01-02", c.Value, lotteryLocation)
			if c.Kind == "registered_before" {
				match = e.RegisteredAt.Before(date)
			} else {
				match = !e.RegisteredAt.Before(date)
			}
		case "user_ids":
			id := strconv.FormatInt(e.UserID, 10)
			for _, v := range strings.Split(c.Value, ",") {
				if id == v {
					match = true
					break
				}
			}
		default:
			values := map[string]string{"balance_min": e.Balance, "spend_today": e.SpendToday, "spend_total": e.SpendTotal, "recharge_today": e.RechargeToday, "recharge_total": e.RechargeTotal}
			value, err := decimal.NewFromString(values[c.Kind])
			threshold, err2 := decimal.NewFromString(c.Value)
			if err == nil && err2 == nil {
				match = value.GreaterThan(threshold)
			}
		}
		if s.Match == "all" && !match {
			return false
		}
		if s.Match == "any" && match {
			return true
		}
	}
	return s.Match == "all"
}
func (h *Handler) lotteryEligibility(ctx context.Context, db lotteryDB, s lotterySpec, userID int64) (lotteryEvidence, bool, error) {
	start, end := lotteryDateBounds(s)
	e, err := scanLotteryEvidence(db.QueryRowContext(ctx, lotteryEvidenceSQL+" AND u.id=$1", userID, start, end), s)
	if err != nil {
		return e, false, err
	}
	return e, lotteryMatches(s, e), nil
}

func (h *Handler) lotteryPreview(ctx context.Context, s lotterySpec) (int, []map[string]any, error) {
	start, end := lotteryDateBounds(s)
	rows, err := h.db.QueryContext(ctx, lotteryEvidenceSQL+" AND u.id >= $1 ORDER BY u.id LIMIT 100001", int64(1), start, end)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	count := 0
	scanned := 0
	samples := []map[string]any{}
	for rows.Next() {
		scanned++
		if scanned > 100000 {
			return 0, nil, badRequest("用户超过单场活动核验上限，请缩小范围。")
		}
		e, err := scanLotteryEvidence(rows, s)
		if err != nil {
			return 0, nil, err
		}
		if lotteryMatches(s, e) {
			count++
			if len(samples) < 20 {
				samples = append(samples, map[string]any{"user_id": e.UserID, "evidence": e})
			}
		}
	}
	return count, samples, rows.Err()
}
