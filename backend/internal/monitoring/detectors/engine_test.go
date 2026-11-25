package detectors

import (
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"testing"
	"time"
)

func observation(kind string, now time.Time) domain.Observation {
	return domain.Observation{Schema: 1, Site: "test", Source: "gateway", EventID: kind, Kind: kind, OccurredAt: now, ObservedAt: now, Quality: domain.Fresh, RequestID: "r1", AccountID: 7, GroupID: 19, Model: "model", Metrics: map[string]float64{}}
}
func hasRule(fs []domain.Finding, id string) bool {
	for _, f := range fs {
		if f.Rule == id && !f.Healthy {
			return true
		}
	}
	return false
}
func TestDailyLimitDoesNotSuppressIndependentUpstream(t *testing.T) {
	now := time.Now()
	local := observation("request.finished", now)
	local.Reason = "key_daily_limit"
	local.ClientStatus = 429
	up := observation("attempt.failed", now)
	up.UpstreamStatus = 429
	got := Evaluate([]domain.Observation{local, up}, now, domain.DefaultConfig())
	if !hasRule(got, "U01") || hasRule(got, "Q01") || hasRule(got, "Q05") || hasRule(got, "F01") {
		t.Fatal(got)
	}
}
func TestRecoveredRequestStillReportsUpstreamWithoutClaimingQuota(t *testing.T) {
	now := time.Now()
	up := observation("attempt.failed", now)
	up.UpstreamStatus = 429
	end := observation("request.finished", now)
	end.Outcome = "success"
	end.AccountID = 8
	got := Evaluate([]domain.Observation{up, end}, now, domain.DefaultConfig())
	for _, f := range got {
		if f.Rule == "U01" && (f.Healthy || f.Severity != "P2") {
			t.Fatal(f)
		}
	}
	if !hasRule(got, "U01") || hasRule(got, "Q05") {
		t.Fatal(got)
	}
}
func TestZeroPoolIsNotRateLimit(t *testing.T) {
	now := time.Now()
	o := observation("pool.snapshot", now)
	o.Metrics = map[string]float64{"total": 0, "eligible": 0, "model_rate_limited": 0}
	got := Evaluate([]domain.Observation{o}, now, domain.DefaultConfig())
	if !hasRule(got, "S01") || hasRule(got, "Q05") || hasRule(got, "U01") {
		t.Fatal(got)
	}
}
func TestActiveHeartbeatAndToolOutput(t *testing.T) {
	now := time.Now()
	o := observation("request.active", now)
	o.Metrics["elapsed_ms"] = 65000
	got := Evaluate([]domain.Observation{o}, now, domain.DefaultConfig())
	if !hasRule(got, "L02") {
		t.Fatal(got)
	}
	o.Metrics["has_output"] = 1
	o.Metrics["tool_output"] = 1
	o.Metrics["idle_ms"] = 1000
	got = Evaluate([]domain.Observation{o}, now, domain.DefaultConfig())
	if hasRule(got, "L02") || hasRule(got, "F02") {
		t.Fatal(got)
	}
}
func TestVideoPendingIsNotUnbilledCompletion(t *testing.T) {
	now := time.Now()
	o := observation("media.accepted", now.Add(-time.Hour))
	o.TaskID = "video1"
	got := Evaluate([]domain.Observation{o}, now, domain.DefaultConfig())
	if hasRule(got, "B01") {
		t.Fatal(got)
	}
	o.Kind = "media.completed"
	o.Metrics["deliverable"] = 1
	o.Metrics["billing_expected"] = 1
	got = Evaluate([]domain.Observation{o}, now, domain.DefaultConfig())
	if !hasRule(got, "B01") {
		t.Fatal(got)
	}
	billed := o
	billed.Kind = "billing.applied"
	billed.EventID = "bill"
	billed.Metrics = map[string]float64{"billing_confirmed": 1}
	got = Evaluate([]domain.Observation{o, billed}, now, domain.DefaultConfig())
	if hasRule(got, "B01") {
		t.Fatal(got)
	}
}
func TestSlowChannelHighAndLowVolume(t *testing.T) {
	now := time.Now()
	for _, n := range []int{5, 20} {
		var rows []domain.Observation
		for i := 0; i < n; i++ {
			o := observation("request.finished", now.Add(-6*time.Minute+time.Duration(i)*6*time.Minute/time.Duration(n-1)))
			o.EventID = fmt.Sprint(i)
			o.RequestID = fmt.Sprint(i)
			o.Outcome = "success"
			o.Metrics["first_effective_ms"] = 55000
			o.Metrics["has_output"] = 1
			rows = append(rows, o)
		}
		if got := Evaluate(rows, now, domain.DefaultConfig()); !hasRule(got, "L03") {
			t.Fatal(n, got)
		}
	}
}
func TestQuotaThresholdDisabledAndExplicitUpstreamQuota(t *testing.T) {
	now := time.Now()
	o := observation("quota.snapshot", now)
	o.Reason = "balance"
	o.UserID = 2
	o.Metrics["remaining"] = 1
	cfg := domain.DefaultConfig()
	if got := Evaluate([]domain.Observation{o}, now, cfg); hasRule(got, "Q02") {
		t.Fatal(got)
	}
	threshold := 5.
	cfg.BalanceLow = &threshold
	if got := Evaluate([]domain.Observation{o}, now, cfg); !hasRule(got, "Q02") {
		t.Fatal(got)
	}
	up := observation("attempt.failed", now)
	up.UpstreamStatus = 429
	up.Reason = "upstream_quota_exhausted"
	if got := Evaluate([]domain.Observation{up}, now, cfg); !hasRule(got, "Q05") {
		t.Fatal(got)
	}
}
func TestNoFalseRecoveryFromStaleOrSingleCancellation(t *testing.T) {
	now := time.Now()
	o := observation("request.cancelled", now)
	o.Outcome = "cancelled"
	if got := Evaluate([]domain.Observation{o}, now, domain.DefaultConfig()); hasRule(got, "C01") {
		t.Fatal(got)
	}
	o = observation("quota.snapshot", now)
	o.Reason = "balance"
	o.Quality = domain.Stale
	o.Metrics["remaining"] = 50
	for _, f := range Evaluate([]domain.Observation{o}, now, domain.DefaultConfig()) {
		if f.Healthy {
			t.Fatal("stale data recovered incident", f)
		}
	}
}

func TestThinkingStartAndPartialEvidenceDoNotCreateFalseFirstOutputAlarm(t *testing.T) {
	now := time.Now()
	o := observation("request.active", now)
	o.Metrics = map[string]float64{"elapsed_ms": 125000, "reasoning_started": 1}
	if hasRule(Evaluate([]domain.Observation{o}, now, domain.DefaultConfig()), "L02") {
		t.Fatal("thinking is not proof of first-output failure")
	}
	o.Metrics = map[string]float64{"elapsed_ms": 125000}
	o.Quality = domain.Partial
	if hasRule(Evaluate([]domain.Observation{o}, now, domain.DefaultConfig()), "L02") {
		t.Fatal("partial observation cannot prove no output")
	}
	o.Quality = domain.Fresh
	if !hasRule(Evaluate([]domain.Observation{o}, now, domain.DefaultConfig()), "L02") {
		t.Fatal("genuine waiting no longer alerted")
	}
}

func TestEarlyHiddenThinkingDoesNotBecomeSlowFirstOutputOnCompletion(t *testing.T) {
	now := time.Now()
	var rows []domain.Observation
	for n := 0; n < 3; n++ {
		o := observation("request.finished", now.Add(-time.Duration(3-n)*time.Second))
		o.RequestID = fmt.Sprintf("thinking-%d", n)
		o.Outcome = "success"
		o.Metrics = map[string]float64{"has_output": 1, "first_effective_ms": 118946, "reasoning_started": 1, "reasoning_started_ms": 4600, "pre_forward_ms": 847}
		rows = append(rows, o)
	}
	fs := Evaluate(rows, now, domain.DefaultConfig())
	if hasRule(fs, "L02") || hasRule(fs, "L03") {
		t.Fatal("normal hidden thinking misclassified as slow first output", fs)
	}
	rows[0].Metrics["reasoning_started_ms"] = 65000
	fs = Evaluate(rows[:1], now, domain.DefaultConfig())
	if !hasRule(fs, "L02") {
		t.Fatal("genuinely late start must remain observable", fs)
	}
}

func TestBudgetExhaustionHasSpecificCauseWithoutFirstOutputAlarm(t *testing.T) {
	now := time.Now()
	o := observation("request.finished", now)
	o.Outcome = "failed"
	o.Reason = "output_budget_exhausted"
	o.Metrics = map[string]float64{"reasoning_started": 1, "reasoning_started_ms": 1504, "elapsed_ms": 31136}
	fs := Evaluate([]domain.Observation{o}, now, domain.DefaultConfig())
	if !hasRule(fs, "F02") || hasRule(fs, "L02") {
		t.Fatal("budget failure must remain distinct from latency", fs)
	}
}

func TestPartialCompletedStreamCannotProveLateFirstOutput(t *testing.T) {
	now := time.Now()
	var rows []domain.Observation
	for n := 0; n < 3; n++ {
		o := observation("request.finished", now.Add(-time.Duration(n)*time.Second))
		o.RequestID = fmt.Sprintf("partial-%d", n)
		o.Quality = domain.Partial
		o.Outcome = "unknown"
		o.Metrics = map[string]float64{"has_output": 1, "first_effective_ms": 35100}
		rows = append(rows, o)
	}
	fs := Evaluate(rows, now, domain.DefaultConfig())
	if hasRule(fs, "L02") || hasRule(fs, "L03") {
		t.Fatal("partial protocol cannot prove no earlier output", fs)
	}
}
