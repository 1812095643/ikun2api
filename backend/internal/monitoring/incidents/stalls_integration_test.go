package incidents

import (
	"context"
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/detectors"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/store"
	"path/filepath"
	"testing"
	"time"
)

func TestRepeatedStallsMergeAndRemainFeishuOnlyAfterRestart(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cfg := domain.DefaultConfig()
	cfg.Shadow = false
	cfg.Actor = "admin:1"
	cfg.RequestID = "test-stalls"
	cfg.FeishuTargets = []string{"ou_test"}
	cfg.EmailTargets = []string{"ignored@example.test"}
	cfg.Rules["L04"] = domain.RuleConfig{Enabled: true, Owner: "monitor", Version: 1}
	m := &Manager{Store: s}
	now := time.Now()
	var rows []domain.Observation
	for n := 0; n < 3; n++ {
		rows = append(rows, domain.Observation{Schema: 1, Site: cfg.Site, Source: "gateway", EventID: fmt.Sprint(n), RequestID: fmt.Sprint("r", n), Kind: "request.active", AccountID: 31, GroupID: 23, Model: "model", OccurredAt: now.Add(-time.Duration(n) * time.Second), ObservedAt: now, Quality: domain.Fresh, Metrics: map[string]float64{"stall_evidence_v1": 1, "has_output": 1, "idle_ms": 45000}})
	}
	for _, f := range detectors.Evaluate(rows[:1], now, cfg) {
		if err = m.Apply(ctx, f, cfg, now); err != nil {
			t.Fatal(err)
		}
	}
	ds, _ := s.Deliveries(ctx)
	if len(ds) != 0 {
		t.Fatal("single stall notified")
	}
	for _, f := range detectors.Evaluate(rows, now, cfg) {
		if err = m.Apply(ctx, f, cfg, now); err != nil {
			t.Fatal(err)
		}
	}
	is, _ := s.Incidents(ctx)
	if len(is) != 1 || is[0].RequestCount != 3 {
		t.Fatal("requests not merged", is)
	}
	ds, _ = s.Deliveries(ctx)
	before := len(ds)
	if before != 1 || ds[0].Payload.RequestCount != 3 {
		t.Fatal("first card must aggregate all three requests", ds)
	}
	for _, d := range ds {
		if d.Channel != "feishu" {
			t.Fatal("email routed")
		}
	}
	m = &Manager{Store: s}
	for _, f := range detectors.Evaluate(rows, now, cfg) {
		if err = m.Apply(ctx, f, cfg, now); err != nil {
			t.Fatal(err)
		}
	}
	ds, _ = s.Deliveries(ctx)
	if len(ds) != before {
		t.Fatal("restart replay duplicated notification")
	}
}
