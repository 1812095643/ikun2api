package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/shopspring/decimal"
)

type blindboxSpec struct {
	Price    string `json:"price"`
	Minimum  string `json:"minimum"`
	Maximum  string `json:"maximum"`
	DrawTime string `json:"draw_time"`
}
type blindboxSettings struct {
	Enabled bool `json:"enabled"`
	blindboxSpec
}
type blindboxRound struct {
	ID           int64        `json:"id"`
	Date         string       `json:"date"`
	Spec         blindboxSpec `json:"spec"`
	Signature    string       `json:"-"`
	DrawAt       time.Time    `json:"draw_at"`
	Status       string       `json:"status"`
	Count        int          `json:"count"`
	TotalPaid    string       `json:"total_paid"`
	TotalAwarded string       `json:"total_awarded"`
	Digest       string       `json:"result_digest"`
	SettledAt    *time.Time   `json:"settled_at"`
}
type blindboxEntry struct {
	ID              int64      `json:"id"`
	RoundID         int64      `json:"round_id"`
	UserID          int64      `json:"user_id,omitempty"`
	Date            string     `json:"date"`
	Ordinal         int        `json:"ordinal"`
	Paid            string     `json:"paid"`
	Usage           string     `json:"-"`
	Signature       string     `json:"-"`
	Reward          *string    `json:"reward"`
	RewardSignature *string    `json:"-"`
	CreditID        *int64     `json:"credit_id"`
	DebitID         int64      `json:"debit_id"`
	OpenedAt        *time.Time `json:"opened_at"`
	CreatedAt       time.Time  `json:"created_at"`
	DrawAt          time.Time  `json:"draw_at"`
	Status          string     `json:"status"`
	Username        string     `json:"username,omitempty"`
}

var blindboxTimePattern = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)
var blindboxRequestPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)

func normalizeBlindboxSpec(s *blindboxSpec) error {
	price, err := blindboxCents(s.Price)
	if err != nil {
		return err
	}
	minimum, err := blindboxCents(s.Minimum)
	if err != nil {
		return err
	}
	maximum, err := blindboxCents(s.Maximum)
	if err != nil {
		return err
	}
	if minimum >= price || maximum <= price {
		return badRequest("请将售价设在奖励下限和上限之间。")
	}
	if !blindboxTimePattern.MatchString(s.DrawTime) {
		return badRequest("开奖时间请使用 24 小时制，如 21:00。")
	}
	s.Price, s.Minimum, s.Maximum = blindboxMoney(price), blindboxMoney(minimum), blindboxMoney(maximum)
	return nil
}

func blindboxDeadline(day string, s blindboxSpec) (time.Time, error) {
	return time.ParseInLocation("2006-01-02 15:04", day+" "+s.DrawTime, lotteryLocation)
}

func blindboxAllowance(spend string) (int, error) {
	v, err := decimal.NewFromString(spend)
	if err != nil || v.IsNegative() {
		return 0, fmt.Errorf("今日用量金额无法核对")
	}
	if v.IsZero() {
		return 0, nil
	}
	whole, remainder := v.QuoRem(decimal.NewFromInt(100), 0)
	limit := whole
	if remainder.IsPositive() {
		limit = limit.Add(decimal.NewFromInt(1))
	}
	if limit.GreaterThan(decimal.NewFromInt(blindboxCapacity)) {
		return blindboxCapacity, nil
	}
	return int(limit.IntPart()), nil
}

func blindboxRoundSign(key []byte, r blindboxRound) string {
	return lotterySign(key, "blindbox-round-v1", []any{r.ID, r.Date, r.Spec, r.DrawAt.Unix()})
}
func blindboxEntrySign(key []byte, e blindboxEntry) string {
	return lotterySign(key, "blindbox-entry-v1", []any{e.ID, e.RoundID, e.UserID, e.Ordinal, e.Paid, e.Usage, e.DebitID})
}
func blindboxRewardSign(key []byte, e blindboxEntry, reward string, creditID int64) string {
	return lotterySign(key, "blindbox-reward-v1", []any{e.ID, e.RoundID, e.UserID, e.Paid, reward, creditID})
}

const blindboxRoundColumns = `id,business_date::text,spec,signature,draw_at,status,entry_count,total_paid::text,total_awarded::text,result_digest,settled_at`

func scanBlindboxRound(row rowScanner) (blindboxRound, error) {
	var r blindboxRound
	var raw []byte
	err := row.Scan(&r.ID, &r.Date, &raw, &r.Signature, &r.DrawAt, &r.Status, &r.Count, &r.TotalPaid, &r.TotalAwarded, &r.Digest, &r.SettledAt)
	if err != nil {
		return r, err
	}
	if err = json.Unmarshal(raw, &r.Spec); err != nil {
		return r, err
	}
	if err = normalizeBlindboxSpec(&r.Spec); err != nil {
		return r, err
	}
	return r, nil
}

func loadBlindboxRound(ctx context.Context, db lotteryDB, id int64, day string, key []byte, lock bool) (blindboxRound, error) {
	suffix := ""
	if lock {
		suffix = " FOR UPDATE"
	}
	r, err := scanBlindboxRound(db.QueryRowContext(ctx, `SELECT `+blindboxRoundColumns+` FROM workspace_blindbox_rounds WHERE ($1::bigint=0 OR id=$1) AND ($2='' OR business_date::text=$2)`+suffix, id, day))
	if err == nil && !lotterySignatureOK(r.Signature, blindboxRoundSign(key, r)) {
		return r, fmt.Errorf("盲盒场次规则完整性校验未通过")
	}
	return r, err
}

func loadBlindboxSettings(ctx context.Context, db lotteryDB, lock bool) (blindboxSettings, error) {
	var s blindboxSettings
	suffix := ""
	if lock {
		suffix = " FOR SHARE"
	}
	err := db.QueryRowContext(ctx, `SELECT enabled,price::text,minimum::text,maximum::text,draw_time FROM workspace_blindbox_settings WHERE id=1`+suffix).Scan(&s.Enabled, &s.Price, &s.Minimum, &s.Maximum, &s.DrawTime)
	if err == nil {
		err = normalizeBlindboxSpec(&s.blindboxSpec)
	}
	return s, err
}

const blindboxEntryColumns = `e.id,e.round_id,e.user_id,r.business_date::text,e.ordinal,e.paid::text,e.usage_amount::text,e.signature,e.reward::text,e.reward_signature,e.credit_id,e.debit_id,e.opened_at,e.created_at,r.draw_at,r.status,u.username`
const blindboxEntryFrom = ` FROM workspace_blindbox_entries e JOIN workspace_blindbox_rounds r ON r.id=e.round_id JOIN users u ON u.id=e.user_id`

func scanBlindboxEntry(row rowScanner) (blindboxEntry, error) {
	var e blindboxEntry
	err := row.Scan(&e.ID, &e.RoundID, &e.UserID, &e.Date, &e.Ordinal, &e.Paid, &e.Usage, &e.Signature, &e.Reward, &e.RewardSignature, &e.CreditID, &e.DebitID, &e.OpenedAt, &e.CreatedAt, &e.DrawAt, &e.Status, &e.Username)
	if err != nil {
		return e, err
	}
	for _, field := range []*string{&e.Paid, &e.Usage} {
		v, parseErr := decimal.NewFromString(*field)
		if parseErr != nil {
			return e, parseErr
		}
		*field = v.String()
	}
	if e.Reward != nil {
		v, parseErr := decimal.NewFromString(*e.Reward)
		if parseErr != nil {
			return e, parseErr
		}
		*e.Reward = v.String()
	}
	return e, nil
}

func verifyBlindboxEntry(e blindboxEntry, key []byte) error {
	if !lotterySignatureOK(e.Signature, blindboxEntrySign(key, e)) {
		return fmt.Errorf("盲盒购买记录完整性校验未通过")
	}
	if e.Reward != nil && (e.CreditID == nil || e.RewardSignature == nil || !lotterySignatureOK(*e.RewardSignature, blindboxRewardSign(key, e, *e.Reward, *e.CreditID))) {
		return fmt.Errorf("盲盒中奖记录完整性校验未通过")
	}
	return nil
}
