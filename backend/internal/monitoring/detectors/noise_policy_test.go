package detectors

import (
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"testing"
	"time"
)

func TestSustainedLatencyPolicy(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name, model string
		n           int
		ms          float64
		span        time.Duration
		want        bool
	}{
		{"normal GPT", "gpt-6.1-sol", 20, 30000, 6 * time.Minute, false},
		{"old p95 bypass", "gpt-6.1-sol", 20, 50000, 6 * time.Minute, false},
		{"few slow", "gpt-6.1-sol", 4, 90000, 6 * time.Minute, false},
		{"burst", "gpt-6.1-sol", 10, 90000, time.Minute, false},
		{"persistent", "gpt-6.1-sol", 10, 90000, 6 * time.Minute, true},
		{"other text", "claude-opus-5-5", 10, 40000, 6 * time.Minute, true},
		{"image", "gpt-image-2", 10, 180000, 6 * time.Minute, false},
		{"unknown", "", 10, 180000, 6 * time.Minute, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var rows []domain.Observation
			for j := 0; j < tc.n; j++ {
				o := observation("request.finished", now.Add(-tc.span*time.Duration(j)/time.Duration(tc.n-1)))
				o.Model = tc.model
				o.RequestID = fmt.Sprint(j)
				o.EventID = o.RequestID
				o.Metrics = map[string]float64{"has_output": 1, "first_effective_ms": tc.ms}
				rows = append(rows, o)
			}
			if got := hasRule(Evaluate(rows, now, domain.DefaultConfig()), "L03"); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestStreamAndTerminalFailureShareIncident(t *testing.T) {
	now := time.Now()
	stream := observation("stream.failed", now)
	stream.Reason = "stream_error"
	end := observation("request.finished", now.Add(time.Second))
	end.Outcome = "failed"
	end.ClientStatus = 502
	findings := Evaluate([]domain.Observation{stream, end}, now.Add(time.Second), domain.DefaultConfig())
	fps := map[string]bool{}
	n := 0
	for _, f := range findings {
		if f.Rule == "F01" && !f.Healthy {
			n++
			fps[f.Fingerprint] = true
		}
	}
	if n != 2 || len(fps) != 1 {
		t.Fatal("retain both facts in one incident", n, fps)
	}
}
