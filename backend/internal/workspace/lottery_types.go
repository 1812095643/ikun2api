package workspace

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

var lotteryLocation = time.FixedZone("Asia/Shanghai", 8*60*60)
var lotteryPrizeNames = map[string]string{"special": "特等奖", "first": "一等奖", "second": "二等奖", "third": "三等奖", "consolation": "安慰奖"}
var lotteryPrizeOrder = []string{"special", "first", "second", "third", "consolation"}

type lotteryCondition struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}
type lotteryPrize struct {
	Key    string `json:"key"`
	Amount string `json:"amount"`
	Count  int    `json:"count"`
}
type lotterySpec struct {
	Title        string             `json:"title"`
	Description  string             `json:"description"`
	BusinessDate string             `json:"business_date"`
	Match        string             `json:"match"`
	Conditions   []lotteryCondition `json:"conditions"`
	Prizes       []lotteryPrize     `json:"prizes"`
	DrawMode     string             `json:"draw_mode"`
	Capacity     int                `json:"capacity"`
	StartAt      time.Time          `json:"start_at"`
	DrawAt       *time.Time         `json:"draw_at"`
}
type lotteryActivity struct {
	ID                 int64          `json:"id"`
	Spec               lotterySpec    `json:"spec"`
	Signature          string         `json:"-"`
	Status             string         `json:"status"`
	CreatedBy          int64          `json:"-"`
	CreatedAt          time.Time      `json:"created_at"`
	PublishedAt        *time.Time     `json:"published_at"`
	DrawnAt            *time.Time     `json:"drawn_at"`
	EntryCount         int            `json:"entry_count"`
	EligibleCount      int            `json:"eligible_count"`
	PrizeTotal         string         `json:"prize_total"`
	PrizeCount         int            `json:"prize_count"`
	Joined             bool           `json:"joined"`
	Eligible           bool           `json:"eligible"`
	EligibilityMessage string         `json:"eligibility_message"`
	MyWin              *lotteryWin    `json:"my_win,omitempty"`
	Winners            []lotteryWin   `json:"winners,omitempty"`
	Events             []lotteryEvent `json:"events,omitempty"`
	ResultDigest       string         `json:"result_digest"`
}
type lotteryWin struct {
	ID           int64      `json:"id"`
	LotteryID    int64      `json:"lottery_id"`
	EntryID      int64      `json:"-"`
	UserID       int64      `json:"user_id,omitempty"`
	Title        string     `json:"title,omitempty"`
	UserEmail    string     `json:"user_email,omitempty"`
	UserName     string     `json:"user_name"`
	PrizeKey     string     `json:"prize_key"`
	Amount       string     `json:"amount"`
	Signature    string     `json:"-"`
	CreatedAt    time.Time  `json:"created_at"`
	ClaimedAt    *time.Time `json:"claimed_at"`
	RedeemID     *int64     `json:"redeem_id,omitempty"`
	BalanceAfter *float64   `json:"balance_after,omitempty"`
	WinCount     int        `json:"win_count,omitempty"`
}
type lotteryEvent struct {
	ID        int64           `json:"id"`
	Action    string          `json:"action"`
	ActorID   *int64          `json:"actor_id"`
	Detail    json.RawMessage `json:"detail"`
	CreatedAt time.Time       `json:"created_at"`
}
type lotteryEvidence struct {
	UserID        int64     `json:"user_id"`
	Balance       string    `json:"balance"`
	RegisteredAt  time.Time `json:"registered_at"`
	SpendToday    string    `json:"spend_today"`
	SpendTotal    string    `json:"spend_total"`
	RechargeToday string    `json:"recharge_today"`
	RechargeTotal string    `json:"recharge_total"`
	BusinessDate  string    `json:"business_date"`
}

type lotteryDB interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func normalizeLotterySpec(s *lotterySpec) error {
	var err error
	s.Title, err = trimText(s.Title, 80)
	if err != nil || s.Title == "" {
		return badRequest("请填写不超过 80 字的活动名称。")
	}
	s.Description, err = trimText(s.Description, 1500)
	if err != nil {
		return err
	}
	if s.Match != "all" && s.Match != "any" {
		return badRequest("请选择满足全部条件或任意条件。")
	}
	if _, err = time.ParseInLocation("2006-01-02", s.BusinessDate, lotteryLocation); err != nil {
		return badRequest("请设置统计日期。")
	}
	if len(s.Conditions) > 10 {
		return badRequest("每场活动最多设置 10 个条件。")
	}
	for i := range s.Conditions {
		c := &s.Conditions[i]
		switch c.Kind {
		case "spend_today", "spend_total", "recharge_today", "recharge_total", "balance_min":
			if len(c.Value) > 64 {
				return badRequest("条件金额过长。")
			}
			value, e := decimal.NewFromString(c.Value)
			if e != nil || value.IsNegative() || value.Exponent() < -8 || value.Exponent() > 8 || value.GreaterThan(decimal.NewFromInt(100000000)) {
				return badRequest("条件金额应在 0 至 100000000 之间，最多八位小数。")
			}
			c.Value = value.String()
		case "registered_before", "registered_after":
			if _, e := time.ParseInLocation("2006-01-02", c.Value, lotteryLocation); e != nil {
				return badRequest("请检查注册日期。")
			}
		case "user_ids":
			ids := strings.Split(c.Value, ",")
			if len(ids) > 1000 {
				return badRequest("指定用户最多 1000 人。")
			}
			for j, id := range ids {
				n, e := strconv.ParseInt(strings.TrimSpace(id), 10, 64)
				if e != nil || n < 1 {
					return badRequest("请使用逗号分隔用户编号。")
				}
				ids[j] = strconv.FormatInt(n, 10)
			}
			c.Value = strings.Join(ids, ",")
		default:
			return badRequest("抽奖条件无法识别。")
		}
	}
	if s.DrawMode != "time" && s.DrawMode != "full" && s.DrawMode != "either" {
		return badRequest("请选择开奖方式。")
	}
	if s.StartAt.IsZero() {
		return badRequest("请设置报名开始时间。")
	}
	if s.Capacity < 0 || s.Capacity > 100000 {
		return badRequest("报名上限为 1 至 100000 人，定时开奖可填 0 表示不限。")
	}
	if s.DrawMode != "time" && s.Capacity < 1 {
		return badRequest("人满开奖需要设置报名人数。")
	}
	if s.DrawMode != "full" && (s.DrawAt == nil || !s.DrawAt.After(s.StartAt)) {
		return badRequest("开奖时间需要晚于报名开始时间。")
	}
	if s.DrawMode == "full" {
		s.DrawAt = nil
	}
	if len(s.Prizes) < 1 || len(s.Prizes) > 5 {
		return badRequest("至少启用一个奖项。")
	}
	seen := map[string]bool{}
	count := 0
	total := decimal.Zero
	for i := range s.Prizes {
		p := &s.Prizes[i]
		if lotteryPrizeNames[p.Key] == "" || seen[p.Key] {
			return badRequest("奖项重复或无法识别。")
		}
		seen[p.Key] = true
		if len(p.Amount) > 64 {
			return badRequest("奖金金额过长。")
		}
		amount, e := decimal.NewFromString(p.Amount)
		if e != nil || !amount.IsPositive() || amount.Exponent() < -8 || amount.Exponent() > 8 || amount.GreaterThan(decimal.NewFromInt(1000000)) {
			return badRequest("每份奖金应大于 0、最多八位小数，且不超过 1000000。")
		}
		if p.Count < 1 || p.Count > 100000 {
			return badRequest("每个奖项人数应在 1 至 100000 之间。")
		}
		p.Amount = amount.String()
		count += p.Count
		total = total.Add(amount.Mul(decimal.NewFromInt(int64(p.Count))))
	}
	if count > 100000 || total.GreaterThan(decimal.NewFromInt(100000000)) {
		return badRequest("总奖额或中奖人数超过上限。")
	}
	if s.Capacity > 0 && count > s.Capacity {
		return badRequest("奖项总人数不能超过报名名额。")
	}
	ordered := make([]lotteryPrize, 0, len(s.Prizes))
	for _, key := range lotteryPrizeOrder {
		for _, p := range s.Prizes {
			if p.Key == key {
				ordered = append(ordered, p)
			}
		}
	}
	s.Prizes = ordered
	return nil
}

func lotteryTotals(s lotterySpec) (string, int) {
	total := decimal.Zero
	count := 0
	for _, p := range s.Prizes {
		amount, _ := decimal.NewFromString(p.Amount)
		total = total.Add(amount.Mul(decimal.NewFromInt(int64(p.Count))))
		count += p.Count
	}
	return total.String(), count
}

// 签名密钥独立生成并使用现有加密服务落库，不随登录令牌轮换失效。
func (h *Handler) lotteryKey(ctx context.Context) ([]byte, error) {
	if h.encryptor == nil {
		return nil, fmt.Errorf("抽奖签名服务未配置")
	}
	var record struct {
		Key string `json:"encrypted_key"`
	}
	if err := h.getRuntime(ctx, "lottery_signing_key", &record); err != nil {
		return nil, err
	}
	if record.Key == "" {
		key := make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, key); err != nil {
			return nil, err
		}
		blob, err := h.encryptor.Encrypt(hex.EncodeToString(key))
		if err != nil {
			return nil, err
		}
		raw, _ := json.Marshal(map[string]string{"encrypted_key": blob})
		if _, err = h.db.ExecContext(ctx, `INSERT INTO workspace_runtime(key,value) VALUES('lottery_signing_key',$1) ON CONFLICT(key) DO NOTHING`, string(raw)); err != nil {
			return nil, err
		}
		if err = h.getRuntime(ctx, "lottery_signing_key", &record); err != nil {
			return nil, err
		}
	}
	plain, err := h.encryptor.Decrypt(record.Key)
	if err != nil {
		return nil, err
	}
	key, err := hex.DecodeString(plain)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("抽奖签名密钥无法读取")
	}
	return key, nil
}
func lotterySign(key []byte, domain string, value any) string {
	raw, _ := json.Marshal(value)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(domain + "\x00"))
	mac.Write(raw)
	return hex.EncodeToString(mac.Sum(nil))
}
func lotterySignatureOK(a, b string) bool { return hmac.Equal([]byte(a), []byte(b)) }
func lotterySpecSignature(key []byte, a lotteryActivity) string {
	return lotterySign(key, "spec", []any{a.ID, a.CreatedBy, a.Spec})
}
func lotteryWinSignature(key []byte, w lotteryWin) string {
	return lotterySign(key, "win", []any{w.LotteryID, w.EntryID, w.UserID, w.PrizeKey, w.Amount})
}

const lotteryColumns = `id,spec,signature,status,created_by,created_at,published_at,drawn_at,eligible_count,result_digest`

func scanLottery(row rowScanner) (lotteryActivity, error) {
	a := lotteryActivity{}
	var raw []byte
	err := row.Scan(&a.ID, &raw, &a.Signature, &a.Status, &a.CreatedBy, &a.CreatedAt, &a.PublishedAt, &a.DrawnAt, &a.EligibleCount, &a.ResultDigest)
	if err != nil {
		return a, err
	}
	err = json.Unmarshal(raw, &a.Spec)
	a.PrizeTotal, a.PrizeCount = lotteryTotals(a.Spec)
	return a, err
}
func (h *Handler) loadLottery(ctx context.Context, db lotteryDB, id int64, lock bool, key []byte) (lotteryActivity, error) {
	suffix := ""
	if lock {
		suffix = " FOR UPDATE"
	}
	a, err := scanLottery(db.QueryRowContext(ctx, "SELECT "+lotteryColumns+" FROM workspace_lotteries WHERE id=$1"+suffix, id))
	if err != nil {
		return a, err
	}
	if !lotterySignatureOK(a.Signature, lotterySpecSignature(key, a)) {
		return a, &requestError{409, "活动规则校验未通过，请联系管理员。"}
	}
	return a, nil
}

func lotteryDue(a lotteryActivity, count int, now time.Time) bool {
	if a.Status != "open" || now.Before(a.Spec.StartAt) {
		return false
	}
	return (a.Spec.DrawMode != "full" && a.Spec.DrawAt != nil && !now.Before(*a.Spec.DrawAt)) || (a.Spec.DrawMode != "time" && a.Spec.Capacity > 0 && count >= a.Spec.Capacity)
}

func lotteryAudit(ctx context.Context, db lotteryDB, id int64, actorID *int64, action string, detail any) error {
	raw, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `INSERT INTO workspace_lottery_events(lottery_id,actor_id,action,detail) VALUES($1,$2,$3,$4)`, id, actorID, action, string(raw))
	return err
}
