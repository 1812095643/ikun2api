package runtime

import (
	"context"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/incidents"
	"github.com/MACOS-DO/sub4api/internal/monitoring/store"
	"path/filepath"
	"testing"
	"time"
)

func TestEvidenceToOutboxAndRestart(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	cfg := domain.DefaultConfig()
	cfg.Actor = "admin"
	cfg.RequestID = "cfg"
	cfg.Rules["U01"] = domain.RuleConfig{Enabled: true, Owner: "monitor", Version: 1}
	cfg.FeishuTargets = []string{"oc_mock"}
	if err = s.SaveConfig(ctx, cfg, 0); err != nil {
		t.Fatal(err)
	}
	observation := domain.Observation{Schema: 1, Site: cfg.Site, Source: "gateway", EventID: "error", Kind: "attempt.failed", RequestID: "request", AccountID: 1, UpstreamStatus: 429, OccurredAt: now, ObservedAt: now, Quality: domain.Fresh}
	if err = s.Ingest(ctx, []domain.Observation{observation}, domain.Cursor{Source: "gateway", LastSuccess: now, Quality: domain.Fresh}); err != nil {
		t.Fatal(err)
	}
	e := &Evaluator{Store: s, Manager: &incidents.Manager{Store: s}, Clock: &fakeClock{now}}
	if err = e.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	if err = e.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	// A single U01 is archived on its own incident and the account rollup, with no card.
	rows, _ := s.Incidents(ctx)
	members, rollups := 0, 0
	for _, r := range rows {
		if r.Rollup {
			rollups++
		} else if r.RequestCount == 1 && r.Count == 1 && r.ParentID != "" {
			members++
		}
	}
	if len(rows) != 2 || members != 1 || rollups != 1 {
		t.Fatal(rows)
	}
	ds, _ := s.Deliveries(ctx)
	if len(ds) != 0 {
		t.Fatal("below threshold must not queue a card", ds)
	}
}
