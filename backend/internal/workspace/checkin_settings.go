package workspace

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"regexp"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/response"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

type checkinSettings struct {
	Enabled     bool   `json:"enabled"`
	Mode        string `json:"mode"`
	FixedAmount string `json:"fixed_amount"`
	RandomMin   string `json:"random_min"`
	RandomMax   string `json:"random_max"`
}

var checkinMoneyPattern = regexp.MustCompile(`^[0-9]{1,7}(\.[0-9]{1,8})?$`)

func normalizeCheckinSettings(s *checkinSettings) error {
	if s.Mode != "fixed" && s.Mode != "random" {
		return badRequest("请选择固定金额或随机金额。")
	}
	for _, field := range []*string{&s.FixedAmount, &s.RandomMin, &s.RandomMax} {
		if !checkinMoneyPattern.MatchString(*field) {
			return badRequest("金额须大于 0，最多保留 8 位小数。")
		}
		amount, err := decimal.NewFromString(*field)
		if err != nil || !amount.IsPositive() || amount.GreaterThan(decimal.NewFromInt(1000000)) {
			return badRequest("单次签到金额须大于 0，且不超过 1,000,000。")
		}
		*field = amount.String()
	}
	minimum, _ := decimal.NewFromString(s.RandomMin)
	maximum, _ := decimal.NewFromString(s.RandomMax)
	if minimum.GreaterThan(maximum) {
		return badRequest("随机金额上限不能低于下限。")
	}
	return nil
}

func loadCheckinSettings(ctx context.Context, db lotteryDB, lock bool) (checkinSettings, error) {
	s := checkinSettings{}
	query := `SELECT enabled,mode,fixed_amount::text,random_min::text,random_max::text FROM workspace_checkin_settings WHERE id=1`
	if lock {
		query += " FOR SHARE"
	}
	err := db.QueryRowContext(ctx, query).Scan(&s.Enabled, &s.Mode, &s.FixedAmount, &s.RandomMin, &s.RandomMax)
	if err == nil {
		err = normalizeCheckinSettings(&s)
	}
	return s, err
}

func checkinReward(s checkinSettings) (string, string, string, error) {
	if err := normalizeCheckinSettings(&s); err != nil {
		return "", "", "", err
	}
	if s.Mode == "fixed" {
		return s.FixedAmount, s.FixedAmount, s.FixedAmount, nil
	}
	minimum, _ := decimal.NewFromString(s.RandomMin)
	maximum, _ := decimal.NewFromString(s.RandomMax)
	// 常规区间按分随机；更小金额沿用管理员输入的精度，避免出现无意义的八位随机小数。
	precision := max(int32(2), -minimum.Exponent(), -maximum.Exponent())
	low, high := minimum.Shift(precision).BigInt(), maximum.Shift(precision).BigInt()
	width := new(big.Int).Add(new(big.Int).Sub(high, low), big.NewInt(1))
	offset, err := rand.Int(rand.Reader, width)
	if err != nil {
		return "", "", "", err
	}
	amount := decimal.NewFromBigInt(new(big.Int).Add(low, offset), -precision)
	return amount.String(), s.RandomMin, s.RandomMax, nil
}

func decodeCheckinBody(c *gin.Context, target any) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2048)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return badRequest("请检查提交的签到设置。")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return badRequest("请只提交一份签到设置。")
	}
	return nil
}

func (h *Handler) getCheckinSettings(c *gin.Context) {
	if !c.GetBool("workspace_admin") {
		fail(c, notFound())
		return
	}
	c.Header("Cache-Control", "no-store")
	s, err := loadCheckinSettings(c.Request.Context(), h.db, false)
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, s)
}

func (h *Handler) saveCheckinSettings(c *gin.Context) {
	if !c.GetBool("workspace_admin") {
		fail(c, notFound())
		return
	}
	a, err := h.identity(c)
	if err != nil {
		fail(c, err)
		return
	}
	var s checkinSettings
	if err = decodeCheckinBody(c, &s); err == nil {
		err = normalizeCheckinSettings(&s)
	}
	if err != nil {
		fail(c, err)
		return
	}
	result, err := h.db.ExecContext(c.Request.Context(), `UPDATE workspace_checkin_settings SET enabled=$1,mode=$2,fixed_amount=$3::numeric,random_min=$4::numeric,random_max=$5::numeric,updated_by=$6,updated_at=NOW() WHERE id=1`, s.Enabled, s.Mode, s.FixedAmount, s.RandomMin, s.RandomMax, a.ID)
	if err != nil {
		fail(c, err)
		return
	}
	if count, e := result.RowsAffected(); e != nil || count != 1 {
		fail(c, fmt.Errorf("签到设置未保存"))
		return
	}
	response.Success(c, s)
}

func invalidateCheckinBalance(h *Handler, ctx context.Context, userID int64) {
	cacheCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if invalidator, ok := h.admin.(interface{ InvalidateBalanceAfterCredit(context.Context, int64) }); ok {
		invalidator.InvalidateBalanceAfterCredit(cacheCtx, userID)
	}
}
