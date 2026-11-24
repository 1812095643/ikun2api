package detectors

import (
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"testing"
	"time"
)

func TestRuleFamiliesAndCounterexamples(t *testing.T) {
	now := time.Now()
	cases := []struct {
		id, kind, reason string
		metrics          map[string]float64
		status           int
	}{
		{"Q01", "quota.snapshot", "balance", map[string]float64{"remaining": 0}, 0},
		{"Q03", "payment.snapshot", "", map[string]float64{"paid": 1, "fulfilled": 0, "paid_at_ms": float64(now.Add(-10 * time.Minute).UnixMilli())}, 0},
		{"U02", "attempt.failed", "", nil, 403}, {"U03", "attempt.failed", "tls", nil, 0},
		{"S03", "account.quarantined", "proxy_failure", nil, 0},
		{"L01", "request.active", "", map[string]float64{"elapsed_ms": 40000, "queue_ms": 35000, "queue_active": 1}, 0},
		{"F01", "stream.failed", "stream_error", nil, 0},
		{"F02", "request.finished", "missing_terminal", nil, 0},
		{"R01", "request.finished", "", map[string]float64{"attempts": 3}, 0},
		{"R02", "account.selected_quarantined", "guard_rejected_selected_account", map[string]float64{"selected_at_ms": 2, "quarantine_since_ms": 1}, 0},
		{"A01", "media.failed", "upstream_task_failed", nil, 0},
		{"B02", "billing.reconciliation", "", map[string]float64{"expected": 1, "actual": 2}, 0},
		{"B03", "billing.metadata_failed", "pending_missing", nil, 0},
		{"H01", "dependency.health", "", map[string]float64{"healthy": 0}, 0},
		{"H02", "host.resources", "", map[string]float64{"disk_free_percent": 2}, 0},
		{"M01", "source.unavailable", "schema_incompatible", nil, 0},
		{"M02", "delivery.health", "", map[string]float64{"permanent_failure": 1}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			o := observation(tc.kind, now)
			o.TaskID = "task"
			o.Reason = tc.reason
			o.Metrics = tc.metrics
			o.UpstreamStatus = tc.status
			got := Evaluate([]domain.Observation{o}, now, domain.DefaultConfig())
			if !hasRule(got, tc.id) {
				t.Fatalf("missing %s: %+v", tc.id, got)
			}
			o.Quality = domain.Stale
			if hasRule(Evaluate([]domain.Observation{o}, now, domain.DefaultConfig()), tc.id) {
				t.Fatal("stale observation raised a business finding")
			}
		})
	}
}
func TestPoolShrinkNeedsBaselineAndDuration(t *testing.T) {
	now := time.Now()
	single := observation("pool.snapshot", now)
	single.Metrics["eligible"] = 1
	if hasRule(Evaluate([]domain.Observation{single}, now, domain.DefaultConfig()), "S02") {
		t.Fatal("single-account pool noisy")
	}
	base := observation("pool.snapshot", now.Add(-10*time.Minute))
	base.Metrics["eligible"] = 3
	early := single
	early.EventID = "early"
	early.OccurredAt = now.Add(-3 * time.Minute)
	if !hasRule(Evaluate([]domain.Observation{base, early, single}, now, domain.DefaultConfig()), "S02") {
		t.Fatal("sustained pool shrink missed")
	}
}
func TestGraceCannotBeResetByRepeatedCompletion(t *testing.T) {
	now := time.Now()
	first := observation("media.completed", now.Add(-3*time.Minute))
	first.TaskID = "video"
	first.Metrics = map[string]float64{"deliverable": 1, "billing_expected": 1}
	repeat := first
	repeat.EventID = "repeat"
	repeat.OccurredAt = now.Add(-time.Second)
	if !hasRule(Evaluate([]domain.Observation{first, repeat}, now, domain.DefaultConfig()), "B01") {
		t.Fatal("completion polling reset billing grace")
	}
}
func TestValidRefundAndLateCancellationNotVPN(t *testing.T) {
	now := time.Now()
	o := observation("billing.reconciliation", now)
	o.TaskID = "task"
	o.Metrics = map[string]float64{"actual": 0, "expected": 1, "legitimate_refund": 1}
	if hasRule(Evaluate([]domain.Observation{o}, now, domain.DefaultConfig()), "B02") {
		t.Fatal("legitimate refund misclassified")
	}
	q := observation("quota.snapshot", now)
	q.Quality = domain.Partial
	q.Metrics["remaining"] = 100
	if hasRule(Evaluate([]domain.Observation{q}, now, domain.DefaultConfig()), "Q01") {
		t.Fatal("positive partial balance reported exhausted")
	}
}
func TestCatalogHasAllApprovedRules(t *testing.T) {
	if len(Catalog) != 28 {
		t.Fatal(len(Catalog))
	}
	seen := map[string]bool{}
	for _, d := range Catalog {
		if seen[d.ID] || d.Advice == "" || len(d.Requires) == 0 {
			t.Fatal(d)
		}
		seen[d.ID] = true
	}
}

func TestCancellationNeedsBaselineAndCorrelatedDegradation(t *testing.T) {
	now := time.Now()
	rows := []domain.Observation{}
	for n := 0; n < 40; n++ {
		o := observation("request.finished", now.Add(-time.Duration(60+n)*time.Second))
		o.EventID = fmt.Sprint(n)
		o.RequestID = o.EventID
		o.Outcome = "success"
		if n < 20 {
			o.OccurredAt = now.Add(-15 * time.Minute)
		} else if n < 25 {
			o.Outcome = "cancelled"
			o.Metrics["elapsed_ms"] = 60000
			o.Metrics["first_effective_ms"] = 45000
		}
		rows = append(rows, o)
	}
	if !hasRule(Evaluate(rows, now, domain.DefaultConfig()), "C01") {
		t.Fatal("correlated spike missing")
	}
	for n := range rows {
		rows[n].Metrics["elapsed_ms"] = 120000
		rows[n].Metrics["first_effective_ms"] = 1000
	}
	if hasRule(Evaluate(rows, now, domain.DefaultConfig()), "C01") {
		t.Fatal("cancellation alone classified as degradation")
	}
}
func TestQuarantineAfterSelectionIsNotSchedulerViolation(t *testing.T) {
	now := time.Now()
	o := observation("account.selected_quarantined", now)
	o.Metrics = map[string]float64{"selected_at_ms": 1, "quarantine_since_ms": 2}
	if hasRule(Evaluate([]domain.Observation{o}, now, domain.DefaultConfig()), "R02") {
		t.Fatal("selection before quarantine misclassified")
	}
}

func TestSlowOutputFindingCarriesSameRequestIdentity(t *testing.T) {
	now := time.Now()
	o := domain.Observation{Schema: 1, Site: "test", Source: "gateway", EventID: "one", RequestID: "r", Kind: "request.active", OccurredAt: now, ObservedAt: now, Quality: domain.Fresh, UserID: 3, KeyID: 7, AccountID: 9, GroupID: 2, Model: "gpt", Metrics: map[string]float64{"elapsed_ms": 61000}}
	findings := Evaluate([]domain.Observation{o}, now, domain.DefaultConfig())
	for _, f := range findings {
		if f.Rule == "L02" {
			if f.UserID != 3 || f.KeyID != 7 || f.AccountID != 9 || f.GroupID != 2 {
				t.Fatalf("identity lost: %+v", f)
			}
			return
		}
	}
	t.Fatal("L02 finding missing")
}
