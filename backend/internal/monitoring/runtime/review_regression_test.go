package runtime

import (
	"context"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/incidents"
	"github.com/MACOS-DO/sub4api/internal/monitoring/sources"
	"github.com/MACOS-DO/sub4api/internal/monitoring/store"
	"path/filepath"
	"testing"
	"time"
)

func TestReviewPaymentCompletionMustResolve(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	at := time.Now().UTC().Truncate(time.Minute)
	c := domain.DefaultConfig()
	c.Actor = "admin:1"
	c.RequestID = "setup"
	c.Rules["Q03"] = domain.RuleConfig{Enabled: true, Owner: "monitor", Version: 1}
	if err = s.SaveConfig(ctx, c, 0); err != nil {
		t.Fatal(err)
	}
	o := domain.Observation{Schema: 1, Site: c.Site, Source: "postgres.payments", EventID: "unfulfilled", TaskID: "payment:1", UserID: 1, Kind: "payment.snapshot", OccurredAt: at.Add(-time.Minute), ObservedAt: at, Quality: domain.Fresh, Metrics: map[string]float64{"paid": 1, "paid_at_ms": float64(at.Add(-10 * time.Minute).UnixMilli()), "fulfilled": 0}}
	cur := domain.Cursor{Source: o.Source, LastSuccess: at, Quality: domain.Partial}
	if err = s.Ingest(ctx, []domain.Observation{o}, cur); err != nil {
		t.Fatal(err)
	}
	clock := &fakeClock{at}
	e := &Evaluator{Store: s, Manager: &incidents.Manager{Store: s}, Clock: clock}
	if err = e.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	o.EventID = "fulfilled"
	o.OccurredAt = at.Add(time.Minute)
	o.ObservedAt = o.OccurredAt
	o.Metrics["fulfilled"] = 1
	clock.at = at.Add(61 * time.Second)
	if err = s.Ingest(ctx, []domain.Observation{o}, cur); err != nil {
		t.Fatal(err)
	}
	if err = e.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.Incidents(ctx)
	for _, i := range rows {
		if i.Rule == "Q03" && i.Status != "resolved" {
			t.Fatalf("fulfilled payment remains %s; fault=%s, healthy=%s", i.Status, i.LastFault, i.LastHealthy)
		}
	}
}

type reviewPartialSource struct {
	fail bool
	skip bool
}

func (*reviewPartialSource) Name() string { return "postgres.errors" }
func (p *reviewPartialSource) Poll(_ context.Context, previous domain.Cursor, now time.Time) (sources.Batch, error) {
	if p.skip {
		previous.Quality = domain.Partial
		return sources.Batch{Cursor: previous}, nil
	}
	if p.fail {
		return sources.Batch{}, sources.ErrSourceUnavailable
	}
	return sources.Batch{Cursor: domain.Cursor{Source: p.Name(), LastSuccess: now, Quality: domain.Partial}}, nil
}
func TestReviewPostgresReadRecoveryMustResolve(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := domain.DefaultConfig()
	clock := &fakeClock{time.Now().UTC()}
	source := &reviewPartialSource{fail: true}
	collector := &Collector{Store: s, Source: source, Clock: clock, Site: c.Site}
	e := &Evaluator{Store: s, Manager: &incidents.Manager{Store: s}, Clock: clock}
	_ = collector.Poll(ctx)
	if err = e.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	source.fail = false
	clock.at = clock.at.Add(time.Minute)
	if err = collector.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	if err = e.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.Incidents(ctx)
	for _, i := range rows {
		if i.Rule == "M01" && i.Scope == source.Name() && i.Status != "resolved" {
			t.Fatalf("successfully recovered PostgreSQL source remains %s", i.Status)
		}
	}
}

func TestSkippedPartialPollDoesNotInventRecovery(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	clock := &fakeClock{time.Now()}
	source := &reviewPartialSource{fail: true}
	c := &Collector{Store: s, Source: source, Clock: clock, Site: "test"}
	_ = c.Poll(ctx)
	source.fail = false
	source.skip = true
	clock.at = clock.at.Add(time.Minute)
	if err = c.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Observations(ctx, clock.at.Add(-time.Hour), 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range rows {
		if o.Kind == "source.recovered" {
			t.Fatal("skipped source query claimed recovery")
		}
	}
}
