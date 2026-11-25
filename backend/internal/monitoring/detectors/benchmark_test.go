package detectors

import (
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"testing"
	"time"
)

func BenchmarkEvaluateBoundedWindow(b *testing.B) {
	now := time.Now()
	cfg := domain.DefaultConfig()
	rows := make([]domain.Observation, 20000)
	for n := range rows {
		rows[n] = domain.Observation{Schema: 1, Site: "test", Source: "gateway", EventID: fmt.Sprint(n), RequestID: fmt.Sprint(n), AccountID: int64(n%100 + 1), GroupID: int64(n%10 + 1), Model: "fixture", Kind: "request.finished", Outcome: "success", Quality: domain.Fresh, OccurredAt: now.Add(-time.Duration(n) * time.Millisecond), Metrics: map[string]float64{"terminal": 1, "first_ms": 1000, "attempts": 1, "elapsed_ms": 3000}}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		Evaluate(rows, now, cfg)
	}
}
