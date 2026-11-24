package ownership

import (
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"path/filepath"
	"testing"
)

func TestClaimsAreLimitedToExplicitRuleAndScope(t *testing.T) {
	cfg := domain.DefaultConfig()
	cfg.Shadow = false
	cfg.Rules["U01"] = domain.RuleConfig{Enabled: true, Owner: "monitor", Version: 1}
	group := int64(19)
	cfg.LegacyHandoffs = []domain.LegacyHandoff{{LegacyRuleID: 7, GroupID: &group, Platform: "openai", MonitorRules: []string{"U01"}}}
	path := filepath.Join(t.TempDir(), "ownership.json")
	if err := Write(path, Project(cfg)); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		id, group int64
		platform  string
		want      bool
	}{{7, 19, "openai", true}, {8, 19, "openai", false}, {7, 20, "openai", false}, {7, 19, "anthropic", false}} {
		got, e := Suppressed(path, c.id, c.group, c.platform)
		if e != nil || got != c.want {
			t.Fatal(c, got, e)
		}
	}
	cfg.Shadow = true
	if err := Write(path, Project(cfg)); err != nil {
		t.Fatal(err)
	}
	got, e := Suppressed(path, 7, 19, "openai")
	if e != nil || got {
		t.Fatal("shadow suppressed old notifications", got, e)
	}
}

func TestBroadHandoffCannotHideLegacyOwnedExceptions(t *testing.T) {
	c := domain.DefaultConfig()
	c.Shadow = false
	c.Rules["U01"] = domain.RuleConfig{Enabled: true, Owner: "monitor", Version: 1}
	group := int64(19)
	c.LegacyHandoffs = []domain.LegacyHandoff{{LegacyRuleID: 7, MonitorRules: []string{"U01"}}}
	c.OwnerOverrides = []domain.OwnerOverride{{Rule: "U01", GroupID: &group, Owner: "legacy"}}
	if len(Project(c).Claims) != 0 {
		t.Fatal("global claim hides legacy-owned group")
	}
	c.LegacyHandoffs[0].GroupID = &group
	c.OwnerOverrides[0].Model = "slow-model"
	if len(Project(c).Claims) != 0 {
		t.Fatal("group claim hides legacy-owned model")
	}
	other := int64(20)
	c.LegacyHandoffs[0].GroupID = &other
	if len(Project(c).Claims) != 1 {
		t.Fatal("unrelated group should remain transferable")
	}
}
