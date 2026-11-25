package incidents

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/MACOS-DO/sub4api/internal/monitoring/detectors"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/store"
)

func TestRealRequestLifecycleReplay(t *testing.T) {
	path := os.Getenv("TEST_MONITOR_REQUEST_REPLAY")
	if path == "" {
		t.Skip("private real observations not configured")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var observations []domain.Observation
	if err = json.Unmarshal(raw, &observations); err != nil || len(observations) == 0 {
		t.Fatal("invalid replay")
	}
	ctx := context.Background()
	s, err := store.Open(filepath.Join(t.TempDir(), "isolated.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	m := &Manager{Store: s}
	cfg := domain.DefaultConfig()
	cfg.FeishuTargets = []string{"ou_test"}
	cfg.Rules["L02"] = domain.RuleConfig{Enabled: true, Owner: "monitor", Version: 1}
	for _, o := range observations {
		prior, e := s.Incidents(ctx)
		if e != nil {
			t.Fatal(e)
		}
		for _, f := range detectors.Evaluate([]domain.Observation{o}, o.OccurredAt, cfg, prior...) {
			if f.Rule == "L02" {
				if e = m.Apply(ctx, f, cfg, o.OccurredAt); e != nil {
					t.Fatal(e)
				}
			}
		}
	}
	rows, err := s.Incidents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var found *domain.Incident
	scope := observations[0].Site + ":" + observations[0].RequestID
	for n := range rows {
		if rows[n].Rule == "L02" && rows[n].Scope == scope {
			found = &rows[n]
			break
		}
	}
	if found == nil || found.Status != "resolved" || found.Sample == nil {
		t.Fatal("per-request incident did not resolve")
	}
	p := found.Sample
	if p.AccountID != 31 || p.UserID != 1 || p.KeyID != 105 || p.GroupID != 23 || p.RequestOutcome != "success" {
		t.Fatal("incorrect real identity or outcome")
	}
	if p.RequestStartedAt.IsZero() || p.FirstOutputAt.IsZero() || p.RequestEndedAt.IsZero() || p.FirstOutputAt.Sub(p.RequestStartedAt).Seconds() < 140 || p.RequestEndedAt.Sub(p.RequestStartedAt).Seconds() < 150 {
		t.Fatal("incorrect real request timing")
	}
	ds, err := s.Deliveries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	seenBefore, seenAfter := false, false
	for _, d := range ds {
		if d.IncidentID != found.ID {
			continue
		}
		if d.Channel != "feishu" {
			t.Fatal("unexpected channel")
		}
		if d.Payload.Sample != nil {
			if d.Payload.Sample.AccountID == 0 {
				seenBefore = true
			}
			if d.Payload.Sample.AccountID == 31 {
				seenAfter = true
			}
		}
	}
	if !seenBefore || !seenAfter {
		t.Fatal("did not update account after selection")
	}
	t.Log("real sanitized request replay: delayed account, card update, first output, terminal and identity verified")
}
