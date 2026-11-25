package incidents

import (
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/stretchr/testify/require"
)

func TestNotificationRoutingDeduplicatesRecipientsAndHonorsControls(t *testing.T) {
	now := time.Now()
	cfg := domain.DefaultConfig()
	cfg.Shadow = false
	cfg.Rules["S01"] = domain.RuleConfig{Enabled: true, Owner: "monitor", Version: 1}
	cfg.Targets = []domain.Target{{ID: "telegram", ChannelID: "bot", ChannelKind: "telegram"}, {ID: "mail", ChannelID: "smtp", ChannelKind: "email"}}
	p := domain.NotificationPolicy{ID: "a", Enabled: true, Platforms: []string{"openai"}, GroupIDs: []int64{2}, AccountIDs: []int64{3}, Models: []string{"gpt"}, TargetIDs: []string{"telegram"}, FallbackTargetIDs: []string{"mail"}, RepeatMinutes: 10}
	cfg.Policies = []domain.NotificationPolicy{p, p}
	i := domain.Incident{ID: "incident", Revision: 2, Rule: "S01", Platform: "openai", GroupID: 2, Model: "gpt", Sample: &domain.IncidentSample{AccountID: 3}, Severity: "P1", Status: "firing", LastNotified: now.Add(-11 * time.Minute)}
	ds := route(i, "firing", cfg, now, false)
	require.Len(t, ds, 1)
	require.Equal(t, []string{"mail"}, ds[0].FallbackTargetIDs)
	i.SilencedUntil = now.Add(time.Hour)
	require.Empty(t, route(i, "escalated", cfg, now, false))
	i.SilencedUntil = time.Time{}
	require.True(t, reminderDue(i, cfg, now))
	i.Acknowledged = true
	require.False(t, reminderDue(i, cfg, now))
	require.Empty(t, route(i, "reminder", cfg, now, false))
	i.Platform = "gemini"
	require.Empty(t, route(i, "firing", cfg, now, false))
	i.Platform = "openai"
	cfg.Shadow = true
	i.Acknowledged = false
	require.Equal(t, "shadow", route(i, "firing", cfg, now, false)[0].State)
	cfg.Shadow = false
	cfg.Policies[0].TargetIDs = []string{"telegram", "mail"}
	cfg.Policies = cfg.Policies[:1]
	require.Len(t, route(i, "update", cfg, now, false), 1, "邮件不跟随普通计数变化刷屏")
	require.Len(t, route(i, "resolved", cfg, now, false), 2)
}

func TestMissingSourcesCannotProveRecovery(t *testing.T) {
	now := time.Now()
	require.False(t, SourcesFresh([]domain.Cursor{{Source: "gateway", Quality: domain.Fresh}}, now))
	require.False(t, SourcesFresh([]domain.Cursor{{Source: "gateway", Quality: domain.Partial, LastSuccess: now}}, now))
	require.True(t, SourcesFresh([]domain.Cursor{{Source: "gateway", Quality: domain.Fresh, LastSuccess: now}}, now))
	cfg := domain.DefaultConfig()
	cfg.Shadow = false
	cfg.Rules["U01"] = domain.RuleConfig{Enabled: true, Owner: "monitor", Version: 1}
	cfg.LegacyHandoffs = []domain.LegacyHandoff{{LegacyRuleID: 7, MonitorRules: []string{"U01"}}}
	cfg.Targets = []domain.Target{{ID: "mail", Enabled: true}}
	cfg.Policies = []domain.NotificationPolicy{{ID: "legacy", Enabled: true, TargetIDs: []string{"mail"}}}
	require.True(t, domain.LegacyTakenOver(cfg, 7, "openai", 2, "gpt"))
	cfg.Shadow = true
	require.False(t, domain.LegacyTakenOver(cfg, 7, "openai", 2, "gpt"))
}
