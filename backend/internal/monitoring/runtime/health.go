package runtime

import (
	"sync"
	"time"
)

type Health struct {
	mu    sync.Mutex
	loops map[string]LoopStatus
}
type LoopStatus struct {
	LastAttempt time.Time `json:"last_attempt"`
	LastSuccess time.Time `json:"last_success"`
	Failed      bool      `json:"failed"`
}
type HealthSnapshot struct {
	Ready bool                  `json:"ready"`
	Loops map[string]LoopStatus `json:"loops"`
}

func (h *Health) Record(name string, now time.Time, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.loops == nil {
		h.loops = map[string]LoopStatus{}
	}
	v := h.loops[name]
	v.LastAttempt = now
	v.Failed = err != nil
	if err == nil {
		v.LastSuccess = now
	}
	h.loops[name] = v
}
func (h *Health) Snapshot(now time.Time) HealthSnapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := HealthSnapshot{Ready: len(h.loops) > 0, Loops: map[string]LoopStatus{}}
	for name, v := range h.loops {
		out.Loops[name] = v
		if v.Failed || v.LastSuccess.IsZero() || now.Sub(v.LastAttempt) > 2*time.Minute {
			out.Ready = false
		}
	}
	return out
}
