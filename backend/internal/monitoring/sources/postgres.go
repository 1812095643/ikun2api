// 只读取固定证据视图；游标分页和查询超时限制后台采集开销。
package sources

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	_ "github.com/lib/pq"
	"time"
)

var ViewNames = []string{"errors", "attempts", "usage", "accounts", "balances", "subscriptions", "payments", "holds", "system", "billing", "reconciliation", "metrics", "dependencies"}

func IsSnapshot(view string) bool {
	switch view {
	case "accounts", "balances", "subscriptions", "payments", "holds", "reconciliation":
		return true
	}
	return false
}

type Postgres struct {
	DB          *sql.DB
	Timezone    string
	View, Site  string
	ActivatedAt time.Time
	Lookback    time.Duration
	Reconcile   bool
}
type pageCursor struct {
	Since    time.Time `json:"since"`
	ID       string    `json:"id"`
	Boundary time.Time `json:"boundary"`
	NextPoll time.Time `json:"next_poll"`
}

func OpenPostgres(dsn string) (*sql.DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err == nil {
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
		db.SetConnMaxLifetime(5 * time.Minute)
	}
	return db, err
}
func (p *Postgres) Name() string {
	if p.Reconcile {
		return "postgres." + p.View + ".reconcile"
	}
	return "postgres." + p.View
}
func (p *Postgres) Poll(ctx context.Context, c domain.Cursor, now time.Time) (Batch, error) {
	allowed := false
	for _, v := range ViewNames {
		if v == p.View {
			allowed = true
		}
	}
	if !allowed {
		return Batch{}, errors.New("unsupported evidence view")
	}
	var pos pageCursor
	if c.Watermark != "" {
		if json.Unmarshal([]byte(c.Watermark), &pos) != nil {
			return Batch{}, ErrIncompatible
		}
	}
	if (p.Reconcile || IsSnapshot(p.View)) && now.Before(pos.NextPoll) {
		return Batch{Cursor: c}, nil
	}
	lookback := p.Lookback
	if lookback == 0 {
		lookback = 2 * time.Minute
	}
	if pos.Since.IsZero() {
		pos.Since = now.Add(-lookback)
	}
	if pos.Boundary.IsZero() {
		pos.Boundary = now
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	tx, err := p.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Batch{}, ErrSourceUnavailable
	}
	defer tx.Rollback()
	zone := p.Timezone
	if zone == "" {
		zone = "Asia/Shanghai"
	}
	if _, err = tx.ExecContext(ctx, "SELECT set_config('TimeZone',$1,true)", zone); err != nil {
		return Batch{}, ErrIncompatible
	}

	if _, err = tx.ExecContext(ctx, "SET LOCAL statement_timeout='3000ms'"); err != nil {
		return Batch{}, ErrSourceUnavailable
	}
	query := fmt.Sprintf("SELECT id::text,observed_at,evidence FROM ops_monitor_v1_%s WHERE (observed_at,id::text)>($1,$2) AND observed_at<=$3 ORDER BY observed_at,id::text LIMIT 500", p.View)
	args := []any{pos.Since, pos.ID, pos.Boundary}
	if p.View != "attempts" && p.View != "dependencies" && p.View != "holds" {
		id := pos.ID
		if id == "" {
			id = "0"
		}
		query = fmt.Sprintf("SELECT id::text,observed_at,evidence FROM ops_monitor_v1_%s WHERE (observed_at,id)>($1,$2::bigint) AND observed_at<=$3 ORDER BY observed_at,id LIMIT 500", p.View)
		args = []any{pos.Since, id, pos.Boundary}
	}
	if IsSnapshot(p.View) {
		query = fmt.Sprintf("SELECT id::text,observed_at,evidence FROM ops_monitor_v1_%s WHERE id::text>$1 ORDER BY id::text LIMIT 500", p.View)
		args = []any{pos.ID}
		if p.View != "holds" {
			id := pos.ID
			if id == "" {
				id = "0"
			}
			query = fmt.Sprintf("SELECT id::text,observed_at,evidence FROM ops_monitor_v1_%s WHERE id>$1::bigint ORDER BY id LIMIT 500", p.View)
			args = []any{id}
		}
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return Batch{}, ErrIncompatible
	}
	out := []domain.Observation{}
	count := 0
	for rows.Next() {
		var id string
		var at time.Time
		var b []byte
		var o domain.Observation
		if err = rows.Scan(&id, &at, &b); err != nil {
			rows.Close()
			return Batch{}, ErrIncompatible
		}
		if json.Unmarshal(b, &o) != nil {
			rows.Close()
			return Batch{}, ErrIncompatible
		}
		o.Schema = 1
		o.Site = p.Site
		o.Source = "postgres." + p.View
		o.EventID = fmt.Sprintf("%s:%d", id, at.UnixMicro())
		o.OccurredAt = at
		o.ObservedAt = now
		o.Quality = domain.Partial
		if IsSnapshot(p.View) {
			o.OccurredAt = pos.Boundary
			o.EventID = fmt.Sprintf("%s:%d", id, pos.Boundary.UnixMilli())
			o.Quality = domain.Fresh
		} else if p.View == "billing" || p.View == "metrics" || p.View == "dependencies" || p.View == "usage" {
			o.Quality = domain.Fresh
		} else if at.Before(p.ActivatedAt) {
			o.Quality = domain.Historical
		}
		domain.NormalizeIdentifiers(&o)
		if err = o.Validate(); err != nil {
			rows.Close()
			return Batch{}, ErrIncompatible
		}
		out = append(out, o)
		pos.Since = at
		pos.ID = id
		count++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return Batch{}, ErrSourceUnavailable
	}
	if err = tx.Commit(); err != nil {
		return Batch{}, ErrSourceUnavailable
	}
	if count < 500 {
		pos.Since = pos.Boundary.Add(-lookback)
		pos.ID = ""
		pos.Boundary = time.Time{}
		if p.Reconcile {
			pos.NextPoll = now.Add(time.Hour)
		}
		if IsSnapshot(p.View) {
			pos.NextPoll = now.Add(time.Minute)
		}
	}
	b, _ := json.Marshal(pos)
	c = domain.Cursor{Source: p.Name(), Watermark: string(b), LastSuccess: now, Quality: domain.Fresh}
	return Batch{Observations: out, Cursor: c}, nil
}
