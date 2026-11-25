package detectors

import (
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"testing"
	"time"
)

func stallRows(now time.Time, n int) []domain.Observation {
	var rows []domain.Observation
	for i := 0; i < n; i++ {
		o := observation("request.active", now.Add(-time.Duration(i)*time.Second))
		o.Source = "gateway"
		o.RequestID = fmt.Sprint("stall-", i)
		o.EventID = o.RequestID
		o.Metrics = map[string]float64{"stall_evidence_v1": 1, "has_output": 1, "idle_ms": 65000}
		rows = append(rows, o)
	}
	return rows
}
func TestStallsRequireDistinctFrequentRequests(t *testing.T) {
	now := time.Now()
	rows := stallRows(now, 3)
	cfg := domain.DefaultConfig()
	if hasRule(Evaluate(rows[:1], now, cfg), "L04") {
		t.Fatal("single stall alerted")
	}
	dup := append([]domain.Observation{}, rows[:1]...)
	for n := 0; n < 20; n++ {
		o := rows[0]
		o.EventID = fmt.Sprint(n)
		dup = append(dup, o)
	}
	if hasRule(Evaluate(dup, now, cfg), "L04") {
		t.Fatal("polling counted as distinct requests")
	}
	fs := Evaluate(rows, now, cfg)
	if !hasRule(fs, "L04") {
		t.Fatal("frequent stalls missed")
	}
	for _, f := range fs {
		if f.Rule == "L04" && f.Scope != cohort(rows[0]) {
			t.Fatal("not aggregated")
		}
	}
	for n := 3; n < 20; n++ {
		o := rows[0]
		o.RequestID = fmt.Sprint("normal", n)
		o.Metrics = map[string]float64{"stall_evidence_v1": 1, "has_output": 1, "idle_ms": 100}
		rows = append(rows, o)
	}
	if hasRule(Evaluate(rows, now, cfg), "L04") {
		t.Fatal("below 20 percent alerted")
	}
}
func TestStallsRejectIncompleteAndThinkingEvidence(t *testing.T) {
	now := time.Now()
	cfg := domain.DefaultConfig()
	for _, kind := range []string{"partial", "reasoning_started", "reasoning_output", "stale", "late_partial", "other_scope", "old", "old_parser", "missing_model"} {
		t.Run(kind, func(t *testing.T) {
			rows := stallRows(now, 3)
			switch kind {
			case "missing_model":
				rows[0].Model = ""
			case "old_parser":
				delete(rows[0].Metrics, "stall_evidence_v1")
			case "partial":
				rows[0].Quality = domain.Partial
			case "stale":
				rows[0].Quality = domain.Stale
			case "reasoning_started", "reasoning_output":
				rows[0].Metrics[kind] = 1
			case "late_partial":
				o := rows[0]
				o.Kind = "request.finished"
				o.Quality = domain.Partial
				rows = append(rows, o)
			case "other_scope":
				rows[0].AccountID++
			case "old":
				rows[0].OccurredAt = now.Add(-11 * time.Minute)
			}
			if hasRule(Evaluate(rows, now, cfg), "L04") {
				t.Fatal("uncertain or separate evidence alerted")
			}
		})
	}
}

func TestStallEvidenceStableAndRecoveryRequiresTraffic(t *testing.T) {
	now := time.Now()
	cfg := domain.DefaultConfig()
	rows := stallRows(now, 3)
	fs := Evaluate(rows, now, cfg)
	var seed domain.Finding
	ids := map[string]bool{}
	for _, f := range fs {
		if f.Rule == "L04" {
			seed = f
			ids[f.EvidenceID] = true
		}
	}
	for n := range rows {
		rows[n].EventID += "new-poll"
		rows[n].OccurredAt = now.Add(time.Second)
	}
	for _, f := range Evaluate(rows, now.Add(time.Second), cfg) {
		if f.Rule == "L04" && !ids[f.EvidenceID] {
			t.Fatal("same requests caused new evidence")
		}
	}
	prior := domain.Incident{Rule: "L04", RuleVersion: seed.RuleVersion, Fingerprint: seed.Fingerprint + ":v1", Scope: seed.Scope, Status: "firing", LastFault: now}
	if len(Evaluate(nil, now.Add(20*time.Minute), cfg, prior)) != 0 {
		t.Fatal("silence treated as recovery")
	}
	rows = nil
	for n := 0; n < 5; n++ {
		o := observation("request.finished", now.Add(time.Duration(n)*90*time.Second))
		o.RequestID = fmt.Sprint("good", n)
		o.EventID = o.RequestID
		o.Outcome = "success"
		o.Metrics = map[string]float64{"stall_evidence_v1": 1, "has_output": 1, "terminal": 1}
		rows = append(rows, o)
	}
	found := false
	for _, f := range Evaluate(rows, now.Add(6*time.Minute), cfg, prior) {
		if f.Rule == "L04" && f.Healthy {
			found = true
		}
	}
	if !found {
		t.Fatal("confirmed sustained improvement missed")
	}
}

func TestStallWindowExpiryDoesNotRenotifySameRequests(t *testing.T) {
	now := time.Now()
	cfg := domain.DefaultConfig()
	rows := stallRows(now, 4)
	ids := map[string]bool{}
	for _, f := range Evaluate(rows, now, cfg) {
		if f.Rule == "L04" {
			ids[f.EvidenceID] = true
		}
	}
	for _, f := range Evaluate(rows[:3], now, cfg) {
		if f.Rule == "L04" && !ids[f.EvidenceID] {
			t.Fatal("expiry generated another alert without a new stalled request")
		}
	}
}
