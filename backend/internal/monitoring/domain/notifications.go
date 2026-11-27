package domain

import (
	"errors"
	"net/mail"
	"net/url"
	"slices"
	"strings"
	"time"
)

// 渠道只描述连接；密钥单独加密保存，不进入配置查询和事件快照。
type Channel struct {
	LastState        string            `json:"last_state,omitempty"`
	LastError        string            `json:"last_error,omitempty"`
	LastAccepted     *time.Time        `json:"last_accepted,omitempty"`
	ID               string            `json:"id"`
	Kind             string            `json:"kind"`
	Name             string            `json:"name"`
	Enabled          bool              `json:"enabled"`
	Settings         map[string]string `json:"settings"`
	SecretConfigured []string          `json:"secret_configured"`
	Revision         int               `json:"revision"`
	UpdatedAt        time.Time         `json:"updated_at"`
}

type ChannelInput struct {
	Channel
	Secrets      map[string]string `json:"secrets,omitempty"`
	ClearSecrets []string          `json:"clear_secrets,omitempty"`
	Targets      []Target          `json:"targets"`
}

type Target struct {
	ID          string            `json:"id"`
	ChannelID   string            `json:"channel_id"`
	Name        string            `json:"name"`
	Address     string            `json:"address"`
	Enabled     bool              `json:"enabled"`
	Settings    map[string]string `json:"settings"`
	ChannelKind string            `json:"channel_kind,omitempty"`
}

type NotificationPolicy struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Enabled           bool     `json:"enabled"`
	Rules             []string `json:"rules"`
	Platforms         []string `json:"platforms"`
	GroupIDs          []int64  `json:"group_ids"`
	AccountIDs        []int64  `json:"account_ids"`
	Models            []string `json:"models"`
	Severities        []string `json:"severities"`
	Kinds             []string `json:"kinds"`
	TargetIDs         []string `json:"target_ids"`
	FallbackTargetIDs []string `json:"fallback_target_ids"`
	RepeatMinutes     int      `json:"repeat_minutes"`
	MentionAll        bool     `json:"mention_all"`
}

var ChannelKinds = []string{"email", "feishu_webhook", "feishu_app", "lark_webhook", "lark_app", "telegram", "wecom", "dingtalk", "qq", "webhook"}

func SupportsEdit(kind string) bool {
	return kind == "telegram" || kind == "feishu_app" || kind == "lark_app"
}

func (p NotificationPolicy) Matches(i Incident) bool {
	return p.Enabled && (len(p.Rules) == 0 || slices.Contains(p.Rules, i.Rule)) &&
		(len(p.Platforms) == 0 || slices.Contains(p.Platforms, i.Platform)) &&
		(len(p.GroupIDs) == 0 || slices.Contains(p.GroupIDs, i.GroupID)) &&
		(len(p.Models) == 0 || slices.Contains(p.Models, i.Model)) &&
		(len(p.AccountIDs) == 0 || i.Sample != nil && slices.Contains(p.AccountIDs, i.Sample.AccountID)) &&
		(len(p.Severities) > 0 && slices.Contains(p.Severities, i.Severity) || len(p.Severities) == 0 && i.Severity <= "P1")
}

func (p NotificationPolicy) Allows(kind string, acknowledged bool) bool {
	if kind == "reminder" {
		return p.RepeatMinutes > 0 && !acknowledged
	}
	if kind == "update" || kind == "closed" {
		return true
	}
	if len(p.Kinds) == 0 {
		return kind == "firing" || kind == "escalated" || kind == "resolved"
	}
	return slices.Contains(p.Kinds, kind)
}

// 只解释当前可确认的配置限制，不把当前配置冒充历史未通知的确定原因。
func CurrentNotificationConstraint(i Incident, cfg Config, now time.Time) string {
	switch {
	case i.Rule == "L02":
		return "single_request"
	case !cfg.Rule(i.Rule).Enabled:
		return "rule_disabled"
	case cfg.Shadow:
		return "record_only"
	case cfg.Owner(i.Rule, i.Scope, i.GroupID, i.Model) != "monitor":
		return "legacy_owner"
	case i.ParentID != "":
		return "child"
	case now.Before(i.SilencedUntil):
		return "silenced"
	}
	matched := false
	for _, policy := range cfg.Policies {
		if !policy.Matches(i) {
			continue
		}
		matched = true
		for _, target := range cfg.Targets {
			if target.Enabled && slices.Contains(policy.TargetIDs, target.ID) {
				return "criteria"
			}
		}
	}
	if matched {
		return "no_target"
	}
	return "no_policy"
}

func ValidatePolicies(policies []NotificationPolicy) error {
	if len(policies) > 100 {
		return errors.New("通知规则最多 100 条")
	}
	ids := map[string]bool{}
	for _, p := range policies {
		if p.ID == "" || !safeCode.MatchString(p.ID) || ids[p.ID] || len(p.Name) > 100 || p.RepeatMinutes < 0 || p.RepeatMinutes > 1440 || p.RepeatMinutes > 0 && p.RepeatMinutes < 5 {
			return errors.New("请检查通知规则名称、编号和提醒间隔（至少 5 分钟）")
		}
		ids[p.ID] = true
		if len(p.TargetIDs) > 50 || len(p.FallbackTargetIDs) > 20 || len(p.Rules) > 100 || len(p.Models) > 100 || len(p.GroupIDs) > 100 || len(p.AccountIDs) > 100 {
			return errors.New("通知规则的筛选或接收对象过多")
		}
		for _, v := range append(append(append([]string{}, p.TargetIDs...), p.FallbackTargetIDs...), p.Rules...) {
			if v == "" || !safeID.MatchString(v) {
				return errors.New("通知规则包含无法识别的编号")
			}
		}
		for _, v := range p.Severities {
			if !slices.Contains([]string{"P0", "P1", "P2", "P3"}, v) {
				return errors.New("请选择通知严重程度")
			}
		}
		for _, v := range p.Kinds {
			if !slices.Contains([]string{"firing", "escalated", "resolved"}, v) {
				return errors.New("请选择首次、升级或恢复通知")
			}
		}
	}
	return nil
}

func (c Channel) Validate() error {
	if c.ID == "" || !safeCode.MatchString(c.ID) || !slices.Contains(ChannelKinds, c.Kind) || strings.TrimSpace(c.Name) == "" || len(c.Name) > 100 || len(c.Settings) > 12 {
		return errors.New("请填写渠道名称并选择支持的通知平台")
	}
	for k, v := range c.Settings {
		if !slices.Contains([]string{"app_id", "robot_code"}, k) || len(v) > 256 || strings.ContainsAny(v, "\r\n\x00") {
			return errors.New("渠道设置包含未支持的字段")
		}
	}
	return nil
}

func (t Target) Validate(kind string) error {
	if t.ID == "" || !safeCode.MatchString(t.ID) || strings.TrimSpace(t.Name) == "" || len(t.Name) > 100 || t.Address == "" || len(t.Address) > 256 || strings.ContainsAny(t.Address, "\r\n\x00") || len(t.Settings) > 8 {
		return errors.New("请填写接收对象名称和有效标识")
	}
	if kind == "email" {
		a, e := mail.ParseAddress(t.Address)
		if e != nil || a.Address != t.Address {
			return errors.New("请填写正确的收件邮箱")
		}
	}
	if strings.Contains(kind, "webhook") || kind == "wecom" {
		if t.Address != "group" {
			return errors.New("群机器人使用当前 Webhook 对应的群")
		}
	}
	for k, v := range t.Settings {
		if !slices.Contains([]string{"type", "thread_id", "mentions"}, k) || len(v) > 1000 || strings.ContainsAny(v, "\r\n\x00") {
			return errors.New("接收对象设置包含未支持的字段")
		}
	}
	if kind == "qq" && t.Settings["type"] != "user" && t.Settings["type"] != "group" {
		return errors.New("QQ 对象请选择用户或群")
	}
	if kind == "dingtalk" && t.Settings["type"] != "user" && t.Settings["type"] != "group" {
		return errors.New("钉钉对象请选择用户或群")
	}
	return nil
}

func ValidHTTPS(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.Fragment == ""
}

func LegacyTakenOver(c Config, id int64, platform string, group int64, model string) bool {
	if c.Shadow {
		return false
	}
	for _, h := range c.LegacyHandoffs {
		if h.LegacyRuleID != id || h.Platform != "" && h.Platform != platform || h.GroupID != nil && *h.GroupID != group {
			continue
		}
		ready := len(h.MonitorRules) > 0
		for _, rule := range h.MonitorRules {
			if !c.Rule(rule).Enabled || c.Owner(rule, "", group, model) != "monitor" {
				ready = false
			}
		}
		if ready {
			for _, rule := range h.MonitorRules {
				covered := false
				for _, p := range c.Policies {
					if !p.Enabled || len(p.Rules) > 0 && !slices.Contains(p.Rules, rule) || len(p.Platforms) > 0 && !slices.Contains(p.Platforms, platform) || len(p.GroupIDs) > 0 && !slices.Contains(p.GroupIDs, group) || len(p.AccountIDs) > 0 || len(p.Models) > 0 {
						continue
					}
					for _, target := range c.Targets {
						if target.Enabled && slices.Contains(p.TargetIDs, target.ID) {
							covered = true
						}
					}
				}
				if !covered {
					ready = false
				}
			}
			if ready {
				return true
			}
		}
	}
	return false
}
