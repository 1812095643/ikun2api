package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCurrentNotificationConstraint(t *testing.T) {
	now := time.Now()
	cfg := DefaultConfig()
	cfg.Shadow = false
	cfg.Rules["U01"] = RuleConfig{Enabled: true, Owner: "monitor", Version: 1}
	i := Incident{Rule: "U01", Severity: "P1"}
	require.Equal(t, "no_policy", CurrentNotificationConstraint(i, cfg, now))
	cfg.Policies = []NotificationPolicy{{Enabled: true, TargetIDs: []string{"target"}}}
	require.Equal(t, "no_target", CurrentNotificationConstraint(i, cfg, now))
	cfg.Targets = []Target{{ID: "target", Enabled: true}}
	require.Equal(t, "criteria", CurrentNotificationConstraint(i, cfg, now))
	i.SilencedUntil = now.Add(time.Hour)
	require.Equal(t, "silenced", CurrentNotificationConstraint(i, cfg, now))
	cfg.Shadow = true
	require.Equal(t, "record_only", CurrentNotificationConstraint(i, cfg, now))
	i.Rule = "L02"
	require.Equal(t, "single_request", CurrentNotificationConstraint(i, cfg, now))
}
