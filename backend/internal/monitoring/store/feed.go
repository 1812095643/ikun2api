package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"time"
)

// Page drains the durable arrival sequence, independently of the aggregate window.
// A busy window can therefore degrade statistics without silently losing each failure.
func (s *Store) Page(ctx context.Context, after int64, limit int) ([]domain.Observation, int64, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT seq,body FROM observations WHERE seq>? ORDER BY seq LIMIT ?", after, bounded(limit))
	if err != nil {
		return nil, after, err
	}
	defer rows.Close()
	out := []domain.Observation{}
	last := after
	for rows.Next() {
		var b []byte
		var o domain.Observation
		if err = rows.Scan(&last, &b); err != nil {
			return nil, after, err
		}
		if err = json.Unmarshal(b, &o); err != nil {
			return nil, after, err
		}
		out = append(out, o)
	}
	return out, last, rows.Err()
}
func (s *Store) Backlog(ctx context.Context, now time.Time) ([]domain.Observation, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT channel,MIN(CASE WHEN state IN ('pending','leased') THEN next_attempt END) FROM notification_outbox WHERE json_extract(body,'$.payload.rule_id')!='M02' GROUP BY channel")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Observation{}
	for rows.Next() {
		var channel string
		var oldest sql.NullInt64
		if err = rows.Scan(&channel, &oldest); err != nil {
			return nil, err
		}
		age := float64(0)
		if oldest.Valid {
			age = now.Sub(time.UnixMilli(oldest.Int64)).Seconds()
		}
		metrics := map[string]float64{"oldest_pending_seconds": age}
		if age <= 60 {
			metrics["queue_cleared"] = 1
		}
		out = append(out, domain.Observation{Kind: "delivery.health", TaskID: "queue:" + channel, Metrics: metrics})
	}
	return out, rows.Err()
}

// UsagePercent excludes reusable pages; warns before the independent SQLite
// page budget is exhausted even when the host still has ample free disk.
func (s *Store) UsagePercent(ctx context.Context) (float64, error) {
	var pages, free, max int64
	for _, q := range []struct {
		sql   string
		value *int64
	}{{"PRAGMA page_count", &pages}, {"PRAGMA freelist_count", &free}, {"PRAGMA max_page_count", &max}} {
		if err := s.db.QueryRowContext(ctx, q.sql).Scan(q.value); err != nil {
			return 0, err
		}
	}
	if max <= 0 {
		return 0, nil
	}
	return float64(pages-free) * 100 / float64(max), nil
}
