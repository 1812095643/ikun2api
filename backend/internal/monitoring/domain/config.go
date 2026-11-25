package domain

import (
	"errors"
	"fmt"
	"math"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
)

var targetPattern = regexp.MustCompile(`^(oc_|ou_)[a-zA-Z0-9_\-]{1,128}$`)

func (c Config) Validate() error {
	if len(c.SiteName) > 120 || strings.ContainsAny(c.SiteName, "\r\n\x00") {
		return errors.New("站点名称过长或包含换行")
	}
	if err := ValidatePolicies(c.Policies); err != nil {
		return err
	}
	if c.RetentionDays != 0 && (c.RetentionDays < 7 || c.RetentionDays > 365) {
		return errors.New("保留时间应为 7–365 天")
	}
	if c.Site == "" || !safeCode.MatchString(c.Site) || len(c.Profiles) > 100 || len(c.Rules) > 100 {
		return errors.New("invalid monitor configuration")
	}
	if c.PaymentGraceSeconds < 30 || c.PaymentGraceSeconds > 86400 || c.BillingGraceSeconds < 30 || c.BillingGraceSeconds > 86400 {
		return errors.New("grace must be between 30 and 86400 seconds")
	}
	for _, v := range []*float64{c.BalanceLow, c.SubscriptionLow} {
		if v != nil && (math.IsNaN(*v) || math.IsInf(*v, 0) || *v <= 0) {
			return errors.New("threshold must be finite and positive")
		}
	}
	if c.SubscriptionLow != nil && *c.SubscriptionLow >= 1 {
		return errors.New("subscription low ratio must be below one")
	}
	for model, p := range c.Profiles {
		if !safeID.MatchString(model) {
			return errors.New("invalid profile model")
		}
		for _, pair := range [][2]int{{p.QueueWarnSeconds, p.QueueFailSeconds}, {p.FirstWarnSeconds, p.FirstFailSeconds}, {p.StallWarnSeconds, p.StallFailSeconds}} {
			if pair[0] < 1 || pair[1] <= pair[0] || pair[1] > 86400 {
				return errors.New("invalid profile timing thresholds")
			}
		}
		if p.TaskSLASeconds < 0 || p.TaskSLASeconds > 30*86400 {
			return errors.New("invalid task SLA")
		}
	}
	for id, r := range c.Rules {
		if !safeCode.MatchString(id) || r.Version < 1 || (r.Owner != "legacy" && r.Owner != "monitor") {
			return errors.New("invalid rule configuration")
		}
	}
	if len(c.FeishuTargets) > 20 || len(c.EmailTargets) > 20 {
		return errors.New("maximum twenty recipients per channel")
	}
	seen := map[string]bool{}
	for _, id := range c.FeishuTargets {
		if !targetPattern.MatchString(id) || seen[id] {
			return errors.New("invalid or duplicate Feishu recipient")
		}
		seen[id] = true
	}
	seen = map[string]bool{}
	for _, email := range c.EmailTargets {
		a, e := mail.ParseAddress(email)
		if e != nil || a.Address != email || strings.ContainsAny(email, "\r\n") || seen[email] {
			return errors.New("invalid or duplicate email recipient")
		}
		seen[email] = true
	}
	if c.DetailBaseURL != "" {
		u, e := url.Parse(c.DetailBaseURL)
		if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("details base must be a credential-free HTTPS URL")
		}
	}
	if len(c.OwnerOverrides) > 200 || len(c.LegacyHandoffs) > 200 {
		return errors.New("ownership mapping too large")
	}
	selectors := map[string]bool{}
	for _, o := range c.OwnerOverrides {
		if !safeCode.MatchString(o.Rule) || o.Scope == "" && o.GroupID == nil && o.Model == "" || !safeID.MatchString(o.Scope) || !safeID.MatchString(o.Model) || o.GroupID != nil && *o.GroupID < 0 || (o.Owner != "legacy" && o.Owner != "monitor") {
			return errors.New("invalid scoped owner")
		}
		group := "*"
		if o.GroupID != nil {
			group = fmt.Sprint(*o.GroupID)
		}
		key := o.Rule + "|" + o.Scope + "|" + group + "|" + o.Model
		if selectors[key] {
			return errors.New("duplicate ownership selector")
		}
		selectors[key] = true
	}
	for _, h := range c.LegacyHandoffs {
		if h.LegacyRuleID <= 0 || len(h.MonitorRules) == 0 || len(h.MonitorRules) > 28 || !safeCode.MatchString(h.Platform) || h.GroupID != nil && *h.GroupID < 0 {
			return errors.New("invalid legacy handoff")
		}
		for _, id := range h.MonitorRules {
			if !safeCode.MatchString(id) {
				return errors.New("invalid handoff rule")
			}
		}
	}
	return nil
}
