package detectors

import (
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"testing"
	"time"
)

func TestWidePoolFailureEscalatesButSingleScopeDoesNot(t *testing.T) {
	now := time.Now().UTC()
	cfg := domain.DefaultConfig()
	rows := []domain.Observation{}
	for n := 0; n < 6; n++ {
		rows = append(rows, domain.Observation{Site: "test", Source: "gateway", EventID: fmt.Sprint(n), RequestID: fmt.Sprint(n), UserID: int64(n%3 + 1), GroupID: int64(n%3 + 1), Model: "model", Kind: "pool.empty", Quality: domain.Fresh, OccurredAt: now.Add(time.Duration(n-6) * time.Second), Metrics: map[string]float64{"total": 0, "eligible": 0}})
	}
	for _, f := range Evaluate(rows, now, cfg) {
		if f.Rule == "S01" && f.Severity != "P0" {
			t.Fatal("wide outage not escalated", f)
		}
	}
	for n := range rows {
		rows[n].GroupID = 1
	}
	for _, f := range Evaluate(rows, now, cfg) {
		if f.Rule == "S01" && f.Severity == "P0" {
			t.Fatal("single scope escalated", f)
		}
	}
}

func TestBatchDuplicateBillingEscalationNeedsIndependentUsers(t *testing.T) {
	now := time.Now()
	rows := []domain.Observation{}
	for n := 1; n <= 2; n++ {
		rows = append(rows, domain.Observation{Site: "test", Source: "pg", EventID: fmt.Sprint(n), TaskID: fmt.Sprint(n), UserID: int64(n), Kind: "billing.reconciliation", Quality: domain.Fresh, OccurredAt: now, Metrics: map[string]float64{"confirmed_duplicate": 1}})
	}
	got := Evaluate(rows, now, domain.DefaultConfig())
	count := 0
	for _, f := range got {
		if f.Rule == "B02" && f.Severity == "P0" {
			count++
		}
	}
	if count != 2 {
		t.Fatal(got)
	}
	rows[1].UserID = 1
	for _, f := range Evaluate(rows, now, domain.DefaultConfig()) {
		if f.Severity == "P0" {
			t.Fatal("single user batch escalated", f)
		}
	}
}

func TestHealthyPopulationOnlyEmitsRecoveryForKnownIncidents(t *testing.T) {
	now := time.Now()
	o := observation("request.finished", now)
	o.Outcome = "success"
	o.Metrics = map[string]float64{"terminal": 1, "attempts": 1}
	if got := Evaluate([]domain.Observation{o}, now, domain.DefaultConfig()); len(got) != 0 {
		t.Fatalf("healthy traffic generated %d unnecessary findings", len(got))
	}
}
