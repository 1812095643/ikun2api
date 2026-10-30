package workspace

import (
	"fmt"
	"math"
	"net/mail"
	"strings"
	"time"
)

// Keys remain in accounts. Only an optional New API personal wallet token is
// stored separately, using the existing SecretEncryptor.
type upstreamBalanceConfig struct {
	Kind               string   `json:"kind"`
	AuthMode           string   `json:"auth_mode"`
	NewAPIUserID       int64    `json:"new_api_user_id"`
	PreferredAccountID int64    `json:"preferred_account_id"`
	MonitorEnabled     bool     `json:"monitor_enabled"`
	IntervalMinutes    int      `json:"interval_minutes"`
	Threshold          float64  `json:"threshold"`
	Recipients         []string `json:"recipients"`
}

func defaultUpstreamBalanceConfig() upstreamBalanceConfig {
	return upstreamBalanceConfig{Kind: "auto", AuthMode: "auto", IntervalMinutes: 30, Threshold: 10, Recipients: []string{}}
}

func normalizeUpstreamBalanceConfig(c *upstreamBalanceConfig) error {
	if c.Kind != "auto" && c.Kind != "sub2api" && c.Kind != "newapi" {
		return badRequest("请选择自动识别、Sub2API 或 New API。")
	}
	if c.AuthMode != "auto" && c.AuthMode != "modern" && c.AuthMode != "legacy" {
		return badRequest("请选择有效的钱包鉴权方式。")
	}
	if c.NewAPIUserID < 0 || c.PreferredAccountID < 0 || (c.AuthMode == "legacy" && c.NewAPIUserID == 0) {
		return badRequest("旧版 New API 需要填写用户 ID。")
	}
	if c.IntervalMinutes < 5 || c.IntervalMinutes > 1440 || !finiteBalance(c.Threshold) || c.Threshold < 0 || c.Threshold > 1e12 {
		return badRequest("检查间隔为 5～1440 分钟，余额阈值须为有效的非负数。")
	}
	if len(c.Recipients) > 10 {
		return badRequest("提醒邮箱最多填写 10 个。")
	}
	seen := map[string]bool{}
	normalized := []string{}
	for _, value := range c.Recipients {
		value = strings.TrimSpace(value)
		a, err := mail.ParseAddress(value)
		if err != nil || a.Address != value || len(value) > 254 || strings.ContainsAny(value, "\r\n") {
			return badRequest("请填写有效的提醒邮箱，一行一个。")
		}
		value = strings.ToLower(a.Address)
		if !seen[value] {
			seen[value] = true
			normalized = append(normalized, value)
		}
	}
	c.Recipients = normalized
	return nil
}

func finiteBalance(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

type upstreamBalanceAccount struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
	key      string
	proxyID  *int64
}

type upstreamBalanceGroup struct {
	ID         string
	URL        string
	Name       string
	SourceHash string
	Accounts   []upstreamBalanceAccount
}

type upstreamBalanceReading struct {
	AccountIDs   []int64  `json:"account_ids"`
	AccountNames []string `json:"account_names"`
	Kind         string   `json:"kind"`
	Scope        string   `json:"scope"`
	Balance      *float64 `json:"balance"`
	Unit         string   `json:"unit"`
	Unlimited    bool     `json:"unlimited"`
	WalletUserID int64    `json:"wallet_user_id,omitempty"`
	Error        string   `json:"error,omitempty"`
}

type upstreamBalanceSnapshot struct {
	Readings   []upstreamBalanceReading `json:"readings"`
	Comparison string                   `json:"comparison"`
	Balance    *float64                 `json:"balance"`
	Unit       string                   `json:"unit"`
	Scope      string                   `json:"scope"`
	Unlimited  bool                     `json:"unlimited"`
	Successful int                      `json:"successful"`
	Total      int                      `json:"total"`
	Error      string                   `json:"error,omitempty"`
}

type upstreamBalanceAlert struct {
	Low         bool       `json:"low"`
	SentTo      []string   `json:"-"`
	LastAttempt *time.Time `json:"last_attempt"`
	LastSent    *time.Time `json:"last_sent"`
	Error       string     `json:"error"`
}

// Explicit storage shape keeps email-delivery deduplication out of API output.
type upstreamBalanceAlertStored struct {
	upstreamBalanceAlert
	RecipientsSent []string `json:"recipients_sent"`
}

func summarizeUpstreamBalances(readings []upstreamBalanceReading, preferred int64) upstreamBalanceSnapshot {
	s := upstreamBalanceSnapshot{Readings: readings, Total: len(readings), Comparison: "unverified"}
	var first *upstreamBalanceReading
	var selected *upstreamBalanceReading
	same := true
	for i := range readings {
		r := &readings[i]
		if r.Error != "" {
			continue
		}
		s.Successful++
		if first == nil {
			first = r
		} else if !sameUpstreamBalance(*first, *r) {
			same = false
		}
		for _, id := range r.AccountIDs {
			if id == preferred {
				selected = r
			}
		}
	}
	switch {
	case s.Successful == 0:
		s.Error = "没有可用的余额读数，请查看 Key 明细。"
	case len(readings) == 1:
		s.Comparison = "single"
	case s.Successful < len(readings):
		s.Comparison = "partial"
	case !same:
		s.Comparison = "different"
	default:
		s.Comparison = "consistent"
	}
	// Never sum balances for multiple keys. An ambiguous/partially failed group
	// cannot drive a wallet alarm until an administrator chooses its source.
	if preferred == 0 && same && s.Successful == len(readings) {
		selected = first
	}
	if selected != nil {
		s.Balance, s.Unit, s.Scope, s.Unlimited = selected.Balance, selected.Unit, selected.Scope, selected.Unlimited
	} else if s.Successful > 0 {
		s.Error = "读数不一致或部分 Key 查询失败，请指定监控 Key。"
	}
	return s
}

func sameUpstreamBalance(a, b upstreamBalanceReading) bool {
	if a.Unit != b.Unit || a.Scope != b.Scope || a.Unlimited != b.Unlimited || (a.WalletUserID > 0 && b.WalletUserID > 0 && a.WalletUserID != b.WalletUserID) {
		return false
	}
	if a.Unlimited {
		return true
	}
	return a.Balance != nil && b.Balance != nil && math.Abs(*a.Balance-*b.Balance) <= 0.010000001
}

func upstreamBalanceID(value string) string { return digestString("upstream-balance:" + value) }

func validateWalletToken(value string) error {
	if len(value) > 8192 || strings.ContainsAny(value, "\r\n\x00") || strings.ContainsAny(value, " \t") {
		return badRequest("钱包访问令牌格式不正确。")
	}
	return nil
}

func balanceStatusError(status int) error {
	if status == 401 || status == 403 {
		return &balanceQueryError{status: status, message: "余额接口鉴权失败，请检查 Key；New API 请配置钱包访问令牌和用户 ID。"}
	}
	return &balanceQueryError{status: status, message: fmt.Sprintf("余额接口返回 HTTP %d。", status)}
}
