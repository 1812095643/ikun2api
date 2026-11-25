package runtime

import (
	"context"
	"sync"
	"time"

	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/incidents"
	"github.com/MACOS-DO/sub4api/internal/monitoring/notify"
	"github.com/MACOS-DO/sub4api/internal/monitoring/sources"
	"github.com/MACOS-DO/sub4api/internal/monitoring/store"
	"github.com/MACOS-DO/sub4api/internal/monitoring/telemetry"
)

// Native 嵌入主程序，复用业务数据库、SMTP 和关闭生命周期。
type Native struct {
	Store          *store.Store
	Manager        *incidents.Manager
	Worker         *notify.Worker
	Hub            *telemetry.Hub
	Health         Health
	Enabled        func() bool
	Site, Instance string
	Evidence       []sources.EvidenceSource
	cancel         context.CancelFunc
	wg             sync.WaitGroup
	stopOnce       sync.Once
	snapshotMu     sync.Mutex
	snapshotAt     time.Time
	snapshot       map[string]any
}

func (n *Native) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	n.cancel = cancel
	n.Manager = &incidents.Manager{Store: n.Store}
	n.wg.Add(2)
	go func() {
		defer n.wg.Done()
		source := &sources.InProcess{Hub: n.Hub, Instance: n.Instance}
		collector := Collector{Store: n.Store, Source: source, Clock: WallClock{}, Site: n.Site, Report: n.Health.Record}
		n.loop(ctx, 5*time.Second, func(ctx context.Context) { _ = collector.Poll(ctx) })
	}()
	go func() {
		defer n.wg.Done()
		evaluation := Evaluator{Store: n.Store, Manager: n.Manager, Clock: WallClock{}, Report: n.Health.Record}
		collectors := make([]*Collector, 0, len(n.Evidence))
		for _, source := range n.Evidence {
			collectors = append(collectors, &Collector{Store: n.Store, Source: source, Clock: WallClock{}, Site: n.Site, Report: n.Health.Record})
		}
		n.loop(ctx, 10*time.Second, func(ctx context.Context) {
			_, err := n.Store.WithEvaluationLock(ctx, func() error {
				for _, c := range collectors {
					if ctx.Err() != nil {
						return ctx.Err()
					}
					_ = c.Poll(ctx)
				}
				return evaluation.Poll(ctx)
			})
			if err != nil {
				n.Health.Record("evaluation", time.Now(), err)
			}
		})
	}()
	for index := 0; index < 4; index++ {
		n.wg.Add(1)
		go func() {
			defer n.wg.Done()
			n.loop(ctx, 500*time.Millisecond, func(ctx context.Context) {
				err := n.Worker.Tick(ctx, time.Now().UTC())
				n.Health.Record("notification", time.Now(), err)
			})
		}()
	}
}

func (n *Native) loop(ctx context.Context, interval time.Duration, run func(context.Context)) {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if n.Enabled == nil || n.Enabled() {
				call, cancel := context.WithTimeout(ctx, 45*time.Second)
				run(call)
				cancel()
			}
			timer.Reset(interval)
		}
	}
}

func (n *Native) Stop() {
	if n == nil {
		return
	}
	n.stopOnce.Do(func() {
		if n.cancel != nil {
			n.cancel()
		}
	})
	n.wg.Wait()
}

func (n *Native) Snapshot(ctx context.Context) (map[string]any, error) {
	n.snapshotMu.Lock()
	defer n.snapshotMu.Unlock()
	if n.snapshot != nil && time.Since(n.snapshotAt) < 5*time.Second {
		return n.snapshot, nil
	}
	cursors, err := n.Store.Cursors(ctx)
	if err != nil {
		return nil, err
	}
	queue, err := n.Store.QueueHealth(ctx)
	if err != nil {
		return nil, err
	}
	ready := true
	for _, c := range cursors {
		if c.Source == "notification" {
			continue
		}
		if c.Quality != domain.Fresh || c.LastSuccess.IsZero() || time.Since(c.LastSuccess) > 2*time.Minute {
			ready = false
		}
	}
	runtimeHealth := n.Health.Snapshot(time.Now())
	n.snapshot = map[string]any{"health": runtimeHealth, "sources": cursors, "queue": queue, "complete": ready && len(cursors) > 0 && runtimeHealth.Ready}
	n.snapshotAt = time.Now()
	return n.snapshot, nil
}
