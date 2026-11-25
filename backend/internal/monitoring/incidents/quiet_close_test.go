package incidents

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
)

// openCarded fires a gated incident past its threshold so it owns a card.
func openCarded(t *testing.T, m *Manager, cfg domain.Config, rule, fp string, start time.Time) domain.Finding {
	t.Helper()
	f := domain.Finding{Rule: rule, RuleVersion: 1, Fingerprint: fp, Scope: "group:1/model:m", Severity: "P1", Quality: domain.Fresh}
	for n := 0; n < 5; n++ {
		f.At, f.EvidenceID, f.RequestID, f.UserID = start.Add(time.Duration(n)*time.Second), fmt.Sprint(fp, n), fmt.Sprint("r", n), int64(n+1)
		if err := m.Apply(context.Background(), f, cfg, f.At); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func incidentByFP(t *testing.T, m *Manager, fp string) domain.Incident {
	t.Helper()
	i, err := m.Store.Incident(context.Background(), fp+":v1")
	if err != nil || i == nil {
		t.Fatalf("incident %s: %v", fp, err)
	}
	return *i
}

func TestQuietCloseEditsRecentCardWithoutEmailOrResolvedPush(t *testing.T) {
	ctx := context.Background()
	m, s, cfg := noiseSetup(t)
	cfg.EmailTargets = []string{"ops@example.test"}
	start := time.Now().UTC().Add(-3 * time.Hour)
	openCarded(t, m, cfg, "U02", "q1", start)
	before := len(liveDeliveries(t, s))
	if before == 0 {
		t.Fatal("expected the incident to open a card")
	}

	if n, err := m.CloseQuiet(ctx, cfg, start.Add(domain.QuietRequest-time.Minute)); err != nil || n != 0 {
		t.Fatalf("closed %d before the quiet window, err=%v", n, err)
	}
	now := start.Add(domain.QuietRequest + time.Minute)
	if n, err := m.CloseQuiet(ctx, cfg, now); err != nil || n != 1 {
		t.Fatalf("closed %d, err=%v", n, err)
	}
	i := incidentByFP(t, m, "q1")
	if i.Status != "resolved" || i.ClosedBy != "quiet" {
		t.Fatalf("status=%s closed_by=%s", i.Status, i.ClosedBy)
	}
	ds := liveDeliveries(t, s)[before:]
	if len(ds) != 1 || ds[0].Kind != "closed" || ds[0].Channel != "feishu" || ds[0].ForceNew {
		t.Fatalf("want one feishu card edit, got %+v", ds)
	}

	// Tick's delayed "已恢复" announcement must not fire for a quiet close.
	if err := m.Tick(ctx, cfg, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := len(liveDeliveries(t, s)); got != before+1 {
		t.Fatalf("Tick added %d deliveries after a quiet close", got-before-1)
	}
}

func TestQuietCloseLeavesOldCardsAndMoneyRulesAlone(t *testing.T) {
	ctx := context.Background()
	m, s, cfg := noiseSetup(t)
	cfg.Rules["Q03"] = domain.RuleConfig{Enabled: true, Owner: "monitor", Version: 1}
	start := time.Now().UTC().Add(-3 * 24 * time.Hour)
	openCarded(t, m, cfg, "U01", "old", start)
	order := domain.Finding{Rule: "Q03", RuleVersion: 1, Fingerprint: "q03", Scope: "task:pay-1", Severity: "P1", Quality: domain.Fresh, At: start, EvidenceID: "q03"}
	if err := m.Apply(ctx, order, cfg, start); err != nil {
		t.Fatal(err)
	}
	before := len(liveDeliveries(t, s))

	if n, err := m.CloseQuiet(ctx, cfg, time.Now().UTC()); err != nil || n != 1 {
		t.Fatalf("closed %d, err=%v", n, err)
	}
	if i := incidentByFP(t, m, "old"); i.Status != "resolved" {
		t.Fatalf("old request incident status=%s", i.Status)
	}
	if i := incidentByFP(t, m, "q03"); i.Status == "resolved" {
		t.Fatal("an unfulfilled paid order needs explicit recovery evidence")
	}
	if got := len(liveDeliveries(t, s)); got != before {
		t.Fatalf("a card older than a day must not be edited, got %d new deliveries", got-before)
	}
}

func TestStateIncidentClosesAfterADayWithoutNews(t *testing.T) {
	ctx := context.Background()
	m, _, cfg := noiseSetup(t)
	cfg.Rules["S03"] = domain.RuleConfig{Enabled: true, Owner: "monitor", Version: 1}
	start := time.Now().UTC().Add(-48 * time.Hour)
	quarantine := domain.Finding{Rule: "S03", RuleVersion: 1, Fingerprint: "s03", Scope: "account:9", Severity: "P1", Quality: domain.Fresh, At: start, EvidenceID: "s03"}
	if err := m.Apply(ctx, quarantine, cfg, start); err != nil {
		t.Fatal(err)
	}
	if n, _ := m.CloseQuiet(ctx, cfg, start.Add(3*time.Hour)); n != 0 {
		t.Fatal("a quarantine must outlive the 2-hour request window")
	}
	if n, _ := m.CloseQuiet(ctx, cfg, start.Add(domain.QuietState+time.Minute)); n != 1 {
		t.Fatal("a quarantine with no news for a day should close")
	}
	if i := incidentByFP(t, m, "s03"); i.Status != "resolved" || i.ClosedBy != "quiet" {
		t.Fatalf("status=%s closed_by=%s", i.Status, i.ClosedBy)
	}
}

func TestQuietClosedIncidentReopensOnNewFault(t *testing.T) {
	ctx := context.Background()
	m, s, cfg := noiseSetup(t)
	start := time.Now().UTC().Add(-4 * time.Hour)
	f := openCarded(t, m, cfg, "U02", "re", start)
	if _, err := m.CloseQuiet(ctx, cfg, start.Add(domain.QuietRequest+time.Minute)); err != nil {
		t.Fatal(err)
	}
	before := len(liveDeliveries(t, s))
	later := time.Now().UTC()
	for n := 0; n < 5; n++ {
		f.At, f.EvidenceID, f.RequestID = later.Add(time.Duration(n)*time.Second), fmt.Sprint("new", n), fmt.Sprint("n", n)
		if err := m.Apply(ctx, f, cfg, f.At); err != nil {
			t.Fatal(err)
		}
	}
	i := incidentByFP(t, m, "re")
	if i.Status != "firing" || i.ClosedBy != "" {
		t.Fatalf("status=%s closed_by=%q", i.Status, i.ClosedBy)
	}
	ds := liveDeliveries(t, s)[before:]
	if len(ds) == 0 || ds[0].Kind != "firing" || !ds[0].ForceNew {
		t.Fatalf("a new outage should post a new card, got %+v", ds)
	}
}

func TestSourcesFresh(t *testing.T) {
	now := time.Now().UTC()
	ok := []domain.Cursor{
		{Source: "gateway", LastSuccess: now.Add(-30 * time.Second)},
		{Source: "postgres.errors.reconcile", LastSuccess: now.Add(-90 * time.Minute)},
		{Source: "notification", LastSuccess: now.Add(-24 * time.Hour)},
		{Source: "postgres.payments"},
	}
	if !SourcesFresh(ok, now) {
		t.Fatal("expected fresh")
	}
	if SourcesFresh(append(ok, domain.Cursor{Source: "postgres.attempts", LastSuccess: now.Add(-3 * time.Minute)}), now) {
		t.Fatal("a lagging source must block quiet closes")
	}
}
