package detectors

import (
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"testing"
	"time"
)

func TestIncompleteRecoveryNeverBecomesFailure(t *testing.T) {
	now := time.Now()
	for _, kind := range []string{"dependency.health", "account.recovered"} {
		o := observation(kind, now)
		o.Quality = domain.Partial
		o.Metrics["healthy"] = 1
		for _, f := range Evaluate([]domain.Observation{o}, now, domain.DefaultConfig()) {
			if !f.Healthy {
				t.Fatal("recovery converted into failure", f)
			}
		}
	}
	o := observation("attempt.failed", now)
	o.Quality = domain.Partial
	o.UpstreamStatus = 429
	if !hasRule(Evaluate([]domain.Observation{o}, now, domain.DefaultConfig()), "U01") {
		t.Fatal("explicit 429 incorrectly suppressed")
	}
}
func TestMissingFulfillmentIsNotConfirmedUnfulfilled(t *testing.T) {
	now := time.Now()
	o := observation("payment.snapshot", now)
	o.TaskID = "paid"
	o.Metrics = map[string]float64{"paid": 1, "paid_at_ms": float64(now.Add(-time.Hour).UnixMilli())}
	if hasRule(Evaluate([]domain.Observation{o}, now, domain.DefaultConfig()), "Q03") {
		t.Fatal("missing field treated as false")
	}
	o.Metrics["fulfilled"] = 0
	if !hasRule(Evaluate([]domain.Observation{o}, now, domain.DefaultConfig()), "Q03") {
		t.Fatal("explicit unfulfilled missed")
	}
}
