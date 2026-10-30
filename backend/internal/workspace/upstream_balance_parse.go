package workspace

import (
	"encoding/json"
	"strconv"
	"strings"
)

type balanceQueryError struct {
	status  int
	message string
}

func (e *balanceQueryError) Error() string { return e.message }
func balanceError(message string) error    { return &balanceQueryError{message: message} }
func balanceData(p map[string]any) map[string]any {
	if d, ok := p["data"].(map[string]any); ok {
		return d
	}
	return p
}
func balanceNumber(p map[string]any, names ...string) (float64, bool) {
	for _, name := range names {
		var n float64
		var err error
		switch v := p[name].(type) {
		case json.Number:
			n, err = v.Float64()
		case float64:
			n = v
		case string:
			n, err = strconv.ParseFloat(v, 64)
		default:
			continue
		}
		if err == nil && finiteBalance(n) {
			return n, true
		}
		return 0, false
	}
	return 0, false
}
func balanceText(p map[string]any, name string) string { s, _ := p[name].(string); return s }
func balanceRejected(p map[string]any) bool {
	if v, ok := p["success"].(bool); ok && !v {
		return true
	}
	if v, ok := p["code"].(bool); ok && !v {
		return true
	}
	if n, ok := balanceNumber(p, "code"); ok && n != 0 && n != 200 {
		return true
	}
	return p["error"] != nil
}
func parseSubUpstreamBalance(p map[string]any) (upstreamBalanceReading, error) {
	r := upstreamBalanceReading{Kind: "sub2api", Scope: "wallet", Unit: "USD"}
	if balanceRejected(p) {
		return r, balanceError("Sub2API 拒绝余额查询，请检查 Key 和接口权限。")
	}
	d := balanceData(p)
	if valid, ok := d["isValid"].(bool); ok && !valid {
		return r, balanceError("Sub2API 返回 Key 已失效。")
	}
	if unit := balanceText(d, "unit"); unit != "" && len(unit) <= 16 {
		r.Unit = unit
	}
	// Modern /v1/usage distinguishes wallet, subscription and limited-key quota.
	// A finite wallet must never be hidden by the legacy quota_limited=false flag.
	if n, ok := balanceNumber(d, "wallet_balance", "balance", "remaining_balance"); ok {
		r.Balance = &n
		return r, nil
	}
	if d["subscription"] != nil || (balanceText(d, "planName") != "" && balanceText(d, "planName") != "钱包余额") {
		r.Scope = "subscription"
	}
	if balanceText(d, "mode") == "quota_limited" {
		r.Scope = "key"
	}
	if limited, ok := d["quota_limited"].(bool); ok && limited {
		r.Scope = "key"
	}
	n, ok := balanceNumber(d, "remaining")
	if !ok {
		if q, yes := d["quota"].(map[string]any); yes {
			n, ok = balanceNumber(q, "remaining")
			r.Scope = "key"
		}
	}
	if ok {
		if n == -1 {
			r.Unlimited = true
			if r.Scope == "wallet" {
				r.Scope = "key"
			}
		} else {
			r.Balance = &n
		}
		return r, nil
	}
	if limited, yes := d["quota_limited"].(bool); yes && !limited {
		r.Scope = "key"
		r.Unlimited = true
		return r, nil
	}
	return r, balanceError("Sub2API 未返回钱包余额或可识别的剩余额度。")
}

func convertNewAPIQuota(quota float64, status map[string]any) (float64, string, error) {
	d := balanceData(status)
	if balanceRejected(status) {
		return 0, "", balanceError("New API 状态接口拒绝查询。")
	}
	per, ok := balanceNumber(d, "quota_per_unit")
	if !ok || per <= 0 {
		return 0, "", balanceError("New API 缺少有效的 quota_per_unit 换算参数。")
	}
	value, unit := quota/per, "USD"
	switch strings.ToUpper(balanceText(d, "quota_display_type")) {
	case "", "USD":
	case "CNY":
		rate, ok := balanceNumber(d, "usd_exchange_rate")
		if !ok || rate <= 0 {
			return 0, "", balanceError("New API 缺少有效的汇率配置。")
		}
		value *= rate
		unit = "CNY"
	case "TOKENS":
		value = quota
		unit = "Tokens"
	case "CUSTOM":
		rate, ok := balanceNumber(d, "custom_currency_exchange_rate")
		unit = balanceText(d, "custom_currency_symbol")
		if !ok || rate <= 0 || len(unit) == 0 || len(unit) > 16 {
			return 0, "", balanceError("New API 自定义币种配置不完整。")
		}
		value *= rate
	default:
		return 0, "", balanceError("暂不支持这个 New API 余额展示单位。")
	}
	if !finiteBalance(value) {
		return 0, "", balanceError("New API 余额换算结果无效。")
	}
	return value, unit, nil
}
func parseNewAPITokenBalance(p, status map[string]any) (upstreamBalanceReading, error) {
	r := upstreamBalanceReading{Kind: "newapi", Scope: "key"}
	if balanceRejected(p) {
		return r, balanceError("New API 拒绝 Key 余额查询，请检查 Key 和查询权限。")
	}
	d := balanceData(p)
	q := d
	// Newer New API explicitly exposes the user's wallet when permitted.
	// Read it before unlimited_quota: that flag belongs to the KEY, not the wallet.
	if wallet, ok := d["user_quota"].(map[string]any); ok {
		q = wallet
		r.Scope = "wallet"
	} else if unlimited, ok := d["unlimited_quota"].(bool); ok && unlimited {
		_, unit, err := convertNewAPIQuota(0, status)
		r.Unit = unit
		r.Unlimited = true
		return r, err
	}
	raw, ok := balanceNumber(q, "total_available")
	if !ok {
		return r, balanceError("New API 未返回可识别的额度。")
	}
	n, unit, err := convertNewAPIQuota(raw, status)
	if err != nil {
		return r, err
	}
	r.Balance = &n
	r.Unit = unit
	return r, nil
}
func parseNewAPIWalletBalance(p, status map[string]any) (upstreamBalanceReading, error) {
	r := upstreamBalanceReading{Kind: "newapi", Scope: "wallet"}
	if balanceRejected(p) {
		return r, &balanceQueryError{status: 401, message: "钱包鉴权未通过，请检查钱包访问令牌和 New API 用户 ID。"}
	}
	d := balanceData(p)
	raw, ok := balanceNumber(d, "quota")
	if !ok {
		return r, balanceError("New API 未返回钱包额度；请检查钱包访问令牌和用户 ID。")
	}
	n, unit, err := convertNewAPIQuota(raw, status)
	if err != nil {
		return r, err
	}
	r.Balance = &n
	r.Unit = unit
	if id, ok := balanceNumber(d, "id"); ok && id > 0 && id < 1<<53 {
		r.WalletUserID = int64(id)
	}
	return r, nil
}
