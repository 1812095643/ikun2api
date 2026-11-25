package sources

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/telemetry"
)

// 进程内读取有界观测，不新增监听端口、内部鉴权 Token 或 HTTP 轮询。
type InProcess struct {
	Hub      *telemetry.Hub
	Instance string
}

func (s *InProcess) Name() string { return "gateway." + s.Instance }
func (s *InProcess) Poll(_ context.Context, c domain.Cursor, now time.Time) (Batch, error) {
	if s.Hub == nil {
		return Batch{}, ErrSourceUnavailable
	}
	after, _ := strconv.ParseUint(c.Watermark, 10, 64)
	snap := s.Hub.Snapshot(after, now)
	if c.BootID != "" && c.BootID != snap.BootID {
		after = 0
		snap = s.Hub.Snapshot(0, now)
	}
	gap := c.BootID != "" && c.BootID != snap.BootID || snap.Dropped > c.Dropped || after > 0 && snap.Oldest > after+1
	rows := append([]domain.Observation{}, snap.Events...)
	// 一次最多读取环形队列容量，避免采集能力低于请求完成速率。
	for page := 1; page < 8 && len(snap.Events) == 1024; page++ {
		next := s.Hub.Snapshot(snap.Sequence, now)
		if next.Sequence <= snap.Sequence {
			break
		}
		rows = append(rows, next.Events...)
		snap = next
	}
	rows = append(rows, snap.Active...)
	quality := domain.Fresh
	if gap {
		quality = domain.Partial
		rows = append(rows, domain.Observation{Schema: 1, Site: snap.Site, Source: s.Name(), EventID: fmt.Sprintf("gap:%s:%d", snap.BootID, snap.Sequence), Kind: "source.gap", TaskID: s.Name(), Reason: "bounded_observation_gap", OccurredAt: now, ObservedAt: now, Quality: domain.Partial})
	}
	return Batch{Observations: rows, Capabilities: snap.Capabilities, Cursor: domain.Cursor{Source: s.Name(), Watermark: strconv.FormatUint(snap.Sequence, 10), BootID: snap.BootID, LastSuccess: now, Quality: quality, Dropped: snap.Dropped}}, nil
}
