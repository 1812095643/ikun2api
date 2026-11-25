// Package runtime 负责嵌入式监控的有界采集、评估与后台任务。
package runtime

import (
	"context"
	"errors"
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/sources"
	"github.com/MACOS-DO/sub4api/internal/monitoring/store"
	"time"
)

type Clock interface{ Now() time.Time }
type WallClock struct{}

func (WallClock) Now() time.Time { return time.Now().UTC() }

type Collector struct {
	Report func(string, time.Time, error)
	Store  *store.Store
	Source sources.EvidenceSource
	Clock  Clock
	Site   string
}

func (c *Collector) Poll(ctx context.Context) (result error) {
	defer func() {
		if c.Report != nil {
			c.Report(c.Source.Name(), c.Clock.Now(), result)
		}
	}()
	now := c.Clock.Now()
	cur, err := c.Store.Cursor(ctx, c.Source.Name())
	if err != nil {
		return err
	}
	batch, err := c.Source.Poll(ctx, cur, now)
	if err != nil {
		cur.Quality = domain.Stale
		reason := "source_unavailable"
		if errors.Is(err, sources.ErrIncompatible) {
			cur.Quality = domain.Unsupported
			reason = "schema_incompatible"
		}
		o := domain.Observation{Schema: 1, Site: c.Site, Source: "monitor", EventID: fmt.Sprintf("%s:%d", c.Source.Name(), now.Unix()/30), Kind: "source.unavailable", Reason: reason, OccurredAt: now, ObservedAt: now, Quality: domain.Fresh, TaskID: c.Source.Name()}
		if saveErr := c.Store.Ingest(ctx, []domain.Observation{o}, cur); saveErr != nil {
			return saveErr
		}
		return err
	}
	if batch.Cursor.LastSuccess.After(cur.LastSuccess) && (batch.Cursor.Quality == domain.Fresh || batch.Cursor.Quality == domain.Partial) && (cur.Quality == domain.Stale || cur.Quality == domain.Unsupported || cur.Quality == domain.Partial && batch.Cursor.Quality == domain.Fresh) {
		batch.Observations = append(batch.Observations, domain.Observation{Schema: 1, Site: c.Site, Source: "monitor", EventID: fmt.Sprintf("recovered:%s:%d", c.Source.Name(), now.Unix()/5), Kind: "source.recovered", OccurredAt: now, ObservedAt: now, Quality: domain.Fresh, TaskID: c.Source.Name()})
	}
	if len(batch.Capabilities) > 0 {
		if err = c.Store.SaveState(ctx, "capabilities:"+c.Source.Name(), batch.Capabilities); err != nil {
			return err
		}
	}
	return c.Store.Ingest(ctx, batch.Observations, batch.Cursor)
}
func (c *Collector) Run(ctx context.Context, interval time.Duration) {
	_ = c.Poll(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = c.Poll(ctx)
		}
	}
}
