package incidents

import (
	"context"
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/detectors"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/ownership"
	"github.com/MACOS-DO/sub4api/internal/monitoring/store"
	"path/filepath"
	"testing"
	"time"
)

func TestLifecycleDedupRecoveryAndReopen(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	m := &Manager{Store: s}
	now := time.Now().UTC()
	cfg := domain.DefaultConfig()
	cfg.Shadow = false
	cfg.FeishuTargets = []string{"oc_test"}
	cfg.Rules["U01"] = domain.RuleConfig{Enabled: true, Owner: "monitor", Version: 1}
	f := domain.Finding{Rule: "U01", RuleVersion: 1, Fingerprint: "account-failure", Scope: "account:1", Severity: "P1", At: now, EvidenceID: "failure", Quality: domain.Fresh}
	// U01 opens a card only after 5 requests in 10 minutes; duplicates do not count.
	for n := 0; n < 5; n++ {
		f.EvidenceID = fmt.Sprint("failure-", n)
		for k := 0; k < 2; k++ {
			if err = m.Apply(ctx, f, cfg, now); err != nil {
				t.Fatal(err)
			}
		}
	}
	rows, _ := s.Incidents(ctx)
	if len(rows) != 1 || rows[0].Count != 5 || rows[0].Status != "firing" {
		t.Fatal(rows)
	}
	ds, _ := s.Deliveries(ctx)
	if len(ds) != 1 || ds[0].Channel != "feishu" {
		t.Fatal("Feishu-only first occurrence", ds)
	}
	for n := 1; n <= 5; n++ {
		h := f
		h.Healthy = true
		h.At = now.Add(time.Duration(n) * time.Minute)
		h.EvidenceID = h.At.String()
		if err = m.Apply(ctx, h, cfg, h.At); err != nil {
			t.Fatal(err)
		}
	}
	rows, _ = s.Incidents(ctx)
	if rows[0].Status == "resolved" {
		t.Fatal("resolved before five-minute healthy span")
	}
	h := f
	h.Healthy = true
	h.At = now.Add(6 * time.Minute)
	h.EvidenceID = "healthy-final"
	if err = m.Apply(ctx, h, cfg, h.At); err != nil {
		t.Fatal(err)
	}
	rows, _ = s.Incidents(ctx)
	if rows[0].Status != "resolved" {
		t.Fatal(rows)
	}
	f.At = now.Add(7 * time.Minute)
	f.EvidenceID = "reopen"
	if err = m.Apply(ctx, f, cfg, f.At); err != nil {
		t.Fatal(err)
	}
	rows, _ = s.Incidents(ctx)
	if rows[0].Status != "firing" || rows[0].Count != 6 {
		t.Fatal(rows)
	}
}
func TestHistoryOwnerAndSilenceNeverMeanRecovery(t *testing.T) {
	ctx := context.Background()
	s, e := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	m := &Manager{Store: s}
	now := time.Now().UTC()
	cfg := domain.DefaultConfig()
	f := domain.Finding{Rule: "Q01", RuleVersion: 1, Fingerprint: "balance", Scope: "user:1", Severity: "P3", At: now, EvidenceID: "one", Quality: domain.Fresh, Historical: true}
	if e = m.Apply(ctx, f, cfg, now); e != nil {
		t.Fatal(e)
	}
	rows, _ := s.Incidents(ctx)
	if len(rows) != 0 {
		t.Fatal("historical alert replay")
	}
	f.Historical = false
	if e = m.Apply(ctx, f, cfg, now); e != nil {
		t.Fatal(e)
	}
	rows, _ = s.Incidents(ctx)
	if len(rows) != 1 {
		t.Fatal(rows)
	}
	if e = m.Control(ctx, rows[0].ID, "ack", "admin:1", "ack-1", rows[0].Revision, time.Time{}, "", now); e != nil {
		t.Fatal(e)
	}
	rows, _ = s.Incidents(ctx)
	if !rows[0].Acknowledged || rows[0].Status != "firing" {
		t.Fatal(rows)
	}
	if e = m.Control(ctx, rows[0].ID, "silence", "admin:1", "silence-1", rows[0].Revision, now.Add(time.Hour), "maintenance", now); e != nil {
		t.Fatal(e)
	}
	rows, _ = s.Incidents(ctx)
	if rows[0].Status != "firing" {
		t.Fatal("silence resolved incident")
	}
	ds, _ := s.Deliveries(ctx)
	if len(ds) != 0 {
		t.Fatal("legacy owner emitted")
	}
}

func TestStaleLateEvidenceCannotUndoRecoveryAndEscalationBypassesSilence(t *testing.T) {
	ctx := context.Background()
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	m := &Manager{Store: s}
	now := time.Now()
	cfg := domain.DefaultConfig()
	cfg.Rules["Q01"] = domain.RuleConfig{Enabled: true, Owner: "monitor", Version: 1}
	cfg.FeishuTargets = []string{"oc_test"}
	f := domain.Finding{Rule: "Q01", RuleVersion: 1, Fingerprint: "quota", Severity: "P3", At: now, Quality: domain.Fresh, EvidenceID: "fault"}
	if e = m.Apply(ctx, f, cfg, now); e != nil {
		t.Fatal(e)
	}
	rows, _ := s.Incidents(ctx)
	if e = m.Control(ctx, rows[0].ID, "silence", "admin", "silence", rows[0].Revision, now.Add(time.Hour), "maintenance", now); e != nil {
		t.Fatal(e)
	}
	f.Severity = "P1"
	f.At = now.Add(time.Minute)
	f.EvidenceID = "escalation"
	if e = m.Apply(ctx, f, cfg, f.At); e != nil {
		t.Fatal(e)
	}
	ds, _ := s.Deliveries(ctx)
	if len(ds) != 2 {
		t.Fatal("escalation suppressed", ds)
	}
	f.Healthy = true
	f.At = now.Add(2 * time.Minute)
	f.EvidenceID = "healthy"
	if e = m.Apply(ctx, f, cfg, f.At); e != nil {
		t.Fatal(e)
	}
	f.Healthy = false
	f.At = now.Add(time.Second)
	f.EvidenceID = "late"
	if e = m.Apply(ctx, f, cfg, now.Add(3*time.Minute)); e != nil {
		t.Fatal(e)
	}
	rows, _ = s.Incidents(ctx)
	if rows[0].Status != "resolved" {
		t.Fatal("late failure reopened recovered incident", rows)
	}
}

func TestLateTerminalFailureUpgradesSameAttemptWithoutDoubleCounting(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	m := &Manager{Store: s}
	now := time.Now()
	cfg := domain.DefaultConfig()
	cfg.FeishuTargets = []string{"oc_test"}
	cfg.Rules["U01"] = domain.RuleConfig{Enabled: true, Owner: "monitor", Version: 1}
	f := domain.Finding{Rule: "U01", RuleVersion: 1, Fingerprint: "fp", Severity: "P2", At: now, Quality: domain.Fresh}
	for n := 0; n < 4; n++ {
		f.EvidenceID = fmt.Sprint("earlier-", n)
		if err = m.Apply(ctx, f, cfg, now); err != nil {
			t.Fatal(err)
		}
	}
	f.EvidenceID = "attempt"
	if err = m.Apply(ctx, f, cfg, now); err != nil {
		t.Fatal(err)
	}
	f.Severity = "P1"
	if err = m.Apply(ctx, f, cfg, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.Incidents(ctx)
	if rows[0].Severity != "P1" || rows[0].Count != 5 {
		t.Fatal(rows)
	}
	ds, _ := s.Deliveries(ctx)
	if len(ds) != 2 {
		t.Fatal(ds)
	}
}

func TestProxyParentWaitsForAllAffectedAccounts(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	m := &Manager{Store: s}
	now := time.Now()
	cfg := domain.DefaultConfig()
	cfg.FeishuTargets = []string{"oc_test"}
	cfg.Rules["S03"] = domain.RuleConfig{Enabled: true, Owner: "monitor", Version: 1}
	rows := []domain.Observation{}
	for _, id := range []int64{1, 2} {
		rows = append(rows, domain.Observation{Schema: 1, Site: "test", Source: "gateway", EventID: fmt.Sprint(id), Kind: "account.quarantined", AccountID: id, Quality: domain.Fresh, OccurredAt: now, ObservedAt: now, Metrics: map[string]float64{"proxy_id": 5}})
	}
	for _, f := range detectors.Evaluate(rows, now, cfg) {
		if err = m.Apply(ctx, f, cfg, now); err != nil {
			t.Fatal(err)
		}
	}
	ds, _ := s.Deliveries(ctx)
	if len(ds) != 1 {
		t.Fatal("children sent duplicate notifications", ds)
	}
	for _, id := range []int64{1, 2} {
		o := domain.Observation{Schema: 1, Site: "test", Source: "gateway", EventID: fmt.Sprintf("recovered-%d", id), Kind: "account.recovered", AccountID: id, Quality: domain.Fresh, OccurredAt: now.Add(time.Minute), ObservedAt: now.Add(time.Minute)}
		prior, err := s.ActiveIncidents(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range detectors.Evaluate([]domain.Observation{o}, o.OccurredAt, cfg, prior...) {
			if err = m.Apply(ctx, f, cfg, o.OccurredAt); err != nil {
				t.Fatal(err)
			}
		}
		if err = m.Tick(ctx, cfg, o.OccurredAt); err != nil {
			t.Fatal(err)
		}
		current, _ := s.Incidents(ctx)
		for _, i := range current {
			if i.Scope == "proxy:5" {
				if i.AccountCount != 2 {
					t.Fatal(i)
				}
				if (i.Status == "resolved") != (id == 2) {
					t.Fatal("parent recovery lost child impact", i)
				}
			}
		}
	}
}

func TestOwnershipProjectionCASRollbackAndShadow(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	m := &Manager{Store: s, OwnershipPath: filepath.Join(dir, "ownership.json")}
	c := domain.DefaultConfig()
	c.Actor = "admin:1"
	c.RequestID = "initial"
	c.Shadow = false
	c.Rules["U01"] = domain.RuleConfig{Enabled: true, Owner: "monitor", Version: 1}
	c.LegacyHandoffs = []domain.LegacyHandoff{{LegacyRuleID: 7, MonitorRules: []string{"U01"}}}
	if err = m.Configure(ctx, c, 0); err != nil {
		t.Fatal(err)
	}
	suppressed, err := ownership.Suppressed(m.OwnershipPath, 7, 0, "")
	if err != nil || !suppressed {
		t.Fatal(suppressed, err)
	}
	c.RequestID = "stale"
	c.Shadow = true
	if err = m.Configure(ctx, c, 0); err != store.ErrConflict {
		t.Fatal("stale save", err)
	}
	suppressed, err = ownership.Suppressed(m.OwnershipPath, 7, 0, "")
	if err != nil || !suppressed {
		t.Fatal("CAS restored wrong projection", suppressed, err)
	}
	c, err = s.Config(ctx)
	if err != nil {
		t.Fatal(err)
	}
	c.Shadow = true
	c.RequestID = "rollback"
	if err = m.Configure(ctx, c, c.Version); err != nil {
		t.Fatal(err)
	}
	suppressed, err = ownership.Suppressed(m.OwnershipPath, 7, 0, "")
	if err != nil || suppressed {
		t.Fatal("shadow did not return old ownership", suppressed, err)
	}
}

func TestStaleEvaluationCannotUndoConfigurationHandoff(t *testing.T) {
	ctx := context.Background()
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	m := &Manager{Store: s, OwnershipPath: filepath.Join(t.TempDir(), "ownership.json")}
	c := domain.DefaultConfig()
	c.Actor = "admin:1"
	c.RequestID = "first"
	if e = m.Configure(ctx, c, 0); e != nil {
		t.Fatal(e)
	}
	old, _ := s.Config(ctx)
	c = old
	c.RequestID = "second"
	c.BillingGraceSeconds = 180
	if e = m.Configure(ctx, c, c.Version); e != nil {
		t.Fatal(e)
	}
	if e = m.Tick(ctx, old, time.Now()); e != store.ErrConflict {
		t.Fatal("stale Tick accepted", e)
	}
	f := domain.Finding{Rule: "U01", RuleVersion: old.Rule("U01").Version, Fingerprint: "stale", EvidenceID: "stale", Quality: domain.Fresh, At: time.Now(), Severity: "P1"}
	if e = m.Apply(ctx, f, old, time.Now()); e != store.ErrConflict {
		t.Fatal("stale findings accepted", e)
	}
}

func TestSlowFirstOutputDoesNotRoute(t *testing.T) {
	cfg := domain.DefaultConfig()
	cfg.Shadow = false
	cfg.FeishuTargets = []string{"ou_test"}
	cfg.EmailTargets = []string{"ops@example.test"}
	cfg.Rules["L02"] = domain.RuleConfig{Enabled: true, Owner: "monitor"}
	for _, kind := range []string{"firing", "escalated", "resolved", "reminder"} {
		ds := route(domain.Incident{ID: "slow", Rule: "L02", Severity: "P1"}, kind, cfg, time.Now(), false)
		if len(ds) != 0 {
			t.Fatalf("%s routed %+v", kind, ds)
		}
	}
}

func TestIncidentKeepsOneConsistentLatestFaultSample(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	m := &Manager{Store: s}
	now := time.Now().UTC()
	cfg := domain.DefaultConfig()
	cfg.Rules["L02"] = domain.RuleConfig{Enabled: true, Owner: "monitor", Version: 1}
	f := domain.Finding{Rule: "L02", RuleVersion: 1, Fingerprint: "same", Scope: "channel:1/model:m", Severity: "P2", At: now, EvidenceID: "1", Quality: domain.Fresh, UserID: 3, KeyID: 7, AccountID: 9, GroupID: 2}
	if err = m.Apply(ctx, f, cfg, now); err != nil {
		t.Fatal(err)
	}
	f.At = now.Add(time.Second)
	f.EvidenceID = "2"
	f.UserID = 4
	f.KeyID = 8
	f.AccountID = 0
	f.GroupID = 5
	if err = m.Apply(ctx, f, cfg, f.At); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Incidents(ctx)
	if err != nil || len(rows) != 1 {
		t.Fatal(err, rows)
	}
	sample := rows[0].Sample
	if sample == nil || sample.UserID != 4 || sample.KeyID != 8 || sample.AccountID != 0 || sample.GroupID != 5 {
		t.Fatalf("inconsistent sample: %+v", sample)
	}
}

// Email is a backup for P0/P1 new faults, escalations and recoveries only.
func TestEmailOnlyForSeriousStateChanges(t *testing.T) {
	cfg := domain.DefaultConfig()
	cfg.Shadow = false
	cfg.FeishuTargets = []string{"ou_test"}
	cfg.EmailTargets = []string{"ops@example.test"}
	for _, rule := range []string{"U01", "S01", "M01", "Q01", "F01", "L03"} {
		cfg.Rules[rule] = domain.RuleConfig{Enabled: true, Owner: "monitor"}
	}
	cases := []struct {
		severity, kind, scope string
		forceNew              bool
		email                 bool
	}{
		{"P1", "firing", "", false, true},
		{"P0", "escalated", "", false, true},
		{"P1", "resolved", "", false, true},
		{"P1", "update", "", false, false},
		{"P1", "update", "", true, true},
		{"P2", "firing", "", false, false},
		{"P3", "firing", "", false, false},
		{"P1", "firing", "digest", false, false},
	}
	for _, rule := range []string{"U01", "S01", "M01", "Q01", "F01", "L03"} {
		for _, c := range cases {
			out := route(domain.Incident{ID: rule, Rule: rule, Severity: c.severity, Scope: c.scope}, c.kind, cfg, time.Now(), c.forceNew)
			emails := 0
			for _, d := range out {
				if d.Channel == "email" {
					emails++
				}
			}
			if out[0].Channel != "feishu" || (emails == 1) != c.email {
				t.Fatalf("%s %+v: %d deliveries, %d email", rule, c, len(out), emails)
			}
		}
	}
}
