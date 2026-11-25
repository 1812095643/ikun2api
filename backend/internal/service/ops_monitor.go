package service

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/notify"
	monitorruntime "github.com/MACOS-DO/sub4api/internal/monitoring/runtime"
	"github.com/MACOS-DO/sub4api/internal/monitoring/sources"
	"github.com/MACOS-DO/sub4api/internal/monitoring/store"
	"github.com/MACOS-DO/sub4api/internal/monitoring/telemetry"
)

func (s *OpsService) Monitor() *monitorruntime.Native {
	if s == nil {
		return nil
	}
	return s.monitor
}

func (s *OpsService) startMonitor(db *sql.DB, email *EmailService, cipher SecretEncryptor) {
	if s == nil || db == nil || s.cfg == nil || !s.cfg.Ops.Enabled {
		return
	}
	instance, _ := os.Hostname()
	instance = telemetry.SafeID(instance)
	if instance == "" {
		instance = "gateway"
	}
	n := &monitorruntime.Native{Store: store.New(db), Site: "ikun2api", Instance: instance}
	n.Enabled = func() bool { return s.IsMonitoringEnabled(context.Background()) }
	n.Hub = telemetry.New(n.Site, instance, "embedded", 4096, 8192)
	n.Hub.Enabled = n.Enabled
	transport := &http.Transport{Proxy: nil, DialContext: safeDialContext, MaxIdleConns: 16, MaxIdleConnsPerHost: 4, IdleConnTimeout: 90 * time.Second, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second}
	sender := &notify.Sender{Client: &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, Email: s.monitorEmailSender(email)}
	n.Worker = &notify.Worker{Store: n.Store, Sender: sender, Cipher: cipher, LegacyAllowed: s.legacyMonitorAllowed, SiteName: func(ctx context.Context) string {
		if email != nil && email.notificationEmailService != nil {
			return email.notificationEmailService.siteName(ctx)
		}
		return ""
	}, DetailsURL: func(ctx context.Context) string {
		if email != nil && email.notificationEmailService != nil {
			if base := email.notificationEmailService.baseURL(ctx); base != "" {
				return strings.TrimSuffix(base, "/v1")
			}
		}
		return s.cfg.Server.FrontendURL
	}, Accepted: func(ctx context.Context, i domain.Incident) {
		if i.LegacyEventID > 0 {
			if all, err := n.Store.LegacyAllAccepted(ctx, i.ID); err == nil && all {
				_ = s.opsRepo.UpdateAlertEventEmailSent(ctx, i.LegacyEventID, true)
			}
		}
	}}
	activated := time.Now().UTC()
	for _, view := range sources.ViewNames {
		n.Evidence = append(n.Evidence, &sources.Postgres{DB: db, Timezone: s.cfg.Timezone, View: view, Site: n.Site, ActivatedAt: activated})
	}
	s.monitor = n
	telemetry.Install(n.Hub)
	n.Start()
}

func (s *OpsService) monitorEmailSender(email *EmailService) notify.EmailSender {
	limiter := newSlidingWindowLimiter(0, time.Hour)
	return func(ctx context.Context, to string, m notify.Message) error {
		if email == nil {
			return &notify.Failure{Class: "credentials", Permanent: true}
		}
		if m.Event.LegacyEventID > 0 {
			cfg, err := s.GetEmailNotificationConfig(ctx)
			if err != nil {
				return &notify.Failure{Class: "network"}
			}
			limiter.SetLimit(cfg.Alert.RateLimitPerHour)
			if !limiter.Allow(time.Now()) {
				return &notify.Failure{Class: "rate_limit", RetryAfter: time.Minute}
			}
		}
		rule := &OpsAlertRule{Name: m.Title, Severity: m.Event.Severity}
		event := &OpsAlertEvent{Title: m.Title, Description: m.Text, Status: m.Event.Status, FiredAt: m.Event.FirstSeen}
		vars := opsAlertEmailVariables(rule, event)
		// 旧规则继续使用原邮件字段，避免统一队列后丢失当前指标和告警阈值。
		if m.Event.LegacyEventID > 0 && len(m.EmailVariables) > 0 {
			vars = make(map[string]string, len(m.EmailVariables))
			for key, value := range m.EmailVariables {
				vars[key] = value
			}
			rule.Name = vars["rule_name"]
			rule.Severity = vars["severity"]
			rule.MetricType = vars["metric_type"]
			rule.Operator = vars["operator"]
			event.Description = vars["alert_description"]
			event.Status = vars["alert_status"]
			if value, e := strconv.ParseFloat(vars["metric_value"], 64); e == nil {
				event.MetricValue = &value
			}
			if value, e := strconv.ParseFloat(vars["threshold_value"], 64); e == nil {
				rule.Threshold = value
				event.ThresholdValue = &value
			}
		}
		event.Description += "\n事件：" + m.Event.ID
		if m.DetailURL != "" {
			event.Description += "\n详情：" + m.DetailURL
		}
		vars["alert_description"] = event.Description
		vars["event_id"] = m.Event.ID
		vars["detail_url"] = m.DetailURL
		vars["request_count"] = strconv.Itoa(m.Event.RequestCount)
		vars["account_count"] = strconv.Itoa(m.Event.AccountCount)
		sourceType, sourceID := "ops_monitor", fmt.Sprintf("%s:%s:%d", m.Event.ID, m.Kind, m.Event.Revision)
		if m.Event.LegacyEventID > 0 && m.Kind == "firing" {
			sourceType = "ops_alert"
			sourceID = fmt.Sprint(m.Event.LegacyEventID)
		}
		if email.notificationEmailService != nil {
			err := email.notificationEmailService.Send(ctx, NotificationEmailSendInput{Event: NotificationEmailEventOpsAlert, RecipientEmail: to, RecipientName: emailRecipientName(to), SourceType: sourceType, SourceID: sourceID, Variables: vars})
			if err == nil {
				return nil
			}
			if isNotificationEmailDeliveryError(err) {
				return &notify.Failure{Class: "uncertain", Uncertain: true}
			}
			if !shouldFallbackNotificationEmail(err) {
				return &notify.Failure{Class: "credentials", Permanent: true}
			}
		}
		if err := email.SendEmail(ctx, to, m.Title, buildOpsAlertEmailBody(rule, event)); err != nil {
			return &notify.Failure{Class: "uncertain", Uncertain: true}
		}
		return nil
	}
}

func (s *OpsService) legacyMonitorAllowed(ctx context.Context, i domain.Incident, t domain.Target) (bool, error) {
	cfg, err := s.GetEmailNotificationConfig(ctx)
	if err != nil {
		return false, err
	}
	if !cfg.Alert.Enabled || !slices.Contains(cfg.Alert.Recipients, t.Address) {
		return false, nil
	}
	if s.monitor == nil {
		return true, nil
	}
	control, err := s.monitor.Store.Config(ctx)
	if err != nil {
		return false, err
	}
	id, _ := strconv.ParseInt(strings.TrimPrefix(i.Rule, "legacy:"), 10, 64)
	return !domain.LegacyTakenOver(control, id, i.Platform, i.GroupID, i.Model), nil
}

func (s *OpsService) QueueLegacyAlert(ctx context.Context, rule *OpsAlertRule, event *OpsAlertEvent, recovery bool) (bool, error) {
	if s == nil || s.monitor == nil || rule == nil || event == nil {
		return false, nil
	}
	queued := false
	err := s.monitor.Store.WithConfigLock(ctx, func() error {
		cfg, err := s.monitor.Store.Config(ctx)
		if err != nil {
			return err
		}
		platform, group, _ := parseOpsAlertRuleScope(rule.Filters)
		gid := int64(0)
		if group != nil {
			gid = *group
		}
		if domain.LegacyTakenOver(cfg, rule.ID, platform, gid, "") {
			return nil
		}
		id := fmt.Sprintf("legacy-%d", event.ID)
		i, err := s.monitor.Store.Incident(ctx, id)
		if err != nil {
			return err
		}
		if i == nil {
			i = &domain.Incident{ID: id, Fingerprint: id, LegacyEventID: event.ID, Rule: fmt.Sprintf("legacy:%d", rule.ID), RuleVersion: 1, Scope: fmt.Sprintf("legacy:%d", rule.ID), Platform: platform, GroupID: gid, FirstSeen: event.FiredAt, LastFault: event.FiredAt, Status: "firing", Severity: monitorSeverity(rule.Severity), Quality: domain.Fresh, Explanation: domain.Explanation{Conclusion: rule.Name, Facts: []string{buildOpsAlertDescription(rule, opsMonitorMetricValue(event.MetricValue), rule.WindowMinutes, platform, group)}}}
		}
		kind := "firing"
		if recovery {
			kind = "resolved"
			i.Status = "resolved"
			i.ResolvedAt = time.Now().UTC()
		}
		i.Revision = 1
		if recovery {
			i.Revision = 2
		}
		i.LastSeen = time.Now().UTC()
		i.LegacyVariables = opsAlertEmailVariables(rule, event)
		i.LegacyVariables["alert_status"] = i.Status
		emailCfg, err := s.GetEmailNotificationConfig(ctx)
		if err != nil {
			return err
		}
		deliveries := []domain.Delivery{}
		if emailCfg.Alert.Enabled && rule.NotifyEmail && shouldSendOpsAlertEmailByMinSeverity(emailCfg.Alert.MinSeverity, rule.Severity) && (!recovery || emailCfg.Alert.IncludeResolvedAlerts) {
			for _, address := range emailCfg.Alert.Recipients {
				t, err := s.monitor.Store.LegacyRecipient(ctx, address)
				if err != nil {
					return err
				}
				deliveries = append(deliveries, domain.Delivery{ID: fmt.Sprintf("%s:%s:%s", id, kind, t.ID), IncidentID: id, Revision: i.Revision, Channel: t.ChannelID, Target: t.ID, Kind: kind, State: "pending", NextAttempt: time.Now().UTC()})
			}
		}
		if err = s.monitor.Store.SaveFinding(ctx, *i, deliveries, id+":"+kind); err != nil {
			return err
		}
		queued = len(deliveries) > 0
		return nil
	})
	return queued, err
}

func opsMonitorMetricValue(v *float64) float64 {
	if v != nil {
		return *v
	}
	return 0
}
func monitorSeverity(v string) string {
	switch v {
	case "critical":
		return "P0"
	case "warning":
		return "P1"
	default:
		return "P2"
	}
}

// 保存前先验证目标，实际发出时仍由已有安全 Dialer 再次校验解析后的 IP。
func (s *OpsService) ValidateMonitorEndpoint(ctx context.Context, kind, raw string) error {
	if raw == "" {
		return nil
	}
	if err := notify.ValidateEndpoint(kind, raw); err != nil {
		return err
	}
	u, _ := url.Parse(raw)
	blocked, err := isPrivateOrLoopbackHost(ctx, u.Hostname())
	if err != nil {
		return fmt.Errorf("通知地址暂时无法解析，请检查后重试")
	}
	if blocked {
		return fmt.Errorf("通知地址需要使用公网 HTTPS 服务")
	}
	return nil
}
