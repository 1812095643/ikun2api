package runtime

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestHealthDoesNotHideErrorsOrStaleLoops(t *testing.T) {
	now := time.Now()
	h := &Health{}
	h.Record("evaluation", now, nil)
	if !h.Snapshot(now).Ready {
		t.Fatal("successful loop not ready")
	}
	h.Record("evaluation", now, errors.New("private-secret"))
	state := h.Snapshot(now)
	b, _ := json.Marshal(state)
	if state.Ready || strings.Contains(string(b), "private-secret") {
		t.Fatal(string(b))
	}
	h.Record("evaluation", now, nil)
	if h.Snapshot(now.Add(3 * time.Minute)).Ready {
		t.Fatal("stalled loop healthy")
	}
}
