package incidents

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/detectors"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/store"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Run only against a redacted projection exported from the production view in a rolled-back transaction.
func TestSignatureRealDatabaseReplay(t *testing.T) {
	p := os.Getenv("TEST_SIGNATURE_REPLAY")
	if p == "" {
		t.Skip("real evidence export not supplied")
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		ID       int64              `json:"id"`
		At       time.Time          `json:"observed_at"`
		Evidence domain.Observation `json:"evidence"`
	}
	if err = json.Unmarshal(b, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("empty replay")
	}
	ctx := context.Background()
	s, err := store.Open(filepath.Join(t.TempDir(), "replay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	m := &Manager{Store: s}
	cfg := domain.DefaultConfig()
	cfg.Shadow = false
	cfg.FeishuTargets = []string{"ou_isolated_test"}
	cfg.Rules["F02"] = domain.RuleConfig{Enabled: true, Owner: "monitor", Version: 1}
	scopes := map[string]bool{}
	observations := []domain.Observation{}
	for _, r := range rows {
		o := r.Evidence
		o.Schema = 1
		o.Site = "replay"
		o.Source = "postgres.errors"
		o.EventID = fmt.Sprint(r.ID)
		o.OccurredAt = r.At
		o.ObservedAt = r.At
		o.Quality = domain.Partial
		if err = o.Validate(); err != nil {
			t.Fatal(err)
		}
		observations = append(observations, o)
		now := r.At.Add(31 * time.Second)
		fs := detectors.Evaluate([]domain.Observation{o}, now, cfg)
		if len(fs) != 1 || fs[0].Rule != "F02" {
			t.Fatalf("id=%d findings=%+v", r.ID, fs)
		}
		scopes[fs[0].Scope] = true
		if err = m.Apply(ctx, fs[0], cfg, now); err != nil {
			t.Fatal(err)
		}
	}
	ds, _ := s.Deliveries(ctx)
	before := len(ds)
	m = &Manager{Store: s}
	for _, o := range observations {
		now := o.OccurredAt.Add(31 * time.Second)
		for _, f := range detectors.Evaluate([]domain.Observation{o}, now, cfg) {
			if err = m.Apply(ctx, f, cfg, now); err != nil {
				t.Fatal(err)
			}
		}
	}
	ds, _ = s.Deliveries(ctx)
	if len(ds) != before {
		t.Fatal("restart duplicated deliveries")
	}
	inc, _ := s.Incidents(ctx)
	if len(inc) != len(scopes) {
		t.Fatal("scope merge failed")
	}
	total := 0
	for _, i := range inc {
		total += i.Count
	}
	if total != len(rows) {
		t.Fatal("missing or duplicate evidence", total)
	}
	t.Logf("real_errors=%d coarse_scopes=%d deliveries=%d (isolated outbox, no production sends)", len(rows), len(scopes), len(ds))
}
