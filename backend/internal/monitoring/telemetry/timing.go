package telemetry

import (
	"github.com/MACOS-DO/sub4api/internal/pkg/requesttiming"
	"time"
)

func (t *Tracker) Forward(now time.Time) {
	if t == nil {
		return
	}
	t.hub.mu.Lock()
	defer t.hub.mu.Unlock()
	// 最新一次真正转发的时点，与选号时点分别记录；之前的尝试可在连接详情中追溯。
	t.observation.Metrics["pre_forward_ms"] = float64(now.Sub(t.start).Milliseconds())
}

func (t *Tracker) Timing(s requesttiming.Snapshot) {
	if t == nil {
		return
	}
	t.hub.mu.Lock()
	defer t.hub.mu.Unlock()
	t.observation.Metrics["upload_ms"] = s.BodyReadMS
	t.observation.Metrics["downstream_write_ms"] = s.DownstreamWriteMS
	if s.Truncated {
		t.partial = true
	}
}
