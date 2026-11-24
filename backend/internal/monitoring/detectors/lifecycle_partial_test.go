package detectors

import (
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"testing"
	"time"
)

func TestPartialFinishedEndsWaitingWithoutClaimingSuccess(t *testing.T) {
	now := time.Now()
	o := observation("request.finished", now)
	o.Outcome = "unknown"
	o.ClientStatus = 200
	o.Quality = domain.Partial
	active := observation("request.active", now.Add(-time.Second))
	active.Metrics["elapsed_ms"] = 65000
	got := Evaluate([]domain.Observation{active, o}, now, domain.DefaultConfig())
	ended := false
	for _, f := range got {
		if f.Rule == "L02" && f.Healthy && f.Quality == domain.Fresh {
			ended = true
		}
		if (f.Rule == "F01" || f.Rule == "F02") && !f.Healthy {
			t.Fatal("unknown protocol is not proof of failure", f)
		}
	}
	if !ended {
		t.Fatal("finished request must end in-flight wait while preserving unknown outcome")
	}
}

func TestRequestLifecycleTimesAndOutcomes(t *testing.T) {
	start := time.Date(2026, 11, 14, 4, 45, 22, 0, time.UTC)
	for _, outcome := range []string{"success", "failed", "cancelled", "unknown"} {
		t.Run(outcome, func(t *testing.T) {
			active := observation("request.active", start.Add(31*time.Second))
			active.Source = "gateway"
			active.OccurredAt = start.Add(31 * time.Second)
			active.ObservedAt = active.OccurredAt
			active.Metrics = map[string]float64{"elapsed_ms": 31000}
			active.RequestID = "req"
			end := observation("request.finished", start.Add(155*time.Second))
			end.Source = "gateway"
			end.RequestID = "req"
			end.OccurredAt = start.Add(155 * time.Second)
			end.ObservedAt = end.OccurredAt
			end.Metrics = map[string]float64{"elapsed_ms": 155000, "first_effective_ms": 145000, "has_output": 1}
			end.Outcome = outcome
			if outcome == "unknown" {
				end.Quality = domain.Partial
			}
			if outcome == "cancelled" {
				end.Kind = "request.cancelled"
			}
			var prior []domain.Incident
			for _, f := range Evaluate([]domain.Observation{active}, active.OccurredAt, domain.DefaultConfig()) {
				if f.Rule == "L02" && !f.Healthy {
					prior = append(prior, domain.Incident{Rule: f.Rule, RuleVersion: f.RuleVersion, Fingerprint: f.Fingerprint + ":v1", Scope: f.Scope, LastFault: f.At})
				}
			}
			if len(prior) == 0 {
				t.Fatal("missing initial finding")
			}
			found := false
			for _, f := range Evaluate([]domain.Observation{end}, end.OccurredAt, domain.DefaultConfig(), prior...) {
				if f.Rule == "L02" && f.Healthy && f.RequestID == "req" {
					found = true
					if !f.RequestStartedAt.Equal(start) || !f.FirstOutputAt.Equal(start.Add(145*time.Second)) || !f.RequestEndedAt.Equal(end.OccurredAt) || f.RequestOutcome != outcome {
						t.Fatalf("incorrect timing/outcome: %+v", f)
					}
				}
			}
			if !found {
				t.Fatal("missing lifecycle close")
			}
		})
	}
}
