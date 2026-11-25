package telemetry

import (
	"context"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
)

func TestEmptyThinkingIsUncertainNotEmptyOutput(t *testing.T) {
	start := time.Now()
	h := New("test", "test", "test", 10, 50)
	_, tr := h.Start(context.Background(), "req", start)
	p := NewParser(func(s Signal) { tr.Signal(s, start.Add(time.Second)) })
	p.Feed([]byte("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\"}}\n\n"))
	p.Feed([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"\"}}\n\n"))
	snap := h.Snapshot(0, start.Add(65*time.Second))
	if len(snap.Active) != 1 || snap.Active[0].N("reasoning_started") != 1 || snap.Active[0].N("has_output") != 0 {
		t.Fatal("thinking must be separate from effective output", snap.Active)
	}
	p.Feed([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	tr.Finish(200, false, start.Add(70*time.Second))
	snap = h.Snapshot(0, start.Add(70*time.Second))
	last := snap.Events[len(snap.Events)-1]
	if last.Outcome != "unknown" || last.Quality != domain.Partial {
		t.Fatal("empty reasoning cannot prove empty response or success", last)
	}
}
func TestNonemptyBlockStartCountsAsOutput(t *testing.T) {
	for _, data := range []string{`{"type":"content_block_start","content_block":{"type":"text","text":"hello"}}`, `{"type":"content_block_start","content_block":{"type":"thinking","thinking":"reason"}}`} {
		var signals []Signal
		p := NewParser(func(s Signal) { signals = append(signals, s) })
		p.JSON([]byte(data))
		found := false
		for _, s := range signals {
			if s.Output != "" {
				found = true
			}
		}
		if !found {
			t.Fatal("nonempty block output ignored")
		}
	}
}

func TestHiddenThinkingBudgetExhaustionIsExplicitFailure(t *testing.T) {
	start := time.Now()
	h := New("test", "test", "test", 10, 50)
	_, tr := h.Start(context.Background(), "budget", start)
	p := NewParser(func(s Signal) { tr.Signal(s, start.Add(time.Second)) })
	for _, frame := range []string{`{"type":"content_block_start","content_block":{"type":"thinking","thinking":""}}`, `{"type":"message_delta","delta":{"stop_reason":"max_tokens"}}`, `{"type":"message_stop"}`} {
		p.JSON([]byte(frame))
	}
	tr.Finish(200, false, start.Add(31*time.Second))
	events := h.Snapshot(0, start.Add(32*time.Second)).Events
	last := events[len(events)-1]
	if last.Outcome != "failed" || last.Reason != "output_budget_exhausted" {
		t.Fatalf("explicit budget exhaustion lost: outcome=%s reason=%s", last.Outcome, last.Reason)
	}
}

func TestBudgetStopWithActualAnswerIsNotEmptyAnswer(t *testing.T) {
	start := time.Now()
	h := New("test", "test", "test", 10, 50)
	_, tr := h.Start(context.Background(), "answer", start)
	p := NewParser(func(s Signal) { tr.Signal(s, start.Add(time.Second)) })
	for _, frame := range []string{`{"type":"content_block_delta","delta":{"type":"text_delta","text":"answer"}}`, `{"type":"message_delta","delta":{"stop_reason":"max_tokens"}}`, `{"type":"message_stop"}`} {
		p.JSON([]byte(frame))
	}
	tr.Finish(200, false, start.Add(120*time.Second))
	events := h.Snapshot(0, start.Add(121*time.Second)).Events
	last := events[len(events)-1]
	if last.Reason == "output_budget_exhausted" || last.Outcome != "success" || last.N("first_effective_ms") != 1000 {
		t.Fatal("long answered stream misclassified", last)
	}
}

func TestEmptyReasoningMarkersAcrossProtocols(t *testing.T) {
	for _, frame := range []string{`{"type":"response.output_item.added","item":{"type":"reasoning"}}`, `{"type":"response.reasoning_summary_text.delta","delta":""}`, `{"choices":[{"delta":{"reasoning_content":""}}]}`, `{"candidates":[{"content":{"parts":[{"thought":true,"text":""}]}}]}`} {
		marker, output := false, false
		p := NewParser(func(s Signal) { marker = marker || s.ReasoningStarted; output = output || s.Output != "" })
		p.JSON([]byte(frame))
		if !marker || output {
			t.Fatal("empty reasoning must be marked without counting as output", frame)
		}
	}
}
