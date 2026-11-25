package runtime

import (
	"context"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/sources"
	"github.com/MACOS-DO/sub4api/internal/monitoring/store"
	"path/filepath"
	"testing"
	"time"
)

type fakeClock struct{ at time.Time }

func (f *fakeClock) Now() time.Time { return f.at }

type fakeSource struct{ err error }

func (f *fakeSource) Name() string { return "test" }
func (f *fakeSource) Poll(_ context.Context, _ domain.Cursor, now time.Time) (sources.Batch, error) {
	return sources.Batch{Cursor: domain.Cursor{Source: "test", Watermark: "next", LastSuccess: now, Quality: domain.Fresh}}, f.err
}
func TestColdStartWithoutBusinessDependencies(t *testing.T) {
	ctx := context.Background()
	s, e := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	clock := &fakeClock{time.Now()}
	source := &fakeSource{sources.ErrSourceUnavailable}
	c := Collector{Store: s, Source: source, Clock: clock, Site: "test"}
	if e = c.Poll(ctx); e == nil {
		t.Fatal("source failure hidden")
	}
	cur, _ := s.Cursor(ctx, "test")
	if cur.Quality != domain.Stale || !cur.LastSuccess.IsZero() {
		t.Fatal("failure reported healthy", cur)
	}
	cfg, e := s.Config(ctx)
	if e != nil || !cfg.Shadow {
		t.Fatal("local config unavailable", e)
	}
	source.err = nil
	clock.at = clock.at.Add(time.Minute)
	if e = c.Poll(ctx); e != nil {
		t.Fatal(e)
	}
	rows, e := s.Observations(ctx, time.Time{}, 100)
	if e != nil || len(rows) != 2 {
		t.Fatal("source state transitions missing", e, len(rows))
	}
}
