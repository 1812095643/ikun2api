package telemetry

import (
	"context"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"time"
)

func Pool(ctx context.Context, group *int64, model string, total, eligible int, reasons map[string]float64) {
	t := FromContext(ctx)
	if t == nil {
		return
	}
	o := domain.Observation{Model: model, Metrics: map[string]float64{"total": float64(total), "eligible": float64(eligible)}}
	if group != nil {
		o.GroupID = *group
	}
	for k, v := range reasons {
		o.Metrics[k] = v
	}
	t.Update(o)
	t.Event("pool.snapshot", "scheduler_counts", 0, 0, time.Now())
}
