package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"time"
)

func (s *Store) Page(ctx context.Context, after int64, limit int) ([]domain.Observation, int64, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT seq,body FROM ops_monitor_observations WHERE seq>$1 ORDER BY seq LIMIT $2", after, bounded(limit))
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
	rows, err := s.db.QueryContext(ctx, "SELECT channel,MIN(CASE WHEN state IN ('pending','leased') THEN next_attempt END) FROM ops_monitor_notification_outbox WHERE (body #>> '{payload,rule_id}')!='M02' AND state IN ('pending','leased','dead','uncertain') GROUP BY channel")
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
