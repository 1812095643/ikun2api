package incidents

import (
	"context"
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/store"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

func noiseSetup(t *testing.T) (*Manager, *store.Store, domain.Config) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	cfg := domain.DefaultConfig()
	cfg.Shadow = false
	cfg.FeishuTargets = []string{"oc_test"}
	for _, id := range []string{"U01", "U02", "U03", "F01", "F02", "R01", "Q01"} {
		cfg.Rules[id] = domain.RuleConfig{Enabled: true, Owner: "monitor", Version: 1}
	}
	return &Manager{Store: s}, s, cfg
}

func liveDeliveries(t *testing.T, s *store.Store) []domain.Delivery {
	t.Helper()
	ds, err := s.Deliveries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var out []domain.Delivery
	for _, d := range ds {
		if d.State != "superseded" {
			out = append(out, d)
		}
	}
	// The store lists newest first; tests read deliveries in the order they were made.
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].IncidentID == out[b].IncidentID {
			return out[a].Revision < out[b].Revision
		}
		return out[a].NextAttempt.Before(out[b].NextAttempt)
	})
	return out
}

func TestGateArchivesBelowThresholdAndOpensAtFiveRequests(t *testing.T) {
	ctx := context.Background()
	m, s, cfg := noiseSetup(t)
	now := time.Now().UTC()
	f := domain.Finding{Rule: "U02", RuleVersion: 1, Fingerprint: "u02", Scope: "group:1", Severity: "P1", Quality: domain.Fresh, UserID: 7}
	for n := 0; n < 4; n++ {
		f.At, f.EvidenceID = now.Add(time.Duration(n)*time.Minute), fmt.Sprint(n)
		if err := m.Apply(ctx, f, cfg, f.At); err != nil {
			t.Fatal(err)
		}
	}
	if ds := liveDeliveries(t, s); len(ds) != 0 {
		t.Fatalf("4 requests from 1 user must stay archived, got %d", len(ds))
	}
	rows, _ := s.Incidents(ctx)
	if len(rows) != 1 || rows[0].Count != 4 {
		t.Fatal("archived incident must remain queryable", rows)
	}
	// Hits older than 10 minutes do not count toward the threshold.
	f.At, f.EvidenceID = now.Add(15*time.Minute), "late"
	if err := m.Apply(ctx, f, cfg, f.At); err != nil {
		t.Fatal(err)
	}
	if ds := liveDeliveries(t, s); len(ds) != 0 {
		t.Fatal("stale hits must expire from the window")
	}
	for n := 0; n < 4; n++ {
		f.At, f.EvidenceID = now.Add(time.Duration(16+n)*time.Minute), fmt.Sprint("w", n)
		if err := m.Apply(ctx, f, cfg, f.At); err != nil {
			t.Fatal(err)
		}
	}
	ds := liveDeliveries(t, s)
	if len(ds) != 1 || ds[0].Kind != "firing" {
		t.Fatalf("5th request in window must open one card: %+v", ds)
	}
}

func TestGateOpensOnTwoUsers(t *testing.T) {
	ctx := context.Background()
	m, s, cfg := noiseSetup(t)
	now := time.Now().UTC()
	f := domain.Finding{Rule: "F01", RuleVersion: 1, Fingerprint: "f01", Scope: "group:1", Severity: "P1", Quality: domain.Fresh, At: now}
	for _, u := range []int64{1, 2} {
		f.UserID, f.EvidenceID = u, fmt.Sprint(u)
		if err := m.Apply(ctx, f, cfg, now); err != nil {
			t.Fatal(err)
		}
	}
	if ds := liveDeliveries(t, s); len(ds) != 1 {
		t.Fatalf("2 users must open a card, got %d", len(ds))
	}
}

func TestUngatedRuleStillOpensImmediately(t *testing.T) {
	ctx := context.Background()
	m, s, cfg := noiseSetup(t)
	now := time.Now().UTC()
	f := domain.Finding{Rule: "Q01", RuleVersion: 1, Fingerprint: "q01", Scope: "user:1", Severity: "P1", Quality: domain.Fresh, At: now, EvidenceID: "1"}
	if err := m.Apply(ctx, f, cfg, now); err != nil {
		t.Fatal(err)
	}
	if ds := liveDeliveries(t, s); len(ds) != 1 {
		t.Fatalf("Q01 is not gated, got %d", len(ds))
	}
}

// Account 33 style: many rules and models on one account become one card.
func TestAccountRollupMergesRulesAndModels(t *testing.T) {
	ctx := context.Background()
	m, s, cfg := noiseSetup(t)
	now := time.Now().UTC()
	rules := []string{"U01", "U02", "U03", "F01", "R01", "F02"}
	models := []string{"claude-a", "claude-b", "gpt-c"}
	n := 0
	for _, r := range rules {
		for _, model := range models {
			for k := 0; k < 2; k++ {
				at := now.Add(time.Duration(n) * 20 * time.Second)
				f := domain.Finding{Rule: r, RuleVersion: 1, Fingerprint: r + ":" + model, Scope: "account:33:" + model, Model: model, AccountID: 33, UserID: int64(k + 1), Severity: "P1", Quality: domain.Fresh, At: at, EvidenceID: fmt.Sprint("req-", n)}
				if err := m.Apply(ctx, f, cfg, at); err != nil {
					t.Fatal(err)
				}
				n++
			}
		}
	}
	firing := 0
	for _, d := range liveDeliveries(t, s) {
		if d.Kind == "firing" {
			firing++
			if !d.Payload.Rollup || d.Payload.Scope != "account:33" {
				t.Fatalf("only the account rollup may open a card: %+v", d.Payload)
			}
		}
	}
	if firing != 1 {
		t.Fatalf("want 1 card for account 33, got %d", firing)
	}
	r, _ := s.Incident(ctx, "rollup:account:33")
	if r == nil || len(r.Members) != len(rules)*len(models) || r.Count != n {
		t.Fatalf("rollup must list every rule/model: %+v", r)
	}
	// Members stay queryable and point at the rollup.
	if c, _ := s.Children(ctx, r.ID); len(c) != len(rules)*len(models) {
		t.Fatalf("children %d", len(c))
	}
}

func openCard(t *testing.T, m *Manager, cfg domain.Config, at time.Time, tag string) {
	t.Helper()
	f := domain.Finding{Rule: "Q01", RuleVersion: 1, Fingerprint: "q", Scope: "user:1", Severity: "P1", Quality: domain.Fresh, At: at, EvidenceID: tag}
	if err := m.Apply(context.Background(), f, cfg, at); err != nil {
		t.Fatal(err)
	}
}

func healthy(t *testing.T, m *Manager, cfg domain.Config, from time.Time, minutes int) time.Time {
	t.Helper()
	at := from
	for n := 1; n <= minutes; n++ {
		at = from.Add(time.Duration(n) * time.Minute)
		h := domain.Finding{Rule: "Q01", RuleVersion: 1, Fingerprint: "q", Scope: "user:1", Healthy: true, Quality: domain.Fresh, At: at, EvidenceID: "h" + at.String()}
		if err := m.Apply(context.Background(), h, cfg, at); err != nil {
			t.Fatal(err)
		}
	}
	return at
}

func kinds(ds []domain.Delivery) (out []string) {
	for _, d := range ds {
		k := d.Kind
		if d.ForceNew {
			k += "+new"
		}
		out = append(out, k)
	}
	return out
}

func TestResolvedWaitsForThirtyQuietMinutes(t *testing.T) {
	ctx := context.Background()
	m, s, cfg := noiseSetup(t)
	now := time.Now().UTC()
	openCard(t, m, cfg, now, "1")
	healthy(t, m, cfg, now, 10)
	rows, _ := s.Incidents(ctx)
	if rows[0].Status != "resolved" {
		t.Fatal("state must resolve on recovery evidence", rows[0].Status)
	}
	if got := kinds(liveDeliveries(t, s)); len(got) != 1 {
		t.Fatalf("no 已恢复 before 30 quiet minutes: %v", got)
	}
	if err := m.Tick(ctx, cfg, now.Add(29*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got := kinds(liveDeliveries(t, s)); len(got) != 1 {
		t.Fatalf("still inside quiet period: %v", got)
	}
	for _, tick := range []time.Duration{31, 32, 45} {
		if err := m.Tick(ctx, cfg, now.Add(tick*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	got := kinds(liveDeliveries(t, s))
	if len(got) != 2 || got[1] != "resolved" {
		t.Fatalf("exactly one 已恢复 after quiet period: %v", got)
	}
}

func TestRecurrenceWithinHourEditsCardSilently(t *testing.T) {
	ctx := context.Background()
	m, s, cfg := noiseSetup(t)
	now := time.Now().UTC()
	openCard(t, m, cfg, now, "1")
	healthy(t, m, cfg, now, 10)
	if err := m.Tick(ctx, cfg, now.Add(41*time.Minute)); err != nil {
		t.Fatal(err)
	}
	// Recurs 50 minutes after recovery: same card, no new message.
	openCard(t, m, cfg, now.Add(60*time.Minute), "2")
	got := kinds(liveDeliveries(t, s))
	if len(got) != 3 || got[2] != "update" {
		t.Fatalf("recurrence within 1h must silently edit: %v", got)
	}
	// Recovers again, then recurs 2 hours later: new episode, new message.
	healthy(t, m, cfg, now.Add(60*time.Minute), 10)
	if err := m.Tick(ctx, cfg, now.Add(101*time.Minute)); err != nil {
		t.Fatal(err)
	}
	openCard(t, m, cfg, now.Add(230*time.Minute), "3")
	got = kinds(liveDeliveries(t, s))
	if got[len(got)-1] != "firing+new" {
		t.Fatalf("recurrence after 1h must post a new message: %v", got)
	}
}

func TestFlappingDuringQuietPeriodSendsNothing(t *testing.T) {
	ctx := context.Background()
	m, s, cfg := noiseSetup(t)
	now := time.Now().UTC()
	openCard(t, m, cfg, now, "1")
	healthy(t, m, cfg, now, 10)
	openCard(t, m, cfg, now.Add(15*time.Minute), "2")
	healthy(t, m, cfg, now.Add(15*time.Minute), 10)
	for n := 26; n <= 90; n++ {
		if err := m.Tick(ctx, cfg, now.Add(time.Duration(n)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	got := kinds(liveDeliveries(t, s))
	resolved := 0
	for _, k := range got {
		if k == "resolved" {
			resolved++
		}
		if k == "firing+new" {
			t.Fatalf("flapping must not post new messages: %v", got)
		}
	}
	if resolved != 1 {
		t.Fatalf("one 已恢复 after things settle: %v", got)
	}
}

// A recovery announced immediately (quiet period already over) must not be re-announced by Tick.
func TestImmediateResolvedIsNotRepeated(t *testing.T) {
	ctx := context.Background()
	m, s, cfg := noiseSetup(t)
	now := time.Now().UTC()
	openCard(t, m, cfg, now, "1")
	at := now.Add(40 * time.Minute)
	h := domain.Finding{Rule: "Q01", RuleVersion: 1, Fingerprint: "q", Scope: "user:1", Healthy: true, Quality: domain.Fresh, At: at, EvidenceID: "late-healthy"}
	if err := m.Apply(ctx, h, cfg, at); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 5; n++ {
		if err := m.Tick(ctx, cfg, at.Add(time.Duration(n)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	got := kinds(liveDeliveries(t, s))
	if len(got) != 2 || got[1] != "resolved" {
		t.Fatalf("one 已恢复 only: %v", got)
	}
}

// M01/S02/S03 recurring hours later edit the original card instead of pushing.
func TestSilentRecurrenceRulesNeverPushAgain(t *testing.T) {
	for _, rule := range []string{"M01", "S02", "S03"} {
		t.Run(rule, func(t *testing.T) {
			ctx := context.Background()
			m, s, cfg := noiseSetup(t)
			cfg.Rules[rule] = domain.RuleConfig{Enabled: true, Owner: "monitor", Version: 1}
			now := time.Now().UTC()
			fault := func(at time.Time, ev string) {
				f := domain.Finding{Rule: rule, RuleVersion: 1, Fingerprint: "x", Scope: "source:x", Severity: "P1", Quality: domain.Fresh, At: at, EvidenceID: ev}
				if err := m.Apply(ctx, f, cfg, at); err != nil {
					t.Fatal(err)
				}
			}
			heal := func(at time.Time) {
				for n := 0; n < 6; n++ {
					h := domain.Finding{Rule: rule, RuleVersion: 1, Fingerprint: "x", Scope: "source:x", Healthy: true, Quality: domain.Fresh, At: at.Add(time.Duration(n) * time.Minute), EvidenceID: fmt.Sprint("h", at.Unix(), n)}
					if err := m.Apply(ctx, h, cfg, h.At); err != nil {
						t.Fatal(err)
					}
				}
			}
			fault(now, "1")
			heal(now.Add(time.Minute))
			if err := m.Tick(ctx, cfg, now.Add(40*time.Minute)); err != nil {
				t.Fatal(err)
			}
			fault(now.Add(3*time.Hour), "2") // recurs 3 hours later
			fault(now.Add(9*time.Hour), "3") // and again much later, still firing
			for _, d := range liveDeliveries(t, s)[1:] {
				if d.ForceNew || d.Kind == "firing" {
					t.Fatalf("recurrence must edit the original card: %v", kinds(liveDeliveries(t, s)))
				}
			}
			if got := kinds(liveDeliveries(t, s)); got[0] != "firing" || len(got) < 2 {
				t.Fatalf("%v", got)
			}
		})
	}
}
