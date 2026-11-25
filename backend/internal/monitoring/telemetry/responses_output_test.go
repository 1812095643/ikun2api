package telemetry

import (
	"context"
	"testing"
	"time"
)

func TestResponsesCompletedNestedOutputClassification(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"terminal_message", `{"type":"response.completed","response":{"output":[{"type":"message","content":[{"type":"output_text","text":"hello"}]}],"usage":{"output_tokens":3}}}`, "success"},
		{"terminal_function", `{"type":"response.completed","response":{"output":[{"type":"function_call","name":"lookup","arguments":"{}"}]}}`, "success"},
		{"usage_without_content", `{"type":"response.completed","response":{"output":[],"usage":{"output_tokens":209}}}`, "unknown"},
		{"empty", `{"type":"response.completed","response":{"output":[],"usage":{"output_tokens":0}}}`, "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			h := New("test", "test", "test", 10, 20)
			_, tr := h.Start(context.Background(), "x", now)
			p := NewParser(func(s Signal) { tr.Signal(s, now) })
			p.JSON([]byte(tc.body))
			tr.Finish(200, false, now)
			snap := h.Snapshot(0, now)
			last := snap.Events[len(snap.Events)-1]
			if last.Outcome != tc.want {
				t.Fatalf("outcome %q reason %q want %q", last.Outcome, last.Reason, tc.want)
			}
		})
	}
}
