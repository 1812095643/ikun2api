package incidents

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/store"
)

func servedObs(id string, kind, outcome string, group int64, model string, at time.Time) domain.Observation {
	return domain.Observation{Schema: 1, Site: "t", Source: "gateway", EventID: id, Kind: kind, Outcome: outcome, GroupID: group, Model: model, OccurredAt: at, ObservedAt: at, Quality: domain.Fresh}
}

// history stores observations reaching back `span` and bootstraps the served index.
func history(t *testing.T, s *store.Store, now time.Time, span time.Duration, extra ...domain.Observation) {
	t.Helper()
	ctx := context.Background()
	rows := append([]domain.Observation{servedObs("oldest", "request.started", "", 1, "x", now.Add(-span))}, extra...)
	if err := s.Ingest(ctx, rows, domain.Cursor{Source: "gateway"}); err != nil {
		t.Fatal(err)
	}
	if err := UpdateServed(ctx, s, nil, now); err != nil {
		t.Fatal(err)
	}
}

func s01(group int64, model, ev string, at time.Time) domain.Finding {
	return domain.Finding{Rule: "S01", RuleVersion: 1, Fingerprint: fmt.Sprintf("s01:%d:%s", group, model), Scope: fmt.Sprintf("group:%d/model:%s", group, model), GroupID: group, Model: model, Severity: "P1", Quality: domain.Fresh, At: at, EvidenceID: ev}
}

func s01Setup(t *testing.T) (*Manager, *store.Store, domain.Config) {
	m, s, cfg := noiseSetup(t)
	cfg.Rules["S01"] = domain.RuleConfig{Enabled: true, Owner: "monitor", Version: 1}
	return m, s, cfg
}

func TestS01SuddenOutageOnServedGroupAlertsImmediately(t *testing.T) {
	ctx := context.Background()
	m, s, cfg := s01Setup(t)
	now := time.Now().UTC()
	// Group 27 served claude-a two days ago (billed usage counts as served too).
	history(t, s, now, 3*24*time.Hour,
		servedObs("ok", "request.finished", "success", 27, "claude-a", now.Add(-48*time.Hour)),
		servedObs("usage", "usage.recorded", "", 27, "claude-b", now.Add(-24*time.Hour)))
	for n, model := range []string{"claude-a", "claude-b"} {
		if err := m.Apply(ctx, s01(27, model, fmt.Sprint(n), now), cfg, now); err != nil {
			t.Fatal(err)
		}
	}
	ds := liveDeliveries(t, s)
	if len(ds) != 1 || ds[0].Kind != "firing" || ds[0].Payload.Scope != "group:27" {
		t.Fatalf("served group must page once, merged per group: %+v", kinds(ds))
	}
	r, _ := s.Incident(ctx, "rollup:group:27:S01")
	if r == nil || len(r.Members) != 2 {
		t.Fatalf("group card must list both models: %+v", r)
	}
}

func TestS01NeverServedGoesToDailyDigestOnce(t *testing.T) {
	ctx := context.Background()
	m, s, cfg := s01Setup(t)
	morning := time.Date(2026, 11, 25, 8, 0, 0, 0, beijing)
	history(t, s, morning, 3*24*time.Hour)
	for n := 0; n < 3; n++ {
		at := morning.Add(time.Duration(n) * time.Minute)
		if err := m.Apply(ctx, s01(23, "claude-3-5-haiku", fmt.Sprint("h", n), at), cfg, at); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.Apply(ctx, s01(23, "sonnet", "s0", morning), cfg, morning); err != nil {
		t.Fatal(err)
	}
	if ds := liveDeliveries(t, s); len(ds) != 0 {
		t.Fatalf("never-served must not page: %v", kinds(ds))
	}
	if err := m.Tick(ctx, cfg, morning.Add(59*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if ds := liveDeliveries(t, s); len(ds) != 0 {
		t.Fatal("no digest before 09:00")
	}
	for _, at := range []time.Time{morning.Add(time.Hour), morning.Add(2 * time.Hour), morning.Add(10 * time.Hour)} {
		if err := m.Tick(ctx, cfg, at); err != nil {
			t.Fatal(err)
		}
	}
	ds := liveDeliveries(t, s)
	if len(ds) != 1 || ds[0].Payload.Scope != "digest" || ds[0].Payload.Count != 4 {
		t.Fatalf("exactly one digest with 4 hits: %+v", ds)
	}
	facts := strings.Join(ds[0].Payload.Explanation.Facts, "\n")
	if !strings.Contains(facts, "claude-3-5-haiku · 被请求 3 次") || !strings.Contains(facts, "sonnet · 被请求 1 次") {
		t.Fatal(facts)
	}
	// Next day with no new hits: no digest.
	if err := m.Tick(ctx, cfg, morning.Add(25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if ds = liveDeliveries(t, s); len(ds) != 1 {
		t.Fatal("empty day must not send a digest")
	}
	// New hits, then the following morning: second digest counts only the new hits.
	at := morning.Add(30 * time.Hour)
	if err := m.Apply(ctx, s01(23, "sonnet", "s1", at), cfg, at); err != nil {
		t.Fatal(err)
	}
	if err := m.Tick(ctx, cfg, morning.Add(49*time.Hour)); err != nil {
		t.Fatal(err)
	}
	ds = liveDeliveries(t, s)
	if len(ds) != 2 || ds[1].Payload.Count != 1 {
		t.Fatalf("second digest counts only new hits: %+v", ds)
	}
}

func TestS01DigestLongListIsTruncated(t *testing.T) {
	ctx := context.Background()
	m, s, cfg := s01Setup(t)
	morning := time.Date(2026, 11, 25, 8, 0, 0, 0, beijing)
	history(t, s, morning, 3*24*time.Hour)
	for n := 0; n < 9; n++ {
		if err := m.Apply(ctx, s01(23, fmt.Sprint("m", n), fmt.Sprint(n), morning), cfg, morning); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.Tick(ctx, cfg, morning.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	ds := liveDeliveries(t, s)
	if len(ds) != 1 {
		t.Fatal(len(ds))
	}
	f := ds[0].Payload.Explanation.Facts
	if len(f) != digestFacts || !strings.Contains(f[0], "共 9 个") || !strings.Contains(f[len(f)-1], "另有 5 个") {
		t.Fatalf("%q", f)
	}
}

func TestS01ShortHistoryStillAlerts(t *testing.T) {
	ctx := context.Background()
	m, s, cfg := s01Setup(t)
	now := time.Now().UTC()
	history(t, s, now, time.Hour) // a fresh install: one hour of evidence
	if err := m.Apply(ctx, s01(23, "claude-new", "1", now), cfg, now); err != nil {
		t.Fatal(err)
	}
	if ds := liveDeliveries(t, s); len(ds) != 1 {
		t.Fatalf("too little history to call it never-served: %v", kinds(ds))
	}
}

func TestS01BecomesServedThenOutageAlerts(t *testing.T) {
	ctx := context.Background()
	m, s, cfg := s01Setup(t)
	now := time.Now().UTC()
	history(t, s, now, 3*24*time.Hour)
	if err := m.Apply(ctx, s01(23, "claude-c", "1", now), cfg, now); err != nil {
		t.Fatal(err)
	}
	if ds := liveDeliveries(t, s); len(ds) != 0 {
		t.Fatal("first: never served")
	}
	ok := now.Add(time.Hour)
	if err := UpdateServed(ctx, s, []domain.Observation{servedObs("ok", "request.finished", "success", 23, "claude-c", ok)}, ok); err != nil {
		t.Fatal(err)
	}
	at := now.Add(2 * time.Hour)
	if err := m.Apply(ctx, s01(23, "claude-c", "2", at), cfg, at); err != nil {
		t.Fatal(err)
	}
	ds := liveDeliveries(t, s)
	if len(ds) != 1 || ds[0].Payload.Scope != "group:23" {
		t.Fatalf("served group losing candidates must page: %v", kinds(ds))
	}
	i, _ := s.Incident(ctx, s01(23, "claude-c", "", at).Fingerprint+":v1")
	if i == nil || i.DigestOnly {
		t.Fatal("incident must leave digest mode", i)
	}
}

func TestServedIndexPrunesAfterSevenDays(t *testing.T) {
	ctx := context.Background()
	_, s, _ := s01Setup(t)
	now := time.Now().UTC()
	history(t, s, now, 3*24*time.Hour, servedObs("ok", "request.finished", "success", 5, "m", now.Add(-time.Hour)))
	later := now.Add(8 * 24 * time.Hour)
	if err := UpdateServed(ctx, s, nil, later); err != nil {
		t.Fatal(err)
	}
	var idx servedIndex
	if err := s.State(ctx, servedStateKey, &idx); err != nil {
		t.Fatal(err)
	}
	if _, ok := idx.Last["5/m"]; ok {
		t.Fatal("served entry older than 7 days must be pruned")
	}
}
