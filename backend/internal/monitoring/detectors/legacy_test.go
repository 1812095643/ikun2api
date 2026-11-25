package detectors

import (
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"testing"
	"time"
)

func TestLegacyErrorProjectionRemainsActionable(t *testing.T) {
	for _, tt := range []struct {
		reason, rule string
		status       float64
	}{{"pool_empty", "S01", 503}, {"upstream_failure", "U02", 200}, {"upstream_failure", "F01", 502}, {"client_request_rejected", "F02", 400}, {"model_unavailable", "F02", 404}, {"request_failure", "F01", 502}, {"timeout", "U03", 502}, {"slot_timeout", "L01", 429}} {
		t.Run(tt.reason, func(t *testing.T) {
			o := domain.Observation{Schema: 1, Site: "s", Source: "postgres.errors", EventID: "1", RequestID: "r", Kind: "request.error", Reason: tt.reason, Quality: domain.Partial, OccurredAt: time.Now(), Metrics: map[string]float64{"intended_status": tt.status}}
			found := false
			for _, f := range Evaluate([]domain.Observation{o}, o.OccurredAt, domain.DefaultConfig()) {
				if f.Rule == tt.rule && !f.Healthy {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing %s for legacy %s", tt.rule, tt.reason)
			}
		})
	}
}

func TestCompletedSlowRequestsMergeByChannel(t *testing.T) {
	at := time.Now()
	rows := []domain.Observation{}
	for n := 0; n < 2; n++ {
		rows = append(rows, domain.Observation{Schema: 1, Site: "s", Source: "postgres.usage", EventID: fmt.Sprint(n), RequestID: fmt.Sprint(n), Kind: "usage.recorded", ChannelID: 7, Model: "model", Quality: domain.Partial, OccurredAt: at.Add(time.Duration(n) * time.Second), Metrics: map[string]float64{"client_first_token_ms": 55000}})
	}
	fs := Evaluate(rows, at.Add(time.Minute), domain.DefaultConfig())
	ids := map[string]bool{}
	hits := 0
	for _, f := range fs {
		if f.Rule == "L02" {
			hits++
			ids[f.Fingerprint] = true
		}
	}
	if hits != 2 || len(ids) != 1 {
		t.Fatalf("slow channel must retain two evidence rows in one incident: hits=%d incidents=%d", hits, len(ids))
	}
}

func TestLegacyHTTP200AttemptDoesNotClaimTerminalFailure(t *testing.T) {
	o := domain.Observation{Schema: 1, Site: "s", Source: "postgres.errors", EventID: "200", Kind: "request.error", Reason: "upstream_failure", Quality: domain.Partial, OccurredAt: time.Now(), Metrics: map[string]float64{"intended_status": 200}}
	found := false
	for _, f := range Evaluate([]domain.Observation{o}, o.OccurredAt, domain.DefaultConfig()) {
		if f.Rule == "F01" {
			t.Fatal("legacy attempt error with outer200 cannot prove final stream failure")
		}
		if f.Rule == "U02" && f.Severity == "P2" {
			found = true
		}
	}
	if !found {
		t.Fatal("unclassified upstream attempt must still notify")
	}
}
