package service

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	infraerrors "github.com/MACOS-DO/sub4api/internal/pkg/errors"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var ErrReceivablesUnavailable = infraerrors.New(503, "RECEIVABLES_UNAVAILABLE", "记账服务暂不可用")

type AdminReceivableService interface {
	CreditUserOnAccount(context.Context, int64, float64, string, int64, string) (*User, error)
}
type ReceivableEntry struct {
	ID               int64     `json:"id"`
	UserID           int64     `json:"user_id"`
	Kind             string    `json:"kind"`
	Amount           string    `json:"amount"`
	OutstandingAfter string    `json:"outstanding_after"`
	ActorID          int64     `json:"actor_id"`
	ActorName        string    `json:"actor_name"`
	Notes            string    `json:"notes"`
	PaymentMethod    string    `json:"payment_method"`
	OccurredAt       time.Time `json:"occurred_at"`
	CreatedAt        time.Time `json:"created_at"`
	OperationKey     string    `json:"operation_key"`
}

func ValidateReceivableAmount(raw string) (decimal.Decimal, error) {
	if len(raw) > 64 {
		return decimal.Zero, infraerrors.BadRequest("INVALID_AMOUNT", "金额过长")
	}
	v, e := decimal.NewFromString(raw)
	if e != nil || !v.IsPositive() || v.Exponent() < -8 || v.Exponent() > 12 || v.GreaterThan(decimal.NewFromInt(999999999999)) {
		return decimal.Zero, infraerrors.BadRequest("INVALID_AMOUNT", "金额需大于零，最多八位小数")
	}
	return v, nil
}
func validateReceivableInput(amount, notes, method string, actorID int64, key string) (decimal.Decimal, error) {
	v, e := ValidateReceivableAmount(amount)
	if e != nil {
		return v, e
	}
	id, e := uuid.Parse(key)
	if e != nil || id == uuid.Nil || key != id.String() {
		return v, infraerrors.BadRequest("INVALID_OPERATION_KEY", "请刷新后重试")
	}
	if actorID <= 0 || utf8.RuneCountInString(notes) > 2000 || utf8.RuneCountInString(method) > 100 || strings.ContainsRune(notes, '\x00') || strings.ContainsAny(method, "\x00\r\n") {
		return v, infraerrors.BadRequest("INVALID_RECEIVABLE_INPUT", "请检查备注或收款方式")
	}
	return v, nil
}
func receivableOne(ctx context.Context, q BalanceCreditSQL, query string, args []any, scan func(*sql.Rows) error) error {
	rows, e := q.QueryContext(ctx, query, args...)
	if e != nil {
		return e
	}
	defer rows.Close()
	if !rows.Next() {
		if e = rows.Err(); e != nil {
			return e
		}
		return sql.ErrNoRows
	}
	if e = scan(rows); e != nil {
		return e
	}
	return rows.Err()
}
func lockReceivableUser(ctx context.Context, q BalanceCreditSQL, id int64, includeDeleted bool) error {
	filter := " AND deleted_at IS NULL"
	if includeDeleted {
		filter = ""
	}
	var found int64
	e := receivableOne(ctx, q, `SELECT id FROM users WHERE id=$1`+filter+` FOR UPDATE`, []any{id}, func(r *sql.Rows) error { return r.Scan(&found) })
	if errors.Is(e, sql.ErrNoRows) {
		return ErrUserNotFound
	}
	return e
}
func outstandingReceivable(ctx context.Context, q BalanceCreditSQL, id int64) (decimal.Decimal, error) {
	var raw string
	e := receivableOne(ctx, q, `SELECT COALESCE(SUM(CASE WHEN kind='credit' THEN amount ELSE -amount END),0)::text FROM admin_receivable_entries WHERE user_id=$1`, []any{id}, func(r *sql.Rows) error { return r.Scan(&raw) })
	if e != nil {
		return decimal.Zero, e
	}
	return decimal.NewFromString(raw)
}
func existingReceivable(ctx context.Context, q BalanceCreditSQL, userID, actorID int64, kind string, amount decimal.Decimal, notes, method, key string, occurredAt *time.Time) (*ReceivableEntry, error) {
	v := &ReceivableEntry{}
	e := receivableOne(ctx, q, `SELECT id,user_id,kind,amount::text,outstanding_after::text,actor_id,notes,payment_method,occurred_at,created_at,operation_key::text FROM admin_receivable_entries WHERE operation_key=$1`, []any{key}, func(r *sql.Rows) error {
		return r.Scan(&v.ID, &v.UserID, &v.Kind, &v.Amount, &v.OutstandingAfter, &v.ActorID, &v.Notes, &v.PaymentMethod, &v.OccurredAt, &v.CreatedAt, &v.OperationKey)
	})
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	original, _ := decimal.NewFromString(v.Amount)
	if v.UserID != userID || v.ActorID != actorID || v.Kind != kind || !original.Equal(amount) || v.Notes != notes || v.PaymentMethod != method || (occurredAt != nil && !v.OccurredAt.Equal(occurredAt.Truncate(time.Microsecond))) {
		return nil, infraerrors.Conflict("RECEIVABLE_OPERATION_CONFLICT", "该操作已提交，请刷新后重新操作")
	}
	return v, nil
}
func (s *adminServiceImpl) CreditUserOnAccount(ctx context.Context, userID int64, amount float64, notes string, actorID int64, key string) (*User, error) {
	if math.IsNaN(amount) || math.IsInf(amount, 0) {
		return nil, infraerrors.BadRequest("INVALID_AMOUNT", "金额无效")
	}
	value, e := validateReceivableInput(strconv.FormatFloat(amount, 'f', -1, 64), notes, "", actorID, key)
	if e != nil {
		return nil, e
	}
	if s.entClient == nil {
		return nil, ErrReceivablesUnavailable
	}
	tx, e := s.entClient.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	q := tx.Client()
	if _, e = q.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,91))`, key); e != nil {
		return nil, e
	}
	if e = lockReceivableUser(ctx, q, userID, false); e != nil {
		return nil, e
	}
	existing, e := existingReceivable(ctx, q, userID, actorID, "credit", value, notes, "", key, nil)
	if e != nil {
		return nil, e
	}
	if existing == nil {
		outstanding, readErr := outstandingReceivable(ctx, q, userID)
		if readErr != nil {
			return nil, readErr
		}
		code, codeErr := GenerateRedeemCode()
		if codeErr != nil {
			return nil, codeErr
		}
		record, _, creditErr := CreditAdminBalanceTx(ctx, q, userID, value.String(), code, "[记账跑付] "+notes, "on_account")
		if creditErr != nil {
			return nil, creditErr
		}
		_, e = q.ExecContext(ctx, `INSERT INTO admin_receivable_entries(user_id,kind,amount,outstanding_after,credit_record_id,actor_id,operation_key,notes) VALUES($1,'credit',$2::numeric,$3::numeric,$4,$5,$6,$7)`, userID, value.String(), outstanding.Add(value).String(), record, actorID, key, notes)
		if e != nil {
			return nil, e
		}
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	cacheCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	s.InvalidateBalanceAfterCredit(cacheCtx, userID)
	return s.userRepo.GetByID(cacheCtx, userID)
}

// RecordReceivablePaymentTx only settles debt; it never credits spendable balance.
// The caller owns the transaction and rolls it back on every error.
func RecordReceivablePaymentTx(ctx context.Context, q BalanceCreditSQL, userID int64, amount, notes, method string, actorID int64, key string, occurredAt *time.Time) (*ReceivableEntry, error) {
	value, e := validateReceivableInput(amount, notes, method, actorID, key)
	if e != nil {
		return nil, e
	}
	if occurredAt != nil && (occurredAt.Before(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)) || occurredAt.After(time.Now().Add(5*time.Minute))) {
		return nil, infraerrors.BadRequest("INVALID_PAYMENT_DATE", "收款时间无效")
	}
	if _, e = q.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,91))`, key); e != nil {
		return nil, e
	}
	if e = lockReceivableUser(ctx, q, userID, true); e != nil {
		return nil, e
	}
	existing, e := existingReceivable(ctx, q, userID, actorID, "payment", value, notes, method, key, occurredAt)
	if e != nil || existing != nil {
		return existing, e
	}
	outstanding, e := outstandingReceivable(ctx, q, userID)
	if e != nil {
		return nil, e
	}
	if value.GreaterThan(outstanding) {
		return nil, infraerrors.BadRequest("PAYMENT_EXCEEDS_DEBT", "收款金额不能超过当前欠款")
	}
	v := &ReceivableEntry{UserID: userID, Kind: "payment", Amount: value.String(), OutstandingAfter: outstanding.Sub(value).String(), ActorID: actorID, Notes: notes, PaymentMethod: method, OperationKey: key}
	e = receivableOne(ctx, q, `INSERT INTO admin_receivable_entries(user_id,kind,amount,outstanding_after,actor_id,operation_key,notes,payment_method,occurred_at) VALUES($1,'payment',$2::numeric,$3::numeric,$4,$5,$6,$7,COALESCE($8::timestamptz,NOW())) RETURNING id,occurred_at,created_at`, []any{userID, v.Amount, v.OutstandingAfter, actorID, key, notes, method, occurredAt}, func(r *sql.Rows) error { return r.Scan(&v.ID, &v.OccurredAt, &v.CreatedAt) })
	return v, e
}
