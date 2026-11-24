package telemetry

import (
	"context"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"strings"
	"testing"
	"time"
)

func TestInFlightHeartbeatAndTerminal(t *testing.T) {
	now := time.Now()
	h := New("test", "instance", "build", 4, 8)
	ctx, track := h.Start(context.Background(), "request1", now)
	p := NewParser(func(s Signal) { track.Signal(s, now) })
	p.Feed([]byte(": ping\n\ndata: {}\n\n"))
	snap := h.Snapshot(0, now.Add(65*time.Second))
	if len(snap.Active) != 1 || snap.Active[0].N("first_effective_ms") != 0 || snap.Active[0].N("elapsed_ms") < 65000 {
		t.Fatal("heartbeat masked slow request", snap)
	}
	now = now.Add(66 * time.Second)
	p.Feed([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"function\":{\"name\":\"f\"}}]}}]}\n\ndata: [DONE]\n\n"))
	track.Finish(200, false, now)
	if FromContext(ctx) != track {
		t.Fatal("context lost tracker")
	}
	snap = h.Snapshot(0, now)
	if len(snap.Active) != 0 {
		t.Fatal("active leak")
	}
	last := snap.Events[len(snap.Events)-1]
	if last.Outcome != "success" || last.N("tool_output") != 1 || last.N("first_effective_ms") != 66000 {
		t.Fatal(last)
	}
}
func TestProtocolSignals(t *testing.T) {
	for _, tc := range []struct {
		name, body              string
		effective, failed, done bool
	}{
		{"sse error", "event: error\ndata: {\"error\":{\"message\":\"secret prompt\"}}\n\n", false, true, false},
		{"responses failure", "data: {\"type\":\"response.failed\"}\n\n", false, true, false},
		{"reasoning", "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"private\"}}\n\ndata: {\"type\":\"message_stop\"}\n\n", true, false, true},
		{"gemini tool", "data: {\"candidates\":[{\"content\":{\"parts\":[{\"functionCall\":{\"name\":\"f\"}}]},\"finishReason\":\"STOP\"}]}\n\n", true, false, true},
		{"ping", "event: ping\ndata: {\"type\":\"ping\"}\n\n", false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var signals []Signal
			p := NewParser(func(s Signal) { signals = append(signals, s) })
			for _, c := range []byte(tc.body) {
				p.Feed([]byte{c})
			}
			effective, failed, done := false, false, false
			for _, s := range signals {
				effective = effective || s.Output != ""
				failed = failed || s.Failed
				done = done || s.Done
			}
			if effective != tc.effective || failed != tc.failed || done != tc.done {
				t.Fatal(signals)
			}
		})
	}
}
func TestBoundedCapacityAndLoss(t *testing.T) {
	h := New("test", "i", "b", 1, 2)
	now := time.Now()
	_, a := h.Start(context.Background(), "a", now)
	_, b := h.Start(context.Background(), "b", now)
	b.Finish(502, false, now)
	a.Finish(200, false, now)
	s := h.Snapshot(0, now)
	if s.Dropped == 0 || len(s.Events) > 2 {
		t.Fatal("loss invisible", s)
	}
	for _, o := range s.Events {
		if e := o.Validate(); e != nil {
			t.Fatal(e)
		}
	}
}
func TestOversizeFrameDegradesWithoutLeaking(t *testing.T) {
	var seen []Signal
	p := NewParser(func(s Signal) { seen = append(seen, s) })
	p.Feed([]byte("data: " + strings.Repeat("sensitive", 20000) + "\n\n"))
	if len(seen) == 0 || !seen[0].Partial {
		t.Fatal("parser drop invisible")
	}
}

func TestSnapshotPaginationDoesNotSkipEvents(t *testing.T) {
	h := New("test", "i", "b", 1, 4096)
	now := time.Now()
	for i := 0; i < 1500; i++ {
		h.Emit(domain.Observation{Kind: "pool.snapshot", OccurredAt: now})
	}
	first := h.Snapshot(0, now)
	if len(first.Events) != 1024 || first.Sequence != 1024 {
		t.Fatal("unbounded page", len(first.Events), first.Sequence)
	}
	second := h.Snapshot(first.Sequence, now)
	if len(second.Events) != 476 || second.Sequence != 1500 {
		t.Fatal("cursor skipped events", len(second.Events), second.Sequence)
	}
}
func BenchmarkObservation(b *testing.B) {
	h := New("test", "i", "b", 4096, 4096)
	for i := 0; i < b.N; i++ {
		_, tr := h.Start(context.Background(), "request", time.Now())
		tr.Update(domain.Observation{AccountID: 1, Metrics: map[string]float64{"attempts": 1}})
		tr.Finish(200, false, time.Now())
	}
}

func TestSegmentTimingsDoNotDoubleCountPreForward(t *testing.T) {
	now := time.Now()
	h := New("test", "fixture", "test", 4, 32)
	_, tr := h.Start(context.Background(), "r", now)
	tr.Queue(true, "user", now.Add(5*time.Second))
	tr.Queue(false, "user", now.Add(15*time.Second))
	tr.Queue(true, "account", now.Add(20*time.Second))
	tr.Queue(false, "account", now.Add(30*time.Second))
	tr.Attempt(1, "model", now.Add(35*time.Second))
	tr.Attempt(2, "model", now.Add(45*time.Second))
	tr.Signal(Signal{Supported: true, Output: "content", Done: true}, now.Add(60*time.Second))
	tr.Finish(200, false, now.Add(70*time.Second))
	events := h.Snapshot(0, now.Add(70*time.Second)).Events
	last := events[len(events)-1]
	if last.N("queue_ms") != 20000 || last.N("pre_forward_ms") != 45000 || last.N("first_effective_ms") != 60000 || last.N("attempts") != 2 {
		t.Fatal(last)
	}
}
