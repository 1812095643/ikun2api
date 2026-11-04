package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	dbent "github.com/MACOS-DO/sub4api/ent"
	infraerrors "github.com/MACOS-DO/sub4api/internal/pkg/errors"
	"github.com/MACOS-DO/sub4api/internal/pkg/timezone"
	"github.com/google/uuid"
	"math"
	"reflect"
	"strings"
	"time"
)

const UniversalPlanType = "universal_credit"
const FundingSourceBalance = "balance"
const FundingSourceUniversal = "universal_subscription"

var ErrUniversalUnavailable = infraerrors.ServiceUnavailable("UNIVERSAL_SUBSCRIPTION_UNAVAILABLE", "订阅计费暂时不可用，请稍后重试")
var ErrUniversalNotActive = infraerrors.Forbidden("UNIVERSAL_SUBSCRIPTION_NOT_ACTIVE", "没有已生效的通用订阅，或订阅已到期；不会扣除普通余额")
var ErrUniversalExhausted = infraerrors.Forbidden("UNIVERSAL_SUBSCRIPTION_EXHAUSTED", "订阅总额度已用尽；不会扣除普通余额")
var ErrLegacySubscriptionRetired = infraerrors.Forbidden("LEGACY_SUBSCRIPTION_RETIRED", "旧分组订阅已停用，请改用通用订阅；历史记录仍保留")

// Snapshot is immutable for a purchased/granted term. Prices and credits use separate units.
type UniversalPlanSnapshot struct {
	PlanID          int64    `json:"plan_id"`
	Name            string   `json:"name"`
	Price           float64  `json:"price"`
	Currency        string   `json:"currency"`
	ValidityDays    int      `json:"validity_days"`
	TotalCreditUSD  *float64 `json:"total_credit_usd"`
	DailyLimitUSD   *float64 `json:"daily_limit_usd"`
	WeeklyLimitUSD  *float64 `json:"weekly_limit_usd"`
	MonthlyLimitUSD *float64 `json:"monthly_limit_usd"`
}

func UniversalSnapshot(plan *dbent.SubscriptionPlan) UniversalPlanSnapshot {
	return UniversalPlanSnapshot{PlanID: plan.ID, Name: plan.Name, Price: plan.Price, Currency: plan.Currency, ValidityDays: psComputeValidityDays(plan.ValidityDays, plan.ValidityUnit), TotalCreditUSD: plan.TotalCreditUsd, DailyLimitUSD: plan.DailyLimitUsd, WeeklyLimitUSD: plan.WeeklyLimitUsd, MonthlyLimitUSD: plan.MonthlyLimitUsd}
}
func (p UniversalPlanSnapshot) Validate() error {
	if strings.TrimSpace(p.Name) == "" || len([]rune(p.Name)) > 100 || p.ValidityDays < 1 || p.ValidityDays > 36500 {
		return infraerrors.BadRequest("INVALID_SUBSCRIPTION_PLAN", "套餐名称或有效期无效")
	}
	if p.TotalCreditUSD == nil {
		return infraerrors.BadRequest("INVALID_SUBSCRIPTION_PLAN", "请设置套餐总额度")
	}
	for _, v := range []*float64{p.TotalCreditUSD, p.DailyLimitUSD, p.WeeklyLimitUSD, p.MonthlyLimitUSD} {
		if v != nil && (math.IsNaN(*v) || math.IsInf(*v, 0) || *v < 0.00000001 || *v > 1e9) {
			return infraerrors.BadRequest("INVALID_SUBSCRIPTION_PLAN", "额度须为大于零的有限数值；未设置的周期上限表示不限")
		}
	}
	return nil
}
func DecodeUniversalSnapshot(raw map[string]any) (UniversalPlanSnapshot, error) {
	var p UniversalPlanSnapshot
	data, err := json.Marshal(raw)
	if err != nil {
		return p, err
	}
	err = json.Unmarshal(data, &p)
	if err != nil {
		return p, err
	}
	return p, p.Validate()
}
func (p UniversalPlanSnapshot) Map() map[string]any {
	data, _ := json.Marshal(p)
	var out map[string]any
	_ = json.Unmarshal(data, &out)
	return out
}

type UniversalSubscription struct {
	ID                 int64     `json:"id"`
	UserID             int64     `json:"user_id"`
	PlanID             *int64    `json:"plan_id"`
	OrderID            *int64    `json:"order_id"`
	Name               string    `json:"name"`
	UserEmail          string    `json:"user_email,omitempty"`
	UserName           string    `json:"user_name,omitempty"`
	StartsAt           time.Time `json:"starts_at"`
	ExpiresAt          time.Time `json:"expires_at"`
	Status             string    `json:"status"`
	TotalCreditUSD     *float64  `json:"total_credit_usd"`
	DailyLimitUSD      *float64  `json:"daily_limit_usd"`
	WeeklyLimitUSD     *float64  `json:"weekly_limit_usd"`
	MonthlyLimitUSD    *float64  `json:"monthly_limit_usd"`
	TotalUsedUSD       float64   `json:"total_used_usd"`
	DailyUsedUSD       float64   `json:"daily_used_usd"`
	WeeklyUsedUSD      float64   `json:"weekly_used_usd"`
	MonthlyUsedUSD     float64   `json:"monthly_used_usd"`
	DailyWindowStart   time.Time `json:"daily_window_start"`
	WeeklyWindowStart  time.Time `json:"weekly_window_start"`
	MonthlyWindowStart time.Time `json:"monthly_window_start"`
	DailyResetAt       time.Time `json:"daily_reset_at"`
	WeeklyResetAt      time.Time `json:"weekly_reset_at"`
	MonthlyResetAt     time.Time `json:"monthly_reset_at"`
	HeldUSD            float64   `json:"held_usd"`
	RemainingUSD       float64   `json:"remaining_usd"`
	Notes              string    `json:"notes"`
	Version            int64     `json:"version"`
	CreatedAt          time.Time `json:"created_at"`
}

const universalColumns = `s.id,s.user_id,s.plan_id,s.order_id,s.name,s.starts_at,s.expires_at,s.status,s.total_credit_usd,s.daily_limit_usd,s.weekly_limit_usd,s.monthly_limit_usd,s.total_used_usd,s.daily_used_usd,s.weekly_used_usd,s.monthly_used_usd,s.daily_window_start,s.weekly_window_start,s.monthly_window_start,s.notes,s.version,s.created_at`

type universalSQL interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func scanUniversal(rows *sql.Rows) (*UniversalSubscription, error) {
	x := new(UniversalSubscription)
	err := rows.Scan(&x.ID, &x.UserID, &x.PlanID, &x.OrderID, &x.Name, &x.StartsAt, &x.ExpiresAt, &x.Status, &x.TotalCreditUSD, &x.DailyLimitUSD, &x.WeeklyLimitUSD, &x.MonthlyLimitUSD, &x.TotalUsedUSD, &x.DailyUsedUSD, &x.WeeklyUsedUSD, &x.MonthlyUsedUSD, &x.DailyWindowStart, &x.WeeklyWindowStart, &x.MonthlyWindowStart, &x.Notes, &x.Version, &x.CreatedAt)
	return x, err
}
func universalLock(ctx context.Context, q universalSQL, userID int64) error {
	rows, err := q.QueryContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("universal-subscription:%d", userID))
	if err != nil {
		return err
	}
	return rows.Close()
}
func normalizeUniversalWindows(x *UniversalSubscription, now time.Time) {
	day := timezone.StartOfDay(now)
	if day.After(x.DailyWindowStart) {
		x.DailyWindowStart = day
		x.DailyUsedUSD = 0
	}
	for _, w := range []struct {
		start  *time.Time
		used   *float64
		period time.Duration
	}{{&x.WeeklyWindowStart, &x.WeeklyUsedUSD, 7 * 24 * time.Hour}, {&x.MonthlyWindowStart, &x.MonthlyUsedUSD, 30 * 24 * time.Hour}} {
		if !now.Before(w.start.Add(w.period)) {
			steps := now.Sub(*w.start) / w.period
			*w.start = w.start.Add(steps * w.period)
			*w.used = 0
		}
	}
	x.DailyResetAt = x.DailyWindowStart.AddDate(0, 0, 1)
	x.WeeklyResetAt = x.WeeklyWindowStart.Add(7 * 24 * time.Hour)
	x.MonthlyResetAt = x.MonthlyWindowStart.Add(30 * 24 * time.Hour)
	if x.DailyResetAt.After(x.ExpiresAt) {
		x.DailyResetAt = x.ExpiresAt
	}
	if x.WeeklyResetAt.After(x.ExpiresAt) {
		x.WeeklyResetAt = x.ExpiresAt
	}
	if x.MonthlyResetAt.After(x.ExpiresAt) {
		x.MonthlyResetAt = x.ExpiresAt
	}
	if x.TotalCreditUSD != nil {
		x.RemainingUSD = math.Max(0, *x.TotalCreditUSD-x.TotalUsedUSD-x.HeldUSD)
	}
}
func universalCurrent(ctx context.Context, q universalSQL, userID int64, now time.Time, lock bool) (*UniversalSubscription, error) {
	suffix := ""
	if lock {
		suffix = " FOR UPDATE"
	}
	rows, err := q.QueryContext(ctx, `SELECT `+universalColumns+` FROM universal_subscriptions s WHERE s.user_id=$1 AND s.status='active' AND s.starts_at<=$2 AND s.expires_at>$2 ORDER BY s.starts_at,s.id LIMIT 1`+suffix, userID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err = rows.Err(); err != nil {
			return nil, err
		}
		return nil, ErrUniversalNotActive
	}
	x, err := scanUniversal(rows)
	if err != nil {
		return nil, err
	}
	rows.Close()
	normalizeUniversalWindows(x, now)
	if lock {
		_, err = q.ExecContext(ctx, `UPDATE universal_subscriptions SET daily_window_start=$2,weekly_window_start=$3,monthly_window_start=$4,daily_used_usd=$5,weekly_used_usd=$6,monthly_used_usd=$7 WHERE id=$1`, x.ID, x.DailyWindowStart, x.WeeklyWindowStart, x.MonthlyWindowStart, x.DailyUsedUSD, x.WeeklyUsedUSD, x.MonthlyUsedUSD)
	}
	return x, err
}
func universalPending(ctx context.Context, q universalSQL, x *UniversalSubscription) (total, day, week, month float64, err error) {
	rows, e := q.QueryContext(ctx, `SELECT COALESCE(sum(amount),0),COALESCE(sum(amount) FILTER(WHERE daily_window_start=$2),0),COALESCE(sum(amount) FILTER(WHERE weekly_window_start=$3),0),COALESCE(sum(amount) FILTER(WHERE monthly_window_start=$4),0) FROM universal_subscription_holds WHERE subscription_id=$1 AND state IN ('reserved','unknown')`, x.ID, x.DailyWindowStart, x.WeeklyWindowStart, x.MonthlyWindowStart)
	if e != nil {
		err = e
		return
	}
	defer rows.Close()
	if rows.Next() {
		err = rows.Scan(&total, &day, &week, &month)
	} else {
		err = rows.Err()
	}
	return
}
func checkUniversalLimits(x *UniversalSubscription, amount, total, day, week, month float64) error {
	if x.TotalCreditUSD != nil && (x.TotalUsedUSD+total >= *x.TotalCreditUSD || x.TotalUsedUSD+total+amount > *x.TotalCreditUSD+1e-9) {
		return ErrUniversalExhausted
	}
	for _, v := range []struct {
		limit         *float64
		used, pending float64
		code, message string
	}{{x.DailyLimitUSD, x.DailyUsedUSD, day, "UNIVERSAL_DAILY_LIMIT", "订阅日额度不足"}, {x.WeeklyLimitUSD, x.WeeklyUsedUSD, week, "UNIVERSAL_WEEKLY_LIMIT", "订阅周额度不足"}, {x.MonthlyLimitUSD, x.MonthlyUsedUSD, month, "UNIVERSAL_MONTHLY_LIMIT", "订阅月额度不足"}} {
		if v.limit != nil && (v.used+v.pending >= *v.limit || v.used+v.pending+amount > *v.limit+1e-9) {
			return infraerrors.TooManyRequests(v.code, v.message+"；不会扣除普通余额")
		}
	}
	return nil
}

type UniversalSubscriptionService struct {
	client *dbent.Client
	now    func() time.Time
}

func NewUniversalSubscriptionService(client *dbent.Client) *UniversalSubscriptionService {
	return &UniversalSubscriptionService{client: client, now: time.Now}
}
func (s *UniversalSubscriptionService) transact(ctx context.Context, userID int64, fn func(universalSQL) error) error {
	if s == nil || s.client == nil {
		return ErrUniversalUnavailable
	}
	if current := dbent.TxFromContext(ctx); current != nil {
		q := current.Client()
		if err := universalLock(ctx, q, userID); err != nil {
			return err
		}
		return fn(q)
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := tx.Client()
	if err = universalLock(ctx, q, userID); err != nil {
		return err
	}
	if err = fn(q); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *UniversalSubscriptionService) Check(ctx context.Context, userID int64) (out *UniversalSubscription, err error) {
	err = s.transact(ctx, userID, func(q universalSQL) error {
		x, e := universalCurrent(ctx, q, userID, s.now(), true)
		if e != nil {
			return e
		}
		total, day, week, month, e := universalPending(ctx, q, x)
		if e != nil {
			return e
		}
		x.HeldUSD = total
		normalizeUniversalWindows(x, s.now())
		if e = checkUniversalLimits(x, 0, total, day, week, month); e != nil {
			return e
		}
		out = x
		return nil
	})
	err = safeUniversalError(err)
	return
}
func (s *UniversalSubscriptionService) Grant(ctx context.Context, userID int64, p UniversalPlanSnapshot, orderID, actorID *int64, key, notes string) (out *UniversalSubscription, err error) {
	if userID <= 0 || len(key) < 8 || len(key) > 160 || len(notes) > 4000 {
		return nil, infraerrors.BadRequest("INVALID_SUBSCRIPTION_GRANT", "请检查用户、操作编号和备注")
	}
	if err = p.Validate(); err != nil {
		return nil, err
	}
	data, _ := json.Marshal(p)
	err = s.transact(ctx, userID, func(q universalSQL) error {
		rows, e := q.QueryContext(ctx, `SELECT `+universalColumns+` FROM universal_subscriptions s WHERE idempotency_key=$1`, key)
		if e != nil {
			return e
		}
		if rows.Next() {
			x, e := scanUniversal(rows)
			rows.Close()
			if e != nil {
				return e
			}
			if x.UserID != userID || x.Name != p.Name || x.Notes != notes {
				return infraerrors.Conflict("SUBSCRIPTION_GRANT_CONFLICT", "操作编号已用于其他订阅")
			}
			check, e := q.QueryContext(ctx, `SELECT snapshot FROM universal_subscriptions WHERE id=$1`, x.ID)
			if e != nil {
				return e
			}
			var old []byte
			if check.Next() {
				e = check.Scan(&old)
			}
			check.Close()
			if e != nil {
				return e
			}
			var saved UniversalPlanSnapshot
			if json.Unmarshal(old, &saved) != nil || !reflect.DeepEqual(saved, p) {
				return infraerrors.Conflict("SUBSCRIPTION_GRANT_CONFLICT", "操作编号已用于不同套餐规则")
			}
			out = x
			return nil
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		rows, e = q.QueryContext(ctx, `SELECT id FROM users WHERE id=$1 AND status='active' AND deleted_at IS NULL`, userID)
		if e != nil {
			return e
		}
		exists := rows.Next()
		rows.Close()
		if !exists {
			return infraerrors.NotFound("USER_NOT_FOUND", "用户不存在或已停用")
		}
		start := s.now()
		rows, e = q.QueryContext(ctx, `SELECT max(expires_at) FROM universal_subscriptions WHERE user_id=$1 AND status IN ('active','suspended') AND expires_at>$2`, userID, start)
		if e != nil {
			return e
		}
		var end *time.Time
		if rows.Next() {
			e = rows.Scan(&end)
		}
		rows.Close()
		if e != nil {
			return e
		}
		if end != nil && end.After(start) {
			start = *end
		}
		expires := start.Add(time.Duration(p.ValidityDays) * 24 * time.Hour)
		rows, e = q.QueryContext(ctx, `INSERT INTO universal_subscriptions(user_id,plan_id,order_id,idempotency_key,name,snapshot,starts_at,expires_at,total_credit_usd,daily_limit_usd,weekly_limit_usd,monthly_limit_usd,daily_window_start,weekly_window_start,monthly_window_start,assigned_by,notes) VALUES($1,NULLIF($2,0),$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$7,$7,$14,$15) RETURNING id`, userID, p.PlanID, orderID, key, p.Name, string(data), start, expires, p.TotalCreditUSD, p.DailyLimitUSD, p.WeeklyLimitUSD, p.MonthlyLimitUSD, timezone.StartOfDay(start), actorID, notes)
		if e != nil {
			return e
		}
		var id int64
		if rows.Next() {
			e = rows.Scan(&id)
		} else {
			e = rows.Err()
		}
		rows.Close()
		if e != nil {
			return e
		}
		_, e = q.ExecContext(ctx, `INSERT INTO universal_subscription_events(subscription_id,action,actor_id,reason,details)VALUES($1,'grant',$2,$3,$4)`, id, actorID, notes, string(data))
		if e != nil {
			return e
		}
		rows, e = q.QueryContext(ctx, `SELECT `+universalColumns+` FROM universal_subscriptions s WHERE s.id=$1`, id)
		if e != nil {
			return e
		}
		defer rows.Close()
		if !rows.Next() {
			return sql.ErrNoRows
		}
		out, e = scanUniversal(rows)
		return e
	})
	return
}
func (s *UniversalSubscriptionService) List(ctx context.Context, userID int64, offset, limit int) ([]UniversalSubscription, int, error) {
	if s == nil || s.client == nil {
		return nil, 0, ErrUniversalUnavailable
	}
	if limit < 1 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.client.QueryContext(ctx, `SELECT count(*) FROM universal_subscriptions WHERE ($1::bigint=0 OR user_id=$1)`, userID)
	if err != nil {
		return nil, 0, err
	}
	var total int
	if rows.Next() {
		err = rows.Scan(&total)
	}
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	rows, err = s.client.QueryContext(ctx, `SELECT `+universalColumns+` FROM universal_subscriptions s WHERE ($1::bigint=0 OR s.user_id=$1) ORDER BY s.created_at DESC,s.id DESC LIMIT $2 OFFSET $3`, userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	out := []UniversalSubscription{}
	for rows.Next() {
		x, e := scanUniversal(rows)
		if e != nil {
			rows.Close()
			return nil, 0, e
		}
		out = append(out, *x)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	now := s.now()
	for i := range out {
		x := &out[i]
		normalizeUniversalWindows(x, now)
		if x.Status == "active" {
			if !now.Before(x.ExpiresAt) {
				x.Status = "expired"
			} else if now.Before(x.StartsAt) {
				x.Status = "scheduled"
			}
		}
		pending, _, _, _, e := universalPending(ctx, s.client, x)
		if e != nil {
			return nil, 0, e
		}
		x.HeldUSD = pending
		normalizeUniversalWindows(x, now)
		r, e := s.client.QueryContext(ctx, `SELECT email,COALESCE(username,'') FROM users WHERE id=$1`, x.UserID)
		if e != nil {
			return nil, 0, e
		}
		if r.Next() {
			e = r.Scan(&x.UserEmail, &x.UserName)
		}
		r.Close()
		if e != nil {
			return nil, 0, e
		}
	}
	return out, total, nil
}
func (s *UniversalSubscriptionService) Manage(ctx context.Context, id, version, actorID int64, status, reason string) error {
	if id <= 0 || version <= 0 || actorID <= 0 || strings.TrimSpace(reason) == "" || len(reason) > 4000 {
		return infraerrors.BadRequest("INVALID_SUBSCRIPTION_ACTION", "请填写操作原因")
	}
	if status != "suspended" && status != "active" && status != "revoked" {
		return infraerrors.BadRequest("INVALID_SUBSCRIPTION_ACTION", "订阅状态无效")
	}
	rows, err := s.client.QueryContext(ctx, `SELECT user_id FROM universal_subscriptions WHERE id=$1`, id)
	if err != nil {
		return err
	}
	var userID int64
	if rows.Next() {
		err = rows.Scan(&userID)
	} else {
		err = sql.ErrNoRows
	}
	rows.Close()
	if err != nil {
		return err
	}
	return s.transact(ctx, userID, func(q universalSQL) error {
		r, e := q.QueryContext(ctx, `UPDATE universal_subscriptions SET status=$3,version=version+1,updated_at=now() WHERE id=$1 AND version=$2 AND status<>'revoked' AND NOT refund_pending RETURNING id`, id, version, status)
		if e != nil {
			return e
		}
		ok := r.Next()
		r.Close()
		if !ok {
			return infraerrors.Conflict("SUBSCRIPTION_VERSION_CONFLICT", "订阅已变更，请刷新后重试")
		}
		_, e = q.ExecContext(ctx, `INSERT INTO universal_subscription_events(subscription_id,action,actor_id,reason)VALUES($1,$2,$3,$4)`, id, status, actorID, reason)
		return e
	})
}

// Reserve is always database-backed and does not honor the optional balance-cache fail-open switch.
func (s *UniversalSubscriptionService) Reserve(ctx context.Context, userID, keyID int64, amount float64) (out *InflightReservation, err error) {
	if math.IsNaN(amount) || math.IsInf(amount, 0) || amount < 0 {
		return nil, infraerrors.BadRequest("UNIVERSAL_UNPRICED_REQUEST", "此请求暂时无法预估订阅费用")
	}
	amount = QuantizeUsageBillingAmount(amount)
	hold := uuid.NewString()
	now := s.now()
	err = s.transact(ctx, userID, func(q universalSQL) error {
		x, e := universalCurrent(ctx, q, userID, now, true)
		if e != nil {
			return e
		}
		total, day, week, month, e := universalPending(ctx, q, x)
		if e != nil {
			return e
		}
		if e = checkUniversalLimits(x, amount, total, day, week, month); e != nil {
			return e
		}
		_, e = q.ExecContext(ctx, `INSERT INTO universal_subscription_holds(id,subscription_id,user_id,api_key_id,amount,daily_window_start,weekly_window_start,monthly_window_start,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, hold, x.ID, userID, keyID, amount, x.DailyWindowStart, x.WeeklyWindowStart, x.MonthlyWindowStart, now.Add(defaultInflightReservationTTL))
		if e != nil {
			return e
		}
		out = &InflightReservation{userID: userID, requestID: hold, amount: amount, ttl: defaultInflightReservationTTL, universalHoldID: hold, universalSubscriptionID: x.ID, universalService: s}
		out.refs.Store(1)
		out.universalUncertain.Store(true)
		return nil
	})
	if err == nil {
		out.startRenewal(universalHoldRenewer{s, hold})
	}
	err = safeUniversalError(err)
	return
}

type universalHoldRenewer struct {
	s  *UniversalSubscriptionService
	id string
}

func (r universalHoldRenewer) RenewInflightBalance(ctx context.Context, _ int64, _ string, ttl time.Duration) (bool, error) {
	v, e := r.s.client.ExecContext(ctx, `UPDATE universal_subscription_holds SET expires_at=$2,updated_at=now() WHERE id=$1 AND state='reserved'`, r.id, r.s.now().Add(ttl))
	if e != nil {
		return false, e
	}
	n, e := v.RowsAffected()
	return n > 0, e
}
func (s *UniversalSubscriptionService) release(ctx context.Context, id string, uncertain bool) error {
	state := "released"
	if uncertain {
		state = "unknown"
	}
	_, err := s.client.ExecContext(ctx, `UPDATE universal_subscription_holds SET state=CASE WHEN $2='released' AND EXISTS(SELECT 1 FROM universal_subscription_charges c WHERE c.hold_id=universal_subscription_holds.id) THEN 'settled' ELSE $2 END,updated_at=now() WHERE id=$1 AND state IN ('reserved','unknown')`, id, state)
	return err
}
func IsUniversalFunding(key *APIKey) bool {
	return key != nil && key.FundingSource == FundingSourceUniversal
}
func normalizeFundingSource(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = FundingSourceBalance
	}
	if value != FundingSourceBalance && value != FundingSourceUniversal {
		return "", infraerrors.BadRequest("INVALID_FUNDING_SOURCE", "请选择余额或通用订阅计费")
	}
	return value, nil
}
func (s *SubscriptionService) Universal() *UniversalSubscriptionService {
	if s == nil {
		return nil
	}
	return NewUniversalSubscriptionService(s.entClient)
}

// ApplyUniversalCharge shares the caller's usage-billing transaction and idempotency claim.
// An underestimated real upstream charge is recorded in full, never diverted to the wallet.
func ApplyUniversalCharge(ctx context.Context, q universalSQL, cmd *UsageBillingCommand) error {
	if math.IsNaN(cmd.UniversalCost) || math.IsInf(cmd.UniversalCost, 0) || cmd.UniversalCost < 0 {
		return errors.New("invalid universal charge")
	}
	if cmd.UniversalHoldID == "" {
		return errors.New("universal subscription charge has no admitted hold")
	}
	if err := universalLock(ctx, q, cmd.UserID); err != nil {
		return err
	}
	rows, err := q.QueryContext(ctx, `SELECT subscription_id,user_id,api_key_id,daily_window_start,weekly_window_start,monthly_window_start,state FROM universal_subscription_holds WHERE id=$1 FOR UPDATE`, cmd.UniversalHoldID)
	if err != nil {
		return err
	}
	var subID, userID, keyID int64
	var day, week, month time.Time
	var state string
	if !rows.Next() {
		rows.Close()
		return errors.New("universal hold not found")
	}
	err = rows.Scan(&subID, &userID, &keyID, &day, &week, &month, &state)
	rows.Close()
	if err != nil {
		return err
	}
	if userID != cmd.UserID || keyID != cmd.APIKeyID || state == "released" {
		return errors.New("universal hold does not match admitted request")
	}
	result, err := q.ExecContext(ctx, `INSERT INTO universal_subscription_charges(subscription_id,request_id,api_key_id,hold_id,amount) VALUES($1,$2,$3,$4,$5) ON CONFLICT(request_id,api_key_id) DO NOTHING`, subID, cmd.RequestID, cmd.APIKeyID, cmd.UniversalHoldID, cmd.UniversalCost)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil || n == 0 {
		return err
	}
	_, err = q.ExecContext(ctx, `UPDATE universal_subscriptions SET total_used_usd=total_used_usd+$2,daily_used_usd=daily_used_usd+CASE WHEN daily_window_start=$3 THEN $2 ELSE 0 END,weekly_used_usd=weekly_used_usd+CASE WHEN weekly_window_start=$4 THEN $2 ELSE 0 END,monthly_used_usd=monthly_used_usd+CASE WHEN monthly_window_start=$5 THEN $2 ELSE 0 END,version=version+1,updated_at=now() WHERE id=$1`, subID, cmd.UniversalCost, day, week, month)
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, `UPDATE universal_subscription_holds SET amount=GREATEST(0,amount-$2),updated_at=now() WHERE id=$1`, cmd.UniversalHoldID, cmd.UniversalCost)
	return err
}

func (s *BillingCacheService) ReserveUniversal(ctx context.Context, key *APIKey, amount float64) (*InflightReservation, error) {
	if s.universal == nil || key == nil {
		return nil, ErrUniversalUnavailable
	}
	return s.universal.Reserve(ctx, key.UserID, key.ID, amount)
}

func (s *UniversalSubscriptionService) Plan(ctx context.Context, id int64) (*dbent.SubscriptionPlan, error) {
	p, err := s.client.SubscriptionPlan.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if p.PlanType != UniversalPlanType {
		return nil, ErrLegacySubscriptionRetired
	}
	return p, UniversalSnapshot(p).Validate()
}
func (s *UniversalSubscriptionService) Records(ctx context.Context, id int64) (map[string]any, error) {
	events := []map[string]any{}
	rows, err := s.client.QueryContext(ctx, `SELECT id,action,actor_id,reason,created_at FROM universal_subscription_events WHERE subscription_id=$1 ORDER BY id DESC LIMIT 200`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var eid int64
		var action, reason string
		var actor *int64
		var at time.Time
		if err = rows.Scan(&eid, &action, &actor, &reason, &at); err != nil {
			rows.Close()
			return nil, err
		}
		events = append(events, map[string]any{"id": eid, "action": action, "actor_id": actor, "reason": reason, "created_at": at})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	holds := []map[string]any{}
	rows, err = s.client.QueryContext(ctx, `SELECT id,api_key_id,amount,state,expires_at,created_at FROM universal_subscription_holds WHERE subscription_id=$1 AND state IN ('reserved','unknown') ORDER BY created_at DESC LIMIT 200`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var hid, state string
		var key int64
		var amount float64
		var expires, at time.Time
		if err = rows.Scan(&hid, &key, &amount, &state, &expires, &at); err != nil {
			rows.Close()
			return nil, err
		}
		if state == "reserved" && expires.Before(s.now()) {
			state = "unknown"
		}
		holds = append(holds, map[string]any{"id": hid, "api_key_id": key, "amount": amount, "state": state, "expires_at": expires, "created_at": at})
	}
	err = rows.Err()
	rows.Close()
	return map[string]any{"events": events, "holds": holds}, err
}
func (s *UniversalSubscriptionService) ResolveHold(ctx context.Context, hold string, actorID int64, reason string) error {
	if _, err := uuid.Parse(hold); err != nil || strings.TrimSpace(reason) == "" || len(reason) > 4000 {
		return infraerrors.BadRequest("INVALID_HOLD_ACTION", "请填写核对原因")
	}
	rows, err := s.client.QueryContext(ctx, `SELECT user_id FROM universal_subscription_holds WHERE id=$1`, hold)
	if err != nil {
		return err
	}
	var userID int64
	if rows.Next() {
		err = rows.Scan(&userID)
	} else {
		err = sql.ErrNoRows
	}
	rows.Close()
	if err != nil {
		return err
	}
	return s.transact(ctx, userID, func(q universalSQL) error {
		rows, err := q.QueryContext(ctx, `UPDATE universal_subscription_holds SET state='released',updated_at=now() WHERE id=$1 AND (state='unknown' OR(state='reserved' AND expires_at<now())) RETURNING subscription_id`, hold)
		if err != nil {
			return err
		}
		var sid int64
		if rows.Next() {
			err = rows.Scan(&sid)
		} else {
			err = infraerrors.Conflict("HOLD_NOT_RELEASABLE", "此预留仍在执行或已经结算")
		}
		rows.Close()
		if err != nil {
			return err
		}
		_, err = q.ExecContext(ctx, `INSERT INTO universal_subscription_events(subscription_id,action,actor_id,reason,details)VALUES($1,'resolve_hold',$2,$3,jsonb_build_object('hold_id',$4::text))`, sid, actorID, reason, hold)
		return err
	})
}

func (s *UniversalSubscriptionService) RefundState(ctx context.Context, orderID int64) (float64, error) {
	rows, err := s.client.QueryContext(ctx, `SELECT total_used_usd FROM universal_subscriptions WHERE order_id=$1`, orderID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	if !rows.Next() {
		return 0, infraerrors.NotFound("SUBSCRIPTION_NOT_FOUND", "未找到此订单的订阅权益")
	}
	var used float64
	err = rows.Scan(&used)
	return used, err
}
func (s *UniversalSubscriptionService) refundTransaction(ctx context.Context, orderID int64, fn func(universalSQL, int64) error) error {
	rows, err := s.client.QueryContext(ctx, `SELECT user_id FROM universal_subscriptions WHERE order_id=$1`, orderID)
	if err != nil {
		return err
	}
	var uid int64
	if rows.Next() {
		err = rows.Scan(&uid)
	} else {
		err = sql.ErrNoRows
	}
	rows.Close()
	if err != nil {
		return err
	}
	return s.transact(ctx, uid, func(q universalSQL) error { return fn(q, uid) })
}
func (s *UniversalSubscriptionService) HoldRefund(ctx context.Context, orderID int64, force ...bool) error {
	return s.refundTransaction(ctx, orderID, func(q universalSQL, _ int64) error {
		// Recheck usage under the same lock as admission, closing the review-to-refund race.
		check, e := q.QueryContext(ctx, `SELECT total_used_usd FROM universal_subscriptions WHERE order_id=$1 FOR UPDATE`, orderID)
		if e != nil {
			return e
		}
		var used float64
		if check.Next() {
			e = check.Scan(&used)
		} else {
			e = sql.ErrNoRows
		}
		check.Close()
		if e != nil {
			return e
		}
		if used > 0 && (len(force) == 0 || !force[0]) {
			return infraerrors.Conflict("SUBSCRIPTION_REFUND_REVIEW_REQUIRED", "订阅已有新消费，请刷新用量并重新确认退款")
		}

		rows, err := q.QueryContext(ctx, `SELECT count(*) FROM universal_subscription_holds h JOIN universal_subscriptions s ON s.id=h.subscription_id WHERE s.order_id=$1 AND h.state IN ('reserved','unknown')`, orderID)
		if err != nil {
			return err
		}
		var pending int
		if rows.Next() {
			err = rows.Scan(&pending)
		}
		rows.Close()
		if err != nil {
			return err
		}
		if pending > 0 {
			return infraerrors.Conflict("SUBSCRIPTION_HAS_PENDING_REQUESTS", "此订阅有执行中或待核对请求，请先完成结算")
		}
		_, err = q.ExecContext(ctx, `UPDATE universal_subscriptions SET refund_previous_status=CASE WHEN refund_pending THEN refund_previous_status ELSE status END,status='suspended',refund_pending=true,version=version+1,updated_at=now() WHERE order_id=$1`, orderID)
		return err
	})
}
func (s *UniversalSubscriptionService) FinishRefund(ctx context.Context, orderID int64, restore bool) error {
	return s.refundTransaction(ctx, orderID, func(q universalSQL, _ int64) error {
		action := "refund"
		if restore {
			action = "refund_failed_restored"
		}
		rows, err := q.QueryContext(ctx, `UPDATE universal_subscriptions SET status=CASE WHEN $2 THEN COALESCE(refund_previous_status,'active') ELSE 'revoked' END,refund_pending=false,version=version+1,updated_at=now() WHERE order_id=$1 AND refund_pending RETURNING id`, orderID, restore)
		if err != nil {
			return err
		}
		var id int64
		if rows.Next() {
			err = rows.Scan(&id)
		}
		rows.Close()
		if err != nil || id == 0 {
			return err
		}
		_, err = q.ExecContext(ctx, `INSERT INTO universal_subscription_events(subscription_id,action,reason)VALUES($1,$2,$3)`, id, action, fmt.Sprintf("订单 %d", orderID))
		return err
	})
}

func (r *InflightReservation) MarkDefinitiveNoCharge() {
	if r != nil && r.universalHoldID != "" {
		r.universalUncertain.Store(false)
	}
}

// ApplyUniversalBatchHold persists a job's funding source, so editing/deleting a Key cannot redirect settlement.
func ApplyUniversalBatchHold(ctx context.Context, q universalSQL, cmd *BatchImageBalanceHoldCommand) (bool, *BatchImageBalanceHoldResult, error) {
	if err := universalLock(ctx, q, cmd.UserID); err != nil {
		return false, nil, err
	}
	reserve := strings.HasPrefix(cmd.RequestID, batchImageHoldRequestPrefix)
	rows, err := q.QueryContext(ctx, `SELECT source,hold_id,user_id,api_key_id FROM universal_batch_funding WHERE batch_id=$1 FOR UPDATE`, cmd.BatchID)
	if err != nil {
		return false, nil, err
	}
	var source string
	var hold *string
	var uid, kid int64
	exists := rows.Next()
	if exists {
		err = rows.Scan(&source, &hold, &uid, &kid)
	}
	rows.Close()
	if err != nil {
		return false, nil, err
	}
	if exists && (uid != cmd.UserID || kid != cmd.APIKeyID) {
		return false, nil, errors.New("batch funding owner mismatch")
	}
	if !exists && !reserve {
		return false, nil, nil
	}
	if !exists {
		rows, err = q.QueryContext(ctx, `SELECT funding_source FROM api_keys WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL`, cmd.APIKeyID, cmd.UserID)
		if err != nil {
			return false, nil, err
		}
		if !rows.Next() {
			rows.Close()
			return false, nil, ErrAPIKeyNotFound
		}
		err = rows.Scan(&source)
		rows.Close()
		if err != nil {
			return false, nil, err
		}
		if source == FundingSourceUniversal {
			now := time.Now()
			x, err := universalCurrent(ctx, q, cmd.UserID, now, true)
			if err != nil {
				return true, nil, err
			}
			total, day, week, month, err := universalPending(ctx, q, x)
			if err != nil {
				return true, nil, err
			}
			if err = checkUniversalLimits(x, cmd.HoldAmount, total, day, week, month); err != nil {
				return true, nil, err
			}
			hid := uuid.NewString()
			hold = &hid
			_, err = q.ExecContext(ctx, `INSERT INTO universal_subscription_holds(id,subscription_id,user_id,api_key_id,amount,daily_window_start,weekly_window_start,monthly_window_start,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, hid, x.ID, cmd.UserID, cmd.APIKeyID, cmd.HoldAmount, x.DailyWindowStart, x.WeeklyWindowStart, x.MonthlyWindowStart, now.Add(7*24*time.Hour))
			if err != nil {
				return true, nil, err
			}
		}
		_, err = q.ExecContext(ctx, `INSERT INTO universal_batch_funding(batch_id,user_id,api_key_id,source,hold_id)VALUES($1,$2,$3,$4,$5)`, cmd.BatchID, cmd.UserID, cmd.APIKeyID, source, hold)
		if err != nil {
			return false, nil, err
		}
	}
	if source != FundingSourceUniversal {
		return false, nil, nil
	}
	if hold == nil {
		return true, nil, errors.New("batch universal funding has no hold")
	}
	if strings.HasPrefix(cmd.RequestID, batchImageCaptureRequestPrefix) {
		if cmd.ActualAmount-cmd.HoldAmount > 1e-8 {
			return true, nil, ErrBatchImageSettlementCostExceedsHold
		}
		err = ApplyUniversalCharge(ctx, q, &UsageBillingCommand{RequestID: cmd.RequestID, APIKeyID: cmd.APIKeyID, UserID: cmd.UserID, UniversalHoldID: *hold, UniversalCost: cmd.ActualAmount})
		if err == nil {
			_, err = q.ExecContext(ctx, `UPDATE universal_subscription_holds SET state='settled',updated_at=now() WHERE id=$1`, *hold)
		}
	} else if strings.HasPrefix(cmd.RequestID, batchImageReleaseRequestPrefix) {
		_, err = q.ExecContext(ctx, `UPDATE universal_subscription_holds SET state='released',updated_at=now() WHERE id=$1 AND state IN ('reserved','unknown')`, *hold)
	}
	return true, &BatchImageBalanceHoldResult{}, err
}

type UniversalSubscriptionAdjustment struct {
	Version         int64      `json:"version"`
	TotalCreditUSD  *float64   `json:"total_credit_usd"`
	DailyLimitUSD   *float64   `json:"daily_limit_usd"`
	WeeklyLimitUSD  *float64   `json:"weekly_limit_usd"`
	MonthlyLimitUSD *float64   `json:"monthly_limit_usd"`
	ExpiresAt       *time.Time `json:"expires_at"`
	Reason          string     `json:"reason"`
}

func (s *UniversalSubscriptionService) Adjust(ctx context.Context, id, actorID int64, in UniversalSubscriptionAdjustment) error {
	if id <= 0 || actorID <= 0 || in.Version <= 0 || strings.TrimSpace(in.Reason) == "" || len(in.Reason) > 4000 {
		return infraerrors.BadRequest("INVALID_SUBSCRIPTION_ADJUSTMENT", "请填写调整原因并刷新订阅")
	}
	rows, err := s.client.QueryContext(ctx, `SELECT user_id FROM universal_subscriptions WHERE id=$1`, id)
	if err != nil {
		return err
	}
	var uid int64
	if rows.Next() {
		err = rows.Scan(&uid)
	} else {
		err = sql.ErrNoRows
	}
	rows.Close()
	if err != nil {
		return err
	}
	return s.transact(ctx, uid, func(q universalSQL) error {
		rows, err := q.QueryContext(ctx, `SELECT `+universalColumns+` FROM universal_subscriptions s WHERE s.id=$1 AND NOT s.refund_pending FOR UPDATE`, id)
		if err != nil {
			return err
		}
		if !rows.Next() {
			rows.Close()
			return infraerrors.Conflict("SUBSCRIPTION_NOT_EDITABLE", "订阅正在退款或不存在")
		}
		x, err := scanUniversal(rows)
		rows.Close()
		if err != nil {
			return err
		}
		if x.Version != in.Version {
			return infraerrors.Conflict("SUBSCRIPTION_VERSION_CONFLICT", "订阅已变更，请刷新")
		}
		if x.Status == "revoked" || !x.ExpiresAt.After(s.now()) {
			return infraerrors.Conflict("SUBSCRIPTION_NOT_EDITABLE", "已结束订阅不能再调整")
		}
		normalizeUniversalWindows(x, s.now())
		total, day, week, month, err := universalPending(ctx, q, x)
		if err != nil {
			return err
		}
		if in.TotalCreditUSD != nil {
			x.TotalCreditUSD = in.TotalCreditUSD
		}
		if in.DailyLimitUSD != nil {
			x.DailyLimitUSD = optionalPlanLimit(in.DailyLimitUSD)
		}
		if in.WeeklyLimitUSD != nil {
			x.WeeklyLimitUSD = optionalPlanLimit(in.WeeklyLimitUSD)
		}
		if in.MonthlyLimitUSD != nil {
			x.MonthlyLimitUSD = optionalPlanLimit(in.MonthlyLimitUSD)
		}
		snapshot := UniversalPlanSnapshot{Name: x.Name, ValidityDays: 1, TotalCreditUSD: x.TotalCreditUSD, DailyLimitUSD: x.DailyLimitUSD, WeeklyLimitUSD: x.WeeklyLimitUSD, MonthlyLimitUSD: x.MonthlyLimitUSD}
		if err = snapshot.Validate(); err != nil {
			return err
		}
		for _, v := range []struct {
			limit *float64
			spent float64
		}{{x.TotalCreditUSD, x.TotalUsedUSD + total}, {x.DailyLimitUSD, x.DailyUsedUSD + day}, {x.WeeklyLimitUSD, x.WeeklyUsedUSD + week}, {x.MonthlyLimitUSD, x.MonthlyUsedUSD + month}} {
			if v.limit != nil && *v.limit+1e-8 < v.spent {
				return infraerrors.BadRequest("SUBSCRIPTION_LIMIT_BELOW_USAGE", "新额度不能低于已消费与在途预留之和")
			}
		}
		if in.ExpiresAt != nil {
			x.ExpiresAt = *in.ExpiresAt
		}
		if !x.ExpiresAt.After(x.StartsAt) || !x.ExpiresAt.After(s.now()) {
			return infraerrors.BadRequest("SUBSCRIPTION_EXPIRY_INVALID", "结束时间必须晚于生效时间与当前时间")
		}
		rows, err = q.QueryContext(ctx, `SELECT count(*) FROM universal_subscriptions WHERE user_id=$1 AND id<>$2 AND status IN ('active','suspended') AND starts_at<$3 AND expires_at>$4`, uid, id, x.ExpiresAt, x.StartsAt)
		if err != nil {
			return err
		}
		var overlaps int
		if rows.Next() {
			err = rows.Scan(&overlaps)
		}
		rows.Close()
		if err != nil {
			return err
		}
		if overlaps > 0 {
			return infraerrors.Conflict("SUBSCRIPTION_TERM_OVERLAP", "调整后会与已排期订阅重叠，请先处理后续排期")
		}
		_, err = q.ExecContext(ctx, `UPDATE universal_subscriptions SET total_credit_usd=$2,daily_limit_usd=$3,weekly_limit_usd=$4,monthly_limit_usd=$5,expires_at=$6,version=version+1,updated_at=now() WHERE id=$1`, id, x.TotalCreditUSD, x.DailyLimitUSD, x.WeeklyLimitUSD, x.MonthlyLimitUSD, x.ExpiresAt)
		if err != nil {
			return err
		}
		details, _ := json.Marshal(in)
		_, err = q.ExecContext(ctx, `INSERT INTO universal_subscription_events(subscription_id,action,actor_id,reason,details)VALUES($1,'adjust',$2,$3,$4)`, id, actorID, in.Reason, string(details))
		return err
	})
}

func (s *BillingCacheService) StoreMediaFunding(ctx context.Context, key *APIKey, id string) error {
	if s == nil || s.universal == nil || key == nil {
		return ErrUniversalUnavailable
	}
	id = strings.TrimSpace(id)
	if id == "" || len(id) > 256 {
		return errors.New("invalid media task id")
	}
	var hold *string
	if IsUniversalFunding(key) {
		h := InflightReservationFromContext(ctx)
		if h == nil || h.universalHoldID == "" {
			return ErrUniversalUnavailable
		}
		hold = &h.universalHoldID
	}
	source, _ := normalizeFundingSource(key.FundingSource)
	_, err := s.universal.client.ExecContext(ctx, `INSERT INTO universal_media_funding(task_id,user_id,api_key_id,source,hold_id)VALUES($1,$2,$3,$4,$5) ON CONFLICT(task_id,user_id,api_key_id) DO NOTHING`, id, key.UserID, key.ID, source, hold)
	return err
}
func (s *BillingCacheService) ResumeMediaFunding(ctx context.Context, key *APIKey, id string) (context.Context, *APIKey, func(), bool, error) {
	if s == nil || s.universal == nil || key == nil {
		return ctx, key, noopRelease, false, ErrUniversalUnavailable
	}
	rows, err := s.universal.client.QueryContext(ctx, `SELECT f.source,f.hold_id,h.subscription_id,h.amount FROM universal_media_funding f LEFT JOIN universal_subscription_holds h ON h.id=f.hold_id WHERE f.task_id=$1 AND f.user_id=$2 AND f.api_key_id=$3`, strings.TrimSpace(id), key.UserID, key.ID)
	if err != nil {
		return ctx, key, noopRelease, false, err
	}
	if !rows.Next() {
		rows.Close()
		return ctx, key, noopRelease, false, nil
	}
	var source string
	var hold *string
	var sid *int64
	var amount *float64
	err = rows.Scan(&source, &hold, &sid, &amount)
	rows.Close()
	if err != nil {
		return ctx, key, noopRelease, false, err
	}
	copy := *key
	copy.FundingSource = source
	if source != FundingSourceUniversal {
		return ctx, &copy, noopRelease, false, nil
	}
	if hold == nil || sid == nil || amount == nil {
		return ctx, key, noopRelease, false, ErrUniversalUnavailable
	}
	handle := &InflightReservation{userID: key.UserID, requestID: *hold, amount: *amount, ttl: defaultInflightReservationTTL, universalHoldID: *hold, universalSubscriptionID: *sid, universalService: s.universal}
	handle.refs.Store(1)
	handle.universalUncertain.Store(true)
	return WithInflightReservation(ctx, handle), &copy, handle.HandlerDone, true, nil
}

// Client messages never expose raw database/transport errors or their attached causes.
func UniversalBillingErrorDetails(err error) (int, string, string) {
	var app *infraerrors.ApplicationError
	if errors.As(err, &app) && strings.HasPrefix(app.Reason, "UNIVERSAL_") {
		return int(app.Code), app.Reason, app.Message
	}
	return 503, "UNIVERSAL_SUBSCRIPTION_UNAVAILABLE", "订阅计费暂时不可用，请稍后重试"
}
func IsUniversalBillingError(err error) bool {
	var app *infraerrors.ApplicationError
	return errors.As(err, &app) && strings.HasPrefix(app.Reason, "UNIVERSAL_")
}
func safeUniversalError(err error) error {
	if err == nil {
		return nil
	}
	var app *infraerrors.ApplicationError
	if errors.As(err, &app) {
		return err
	}
	return ErrUniversalUnavailable.WithCause(err)
}

// DescribeUniversalFunding is the read-only /v1/usage view; expired subscriptions do not expose wallet credit as spendable.
func (s *BillingCacheService) DescribeUniversalFunding(ctx context.Context, key *APIKey) (map[string]any, error) {
	if s == nil || s.universal == nil || key == nil {
		return nil, ErrUniversalUnavailable
	}
	list, _, err := s.universal.List(ctx, key.UserID, 0, 200)
	if err != nil {
		return nil, ErrUniversalUnavailable
	}
	response := map[string]any{"mode": "unrestricted", "funding_source": FundingSourceUniversal, "isValid": false, "planName": "通用订阅", "unit": "USD", "remaining": 0.0, "balance_fallback": false}
	if key.Quota > 0 || key.HasRateLimits() {
		response["mode"] = "quota_limited"
	}
	if key.Quota > 0 {
		response["quota"] = map[string]any{"limit": key.Quota, "used": key.QuotaUsed, "remaining": key.GetQuotaRemaining(), "unit": "USD"}
	}
	for _, x := range list {
		if x.Status != "active" {
			continue
		}
		remaining := x.RemainingUSD
		for _, v := range []struct {
			limit *float64
			used  float64
		}{{x.DailyLimitUSD, x.DailyUsedUSD}, {x.WeeklyLimitUSD, x.WeeklyUsedUSD}, {x.MonthlyLimitUSD, x.MonthlyUsedUSD}} {
			if v.limit != nil {
				remaining = math.Min(remaining, math.Max(0, *v.limit-v.used-x.HeldUSD))
			}
		}
		if key.Quota > 0 {
			remaining = math.Min(remaining, key.GetQuotaRemaining())
		}
		response["planName"] = x.Name
		response["remaining"] = remaining
		response["isValid"] = remaining > 0
		response["expires_at"] = x.ExpiresAt
		response["subscription"] = map[string]any{"id": x.ID, "total_credit_usd": x.TotalCreditUSD, "total_used_usd": x.TotalUsedUSD, "held_usd": x.HeldUSD, "daily_usage_usd": x.DailyUsedUSD, "weekly_usage_usd": x.WeeklyUsedUSD, "monthly_usage_usd": x.MonthlyUsedUSD, "daily_limit_usd": x.DailyLimitUSD, "weekly_limit_usd": x.WeeklyLimitUSD, "monthly_limit_usd": x.MonthlyLimitUSD, "expires_at": x.ExpiresAt}
		return response, nil
	}
	return response, nil
}
