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

func TestBillingMetadataWithoutTaskSurvivesRuntimeSelection(t *testing.T) {
	ctx := context.Background()
	s, e := store.Open(filepath.Join(t.TempDir(), "state"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	now := time.Now()
	o := domain.Observation{Schema: 1, Site: "ikun2api", Source: "postgres.system", EventID: "metadata", Kind: "billing.metadata_failed", Quality: domain.Partial, OccurredAt: now, ObservedAt: now}
	if e = s.Ingest(ctx, []domain.Observation{o}, domain.Cursor{Source: o.Source, LastSuccess: now, Quality: domain.Partial}); e != nil {
		t.Fatal(e)
	}
	ev := Evaluator{Store: s, Manager: &incidents.Manager{Store: s}, Clock: &fakeClock{now}}
	if e = ev.Poll(ctx); e != nil {
		t.Fatal(e)
	}
	rows, e := s.Incidents(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if len(rows) != 1 || rows[0].Rule != "B03" {
		t.Fatalf("missing metadata finding: %+v", rows)
	}
	if len(rows[0].Explanation.Missing) == 0 {
		t.Fatal("unknown task/completion must remain explicit")
	}
}
