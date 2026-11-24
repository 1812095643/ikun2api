package telemetry

import (
	"context"
	"encoding/json"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"strings"
	"testing"
	"time"
)

func TestNoPayloadOrCredentialInEvidence(t *testing.T) {
	h := New("test", "instance", "build", 5, 20)
	now := time.Now()
	_, tr := h.Start(context.Background(), "https://user:password@example.test/?token=secret", now)
	tr.Update(domain.Observation{Model: "模型?credential=secret"})
	p := NewParser(func(s Signal) { tr.Signal(s, now) })
	p.Feed([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"private-customer-content\"},\"finish_reason\":\"stop\"}]}\n\n"))
	tr.Finish(200, false, now)
	snap := h.Snapshot(0, now)
	b, _ := json.Marshal(snap)
	for _, secret := range []string{"password", "secret", "private-customer-content", "example.test", "模型"} {
		if strings.Contains(string(b), secret) {
			t.Fatal("payload leaked", secret)
		}
	}
	for _, o := range snap.Events {
		if err := o.Validate(); err != nil {
			t.Fatal("sanitized identity invalid", err)
		}
	}
}
func TestCancelledAndMissingTerminalRemainDistinct(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		h := New("test", "i", "b", 2, 10)
		now := time.Now()
		_, tr := h.Start(context.Background(), "r", now)
		tr.Signal(Signal{Output: "content", Supported: true}, now)
		tr.Finish(200, cancelled, now)
		s := h.Snapshot(0, now)
		last := s.Events[len(s.Events)-1]
		if cancelled && last.Outcome != "cancelled" {
			t.Fatal(last)
		}
		if !cancelled && last.Reason != "missing_terminal" {
			t.Fatal(last)
		}
	}
}
