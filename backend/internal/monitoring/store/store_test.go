package store

import (
	"context"
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"path/filepath"
	"testing"
	"time"
)

func TestRestartDedupAndCursorAtomicity(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Open(path); err == nil {
		t.Fatal("second writer accepted")
	}
	now := time.Now().UTC()
	o := domain.Observation{Schema: 1, Site: "test", Source: "gateway", EventID: "one", Kind: "attempt.failed", OccurredAt: now, ObservedAt: now, Quality: domain.Fresh, UpstreamStatus: 429}
	if err = s.Ingest(ctx, []domain.Observation{o}, domain.Cursor{Source: "gateway", Watermark: "1", LastSuccess: now, Quality: domain.Fresh}); err != nil {
		t.Fatal(err)
	}
	if err = s.Backup(ctx, filepath.Join(filepath.Dir(path), "backup.db")); err != nil {
		t.Fatal(err)
	}
	backup, err := Open(filepath.Join(filepath.Dir(path), "backup.db"))
	if err != nil {
		t.Fatal(err)
	}
	backed, err := backup.Observations(ctx, now.Add(-time.Minute), 100)
	if err != nil || len(backed) != 1 {
		t.Fatal("WAL snapshot lost committed evidence", err)
	}
	backup.Close()
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Ingest(ctx, []domain.Observation{o}, domain.Cursor{Source: "gateway", Watermark: "1", LastSuccess: now, Quality: domain.Fresh}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Observations(ctx, now.Add(-time.Minute), 100)
	if err != nil || len(rows) != 1 {
		t.Fatalf("dedup: %v %d", err, len(rows))
	}
	c, err := s.Cursor(ctx, "gateway")
	if err != nil || c.Watermark != "1" {
		t.Fatalf("cursor: %+v %v", c, err)
	}
	bad := o
	bad.EventID = "two"
	bad.Schema = 99
	if err = s.Ingest(ctx, []domain.Observation{bad}, domain.Cursor{Source: "gateway", Watermark: "2"}); err == nil {
		t.Fatal("incompatible schema accepted")
	}
	c, _ = s.Cursor(ctx, "gateway")
	if c.Watermark != "1" {
		t.Fatal("cursor advanced on failed ingest")
	}
}

func TestIndependentConfigurationAndLease(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cfg, err := s.Config(ctx)
	if err != nil || !cfg.Shadow {
		t.Fatalf("safe defaults: %+v %v", cfg, err)
	}
	cfg.Actor = "admin:1"
	cfg.RequestID = "mutation-1"
	if err = s.SaveConfig(ctx, cfg, 0); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveConfig(ctx, cfg, 0); err != nil {
		t.Fatal("idempotent retry", err)
	}
	cfg.RequestID = "mutation-2"
	if err = s.SaveConfig(ctx, cfg, 0); err != ErrConflict {
		t.Fatal("missing CAS conflict", err)
	}
	now := time.Now().UTC()
	inc := domain.Incident{ID: "i1", Fingerprint: "U01:account1", Rule: "U01", Revision: 1, Status: "firing", Severity: "P1", FirstSeen: now, LastSeen: now}
	d := domain.Delivery{ID: "d1", IncidentID: "i1", Revision: 1, Channel: "feishu", Target: "target", Kind: "firing", NextAttempt: now}
	if err = s.SaveIncident(ctx, inc, []domain.Delivery{d}); err != nil {
		t.Fatal(err)
	}
	a, err := s.Lease(ctx, now, time.Minute)
	if err != nil || a == nil {
		t.Fatal(err)
	}
	if b, _ := s.Lease(ctx, now, time.Minute); b != nil {
		t.Fatal("double lease")
	}
	if b, _ := s.Lease(ctx, now.Add(2*time.Minute), time.Minute); b == nil {
		t.Fatal("crashed lease not recovered")
	}
}

func TestNewestWindowAndDurableTaskFacts(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	rows := []domain.Observation{}
	for n := 0; n < 4; n++ {
		rows = append(rows, domain.Observation{Schema: 1, Site: "test", Source: "gateway", EventID: fmt.Sprint(n), Kind: "media.completed", TaskID: "video", OccurredAt: now.Add(time.Duration(n) * time.Second), ObservedAt: now, Quality: domain.Fresh, Metrics: map[string]float64{"deliverable": 1}})
	}
	if err = s.Ingest(ctx, rows, domain.Cursor{Source: "gateway"}); err != nil {
		t.Fatal(err)
	}
	got, truncated, err := s.Window(ctx, now.Add(-time.Minute), 2)
	if err != nil || !truncated || len(got) != 2 || got[0].EventID != "2" {
		t.Fatalf("newest window: %+v %v %v", got, truncated, err)
	}
	if err = s.Retain(ctx, now.Add(8*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	facts, err := s.TaskFacts(ctx)
	if err != nil || len(facts) != 1 || facts[0].EventID != "0" {
		t.Fatalf("first completion lost: %+v %v", facts, err)
	}
}

func TestConfigThresholdRevisionAndShadowHandoff(t *testing.T) {
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	now := time.Now()
	cfg := domain.DefaultConfig()
	cfg.Actor = "admin:1"
	cfg.RequestID = "initial"
	cfg.Rules["L02"] = domain.RuleConfig{Enabled: true, Owner: "monitor", Version: 1}
	if e = s.SaveConfig(ctx, cfg, 0); e != nil {
		t.Fatal(e)
	}
	i := domain.Incident{ID: "i", Fingerprint: "fp:v1", Rule: "L02", RuleVersion: 1, Revision: 1, Status: "firing", LastSeen: now, LastNotified: now, NotifiedCount: 1, Count: 1}
	if e = s.SaveIncident(ctx, i, nil); e != nil {
		t.Fatal(e)
	}
	cfg, _ = s.Config(ctx)
	cfg.RequestID = "activate"
	cfg.Shadow = false
	if e = s.SaveConfig(ctx, cfg, cfg.Version); e != nil {
		t.Fatal(e)
	}
	got, _ := s.Incident(ctx, "i")
	if !got.LastNotified.IsZero() {
		t.Fatal("shadow delivery prevents first live alert")
	}
	cfg, _ = s.Config(ctx)
	cfg.RequestID = "profile"
	p := cfg.Profiles["default"]
	p.FirstWarnSeconds = 35
	cfg.Profiles["default"] = p
	if e = s.SaveConfig(ctx, cfg, cfg.Version); e != nil {
		t.Fatal(e)
	}
	updated, _ := s.Config(ctx)
	if updated.Rule("L02").Version != 2 {
		t.Fatal("rule semantics changed without version", updated)
	}
	got, _ = s.Incident(ctx, "i")
	if got.Status != "superseded" {
		t.Fatal("old rule revision still active", got)
	}
}

func TestBacklogRecoveryIsQueueEvidenceNotDeliveryReceipt(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	i := domain.Incident{ID: "queue-test", Fingerprint: "queue-test", Rule: "U01", Revision: 1}
	d := domain.Delivery{ID: "queue-delivery", IncidentID: i.ID, Revision: 1, Channel: "feishu", Target: "oc_test", State: "pending", NextAttempt: now.Add(-2 * time.Minute), Payload: i}
	if err = s.SaveIncident(ctx, i, []domain.Delivery{d}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Backlog(ctx, now)
	if err != nil || len(rows) != 1 || rows[0].N("oldest_pending_seconds") < 60 {
		t.Fatal(rows, err)
	}
	leased, err := s.Lease(ctx, now, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	leased.State = "dead"
	if err = s.CompleteDelivery(ctx, *leased); err != nil {
		t.Fatal(err)
	}
	rows, err = s.Backlog(ctx, now)
	if err != nil || len(rows) != 1 || rows[0].N("queue_cleared") != 1 || rows[0].N("healthy") != 0 {
		t.Fatal("cleared queue must not claim receipt", rows, err)
	}
}

func TestRetentionRemovesOldClosedEvidenceButKeepsPendingDelivery(t *testing.T) {
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	now := time.Now()
	old := now.Add(-91 * 24 * time.Hour)
	i := domain.Incident{ID: "closed", Fingerprint: "closed", Rule: "U01", Revision: 1, Status: "resolved", LastSeen: old}
	if e = s.SaveFinding(ctx, i, nil, "proof"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec("UPDATE incidents SET updated=?", old.UnixMilli()); e != nil {
		t.Fatal(e)
	}
	pending := domain.Incident{ID: "pending-closed", Fingerprint: "pending-closed", Rule: "U01", Revision: 1, Status: "resolved", LastSeen: old}
	if e = s.SaveFinding(ctx, pending, []domain.Delivery{{ID: "pending", IncidentID: pending.ID, Revision: 1, Channel: "feishu", Target: "oc_test", State: "pending", NextAttempt: old, Payload: pending}}, "pending-proof"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec("UPDATE incidents SET updated=?", old.UnixMilli()); e != nil {
		t.Fatal(e)
	}
	if e = s.Retain(ctx, now); e != nil {
		t.Fatal(e)
	}
	kept, e := s.Incident(ctx, "pending-closed")
	if e != nil || kept == nil {
		t.Fatal("pending lost", e)
	}
	got, e := s.Incident(ctx, "closed")
	if e != nil || got != nil {
		t.Fatal("old incident not removed", got, e)
	}
	var count int
	s.db.QueryRow("SELECT count(*) FROM incident_evidence").Scan(&count)
	if count != 1 {
		t.Fatal("orphan evidence or pending proof lost", count)
	}
}

func TestDiskBudgetFailureKeepsCursorAtomic(t *testing.T) {
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	var pages int
	if e = s.db.QueryRow("PRAGMA page_count").Scan(&pages); e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec(fmt.Sprintf("PRAGMA max_page_count=%d", pages+1)); e != nil {
		t.Fatal(e)
	}
	rows := []domain.Observation{}
	now := time.Now()
	for n := 0; n < 1000; n++ {
		rows = append(rows, domain.Observation{Schema: 1, Site: "test", Source: "gateway", EventID: fmt.Sprint(n), Kind: "request.finished", Quality: domain.Fresh, OccurredAt: now})
	}
	if e = s.Ingest(ctx, rows, domain.Cursor{Source: "gateway", Watermark: "1000"}); e == nil {
		t.Fatal("capacity failure not injected")
	}
	c, e := s.Cursor(ctx, "gateway")
	if e != nil || c.Watermark != "" {
		t.Fatal("cursor advanced on storage failure", c, e)
	}
	if percent, e := s.UsagePercent(ctx); e != nil || percent < 80 {
		t.Fatal("budget warning absent", percent, e)
	}
}

func TestRetainKeepsThreeDaysAndOpenIncidents(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	old, recent := now.Add(-4*24*time.Hour), now.Add(-2*24*time.Hour)
	obs := []domain.Observation{
		{Schema: 1, Site: "t", Source: "gateway", EventID: "old", Kind: "request.started", OccurredAt: old, ObservedAt: old, Quality: domain.Fresh},
		{Schema: 1, Site: "t", Source: "gateway", EventID: "recent", Kind: "request.started", OccurredAt: recent, ObservedAt: recent, Quality: domain.Fresh},
	}
	if err = s.Ingest(ctx, obs, domain.Cursor{Source: "gateway"}); err != nil {
		t.Fatal(err)
	}
	for _, x := range []struct {
		id, status string
		at         time.Time
	}{{"closed-old", "resolved", old}, {"open-old", "firing", old}, {"closed-recent", "resolved", recent}} {
		i := domain.Incident{ID: x.id, Fingerprint: x.id, Rule: "U01", Revision: 1, Status: x.status, LastSeen: x.at}
		d := domain.Delivery{ID: x.id, IncidentID: x.id, Revision: 1, Channel: "feishu", Target: "oc_t", Kind: "firing", State: "accepted", NextAttempt: x.at}
		if err = s.SaveFinding(ctx, i, []domain.Delivery{d}, "ev-"+x.id); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.Retain(ctx, now); err != nil {
		t.Fatal(err)
	}
	count := func(q string) (n int) {
		if err := s.db.QueryRow(q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count("SELECT count(*) FROM observations"); n != 1 {
		t.Fatalf("observations kept %d", n)
	}
	for id, want := range map[string]bool{"closed-old": false, "open-old": true, "closed-recent": true} {
		got, _ := s.Incident(ctx, id)
		if (got != nil) != want {
			t.Fatalf("incident %s kept=%v want %v", id, got != nil, want)
		}
	}
	if n := count("SELECT count(*) FROM notification_outbox"); n != 1 {
		t.Fatalf("finished deliveries kept %d, want only the recent one", n)
	}
	if n := count("SELECT count(*) FROM incident_transitions"); n != 1 {
		t.Fatalf("transitions kept %d", n)
	}
	if n := count("SELECT count(*) FROM incident_evidence"); n != 1 {
		t.Fatalf("evidence kept %d", n)
	}
}
