package incidents

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/store"
)

func TestPhasedActivationSendsOnlySelectedCurrentIncidents(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	m := &Manager{Store: s}
	now := time.Now().UTC()
	cfg := domain.DefaultConfig()
	cfg.Actor, cfg.RequestID = "admin:1", "shadow-setup"
	cfg.FeishuTargets = []string{"ou_test"}
	for _, id := range []string{"Q01", "F02"} {
		cfg.Rules[id] = domain.RuleConfig{Enabled: true, Owner: "monitor", Version: 1}
	}
	if err = m.Configure(ctx, cfg, 0); err != nil {
		t.Fatal(err)
	}
	cfg, err = s.Config(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"Q01", "F02"} {
		f := domain.Finding{Rule: id, RuleVersion: cfg.Rule(id).Version, Fingerprint: id, Scope: id, Severity: "P3", At: now, EvidenceID: id, Quality: domain.Fresh, UserID: 1}
		if err = m.Apply(ctx, f, cfg, now); err != nil {
			t.Fatal(err)
		}
		if id == "F02" { // F02 needs a second affected user before it opens a card.
			f.UserID, f.EvidenceID = 2, id+"-2"
			if err = m.Apply(ctx, f, cfg, now); err != nil {
				t.Fatal(err)
			}
		}
	}
	cfg.Shadow = false
	cfg.RequestID = "selective-live"
	r := cfg.Rules["F02"]
	r.Owner = "legacy"
	cfg.Rules["F02"] = r
	if err = m.Configure(ctx, cfg, cfg.Version); err != nil {
		t.Fatal(err)
	}
	cfg, err = s.Config(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Tick(ctx, cfg, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	deliveries, err := s.Deliveries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	shadow, pending := 0, 0
	for _, d := range deliveries {
		switch d.State {
		case "shadow":
			shadow++
		case "pending":
			pending++
			if d.Payload.Rule != "Q01" {
				t.Fatalf("unexpected rule enabled: %s", d.Payload.Rule)
			}
		default:
			t.Fatalf("unexpected state: %s", d.State)
		}
	}
	if shadow != 2 || pending != 1 {
		t.Fatalf("shadow=%d pending=%d", shadow, pending)
	}
}
