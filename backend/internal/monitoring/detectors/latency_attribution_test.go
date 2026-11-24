package detectors

import (
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLatencyAttributionDoesNotConfuseFirstEventWithOutput(t *testing.T) {
	now := time.Now()
	o := observation("request.finished", now)
	o.Metrics = map[string]float64{"sample_first_ms": 50000, "first_effective_ms": 52000, "pre_forward_ms": 2000, "reasoning_started": 1, "reasoning_started_ms": 51000}
	o.Outcome = "success"
	u := o
	u.Kind = "usage.recorded"
	u.EventID = "usage1"
	u.RequestID = "client:" + o.RequestID
	u.Metrics = map[string]float64{"context_input_tokens": 1, "context_cache_read_tokens": 749000, "context_cache_write_tokens": 999, "first_event_semantics": 1, "client_first_token_ms": 15000, "reasoning_configured": 1}
	e := evaluation{rows: []domain.Observation{u}}
	f := domain.Finding{}
	e.explainLatency(&f, []domain.Observation{o}, 30000)
	text := strings.Join(f.Explanation.Facts, "\n")
	for _, part := range []string{"75.0万token", "1000 token", "15.0秒", "52.0秒", "37.0秒", "不包含标记之前"} {
		if !strings.Contains(text, part) {
			t.Fatal(text)
		}
	}
	if !strings.Contains(strings.Join(f.Explanation.Inferences, ""), "不能将全部等待归因") {
		t.Fatal(f)
	}
}
func TestLatencyAttributionRejectsMismatchedAndAmbiguousUsage(t *testing.T) {
	for _, mode := range []string{"missing", "other_account", "duplicate", "partial"} {
		t.Run(mode, func(t *testing.T) {
			o := observation("request.finished", time.Now())
			o.Metrics = map[string]float64{"sample_first_ms": 50000}
			u := o
			u.Kind = "usage.recorded"
			u.EventID = "usage1"
			u.Metrics = map[string]float64{"context_input_tokens": 800000, "context_cache_read_tokens": 0, "context_cache_write_tokens": 0}
			rows := []domain.Observation{u}
			switch mode {
			case "missing":
				rows = nil
			case "other_account":
				rows[0].AccountID++
			case "partial":
				rows[0].Quality = domain.Partial
			case "duplicate":
				v := u
				v.EventID = "usage2"
				rows = append(rows, v)
			}
			e := evaluation{rows: rows}
			f := domain.Finding{}
			e.explainLatency(&f, []domain.Observation{o}, 30000)
			facts := strings.Join(f.Explanation.Facts, " ")
			if strings.Contains(facts, "80.0万") || !strings.Contains(facts, "精确关联用量0个") {
				t.Fatal(facts)
			}
		})
	}
}

func TestNonStreamingCompletionIsNotFirstToken(t *testing.T) {
	now := time.Now()
	var rows []domain.Observation
	for n := 0; n < 6; n++ {
		o := observation("request.finished", now.Add(-time.Duration(n)*time.Minute))
		o.RequestID = strconv.Itoa(n)
		o.EventID = "end" + o.RequestID
		o.Metrics = map[string]float64{"first_effective_ms": 120000, "has_output": 1}
		u := o
		u.EventID = "usage" + o.RequestID
		u.Kind = "usage.recorded"
		u.RequestID = "client:" + o.RequestID
		u.Metrics = map[string]float64{"request_stream": 0}
		rows = append(rows, o, u)
	}
	if hasRule(Evaluate(rows, now, domain.DefaultConfig()), "L03") {
		t.Fatal("nonstream completion misreported as slow first token")
	}
}
