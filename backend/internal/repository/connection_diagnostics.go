package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	infraerrors "github.com/MACOS-DO/sub4api/internal/pkg/errors"
	"github.com/MACOS-DO/sub4api/internal/pkg/requesttiming"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/lib/pq"
)

const connectionTraceColumns = `started_at,trace_id,request_id,client_request_id,user_id,api_key_id,account_id,group_id,model,path,transport,state,status,total_ms,first_byte_ms,auth_ms,routing_ms,upstream_ms,first_token_ms,request_bytes,response_bytes,error_type`

// 每批一次事务和 COPY，转发线程不执行 SQL，也不序列化详情。
func (r *opsRepository) InsertConnectionTraces(ctx context.Context, records []service.ConnectionTrace) error {
	if len(records) == 0 {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PrepareContext(ctx, pq.CopyIn("connection_traces", strings.Split(connectionTraceColumns+",detail", ",")...))
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	for _, v := range records {
		detail, encodeErr := service.EncodeConnectionDetail(v.Detail)
		if encodeErr != nil {
			return encodeErr
		}
		_, err = stmt.ExecContext(ctx, v.StartedAt, v.TraceID, v.RequestID, v.ClientRequestID, v.UserID, v.APIKeyID, v.AccountID, v.GroupID, v.Model, v.Path, v.Transport, v.State, v.Status, v.TotalMS, v.FirstByteMS, v.AuthMS, v.RoutingMS, v.UpstreamMS, v.FirstTokenMS, v.RequestBytes, v.ResponseBytes, v.ErrorType, detail)
		if err != nil {
			return err
		}
	}
	if _, err = stmt.ExecContext(ctx); err != nil {
		return err
	}
	if err = stmt.Close(); err != nil {
		return err
	}
	return tx.Commit()
}

func connectionTraceWhere(f service.ConnectionTraceFilter) (string, []any) {
	args := []any{f.Start, f.End}
	parts := []string{"started_at >= $1", "started_at < $2"}
	add := func(column string, value any) {
		args = append(args, value)
		parts = append(parts, fmt.Sprintf("%s = $%d", column, len(args)))
	}
	for _, v := range []struct {
		name string
		id   int64
	}{{"user_id", f.UserID}, {"api_key_id", f.APIKeyID}, {"account_id", f.AccountID}, {"group_id", f.GroupID}} {
		if v.id > 0 {
			add(v.name, v.id)
		}
	}
	if f.Model != "" {
		add("model", f.Model)
	}
	if f.State != "" {
		add("state", f.State)
	}
	if f.Transport != "" {
		add("transport", f.Transport)
	}
	if f.RequestID != "" {
		args = append(args, f.RequestID)
		parts = append(parts, fmt.Sprintf("(request_id=$%d OR client_request_id=$%d)", len(args), len(args)))
	}
	if !f.BeforeTime.IsZero() {
		args = append(args, f.BeforeTime, f.BeforeID)
		parts = append(parts, fmt.Sprintf("(started_at,trace_id) < ($%d,$%d::uuid)", len(args)-1, len(args)))
	}
	return strings.Join(parts, " AND "), args
}

func scanConnectionTrace(row interface{ Scan(...any) error }, detail bool) (service.ConnectionTrace, error) {
	var v service.ConnectionTrace
	var raw []byte
	args := []any{&v.StartedAt, &v.TraceID, &v.RequestID, &v.ClientRequestID, &v.UserID, &v.APIKeyID, &v.AccountID, &v.GroupID, &v.Model, &v.Path, &v.Transport, &v.State, &v.Status, &v.TotalMS, &v.FirstByteMS, &v.AuthMS, &v.RoutingMS, &v.UpstreamMS, &v.FirstTokenMS, &v.RequestBytes, &v.ResponseBytes, &v.ErrorType}
	if detail {
		args = append(args, &raw)
	} else {
		args = append(args, &v.DetailAvailable)
	}
	err := row.Scan(args...)
	if err != nil {
		return v, err
	}
	if detail && len(raw) > 0 {
		v.Detail = &requesttiming.Snapshot{}
		if err = json.Unmarshal(raw, v.Detail); err != nil {
			return v, err
		}
		v.DetailAvailable = true
	}
	return v, nil
}

// 游标分页无需 COUNT 和深 OFFSET，列表只读取轻量列，详情按需加载。
func (r *opsRepository) ListConnectionTraces(ctx context.Context, f service.ConnectionTraceFilter) ([]service.ConnectionTrace, error) {
	where, args := connectionTraceWhere(f)
	args = append(args, f.Limit+1)
	rows, err := r.db.QueryContext(ctx, "SELECT "+connectionTraceColumns+",detail IS NOT NULL FROM connection_traces WHERE "+where+fmt.Sprintf(" ORDER BY started_at DESC,trace_id DESC LIMIT $%d", len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]service.ConnectionTrace, 0, f.Limit+1)
	for rows.Next() {
		v, e := scanConnectionTrace(rows, false)
		if e != nil {
			return nil, e
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

func (r *opsRepository) GetConnectionTrace(ctx context.Context, at time.Time, id string) (*service.ConnectionTrace, error) {
	v, err := scanConnectionTrace(r.db.QueryRowContext(ctx, "SELECT "+connectionTraceColumns+",detail FROM connection_traces WHERE started_at=$1 AND trace_id=$2::uuid", at, id), true)
	if err == sql.ErrNoRows {
		return nil, infraerrors.NotFound("TRACE_NOT_FOUND", "记录已过保留期或不存在")
	}
	return &v, err
}

var connectionPartitionName = regexp.MustCompile(`^connection_traces_p[0-9]{8}$`)

// 按 UTC 日分区，过期时直接移除整张分区，避免大表逐行删除和膨胀。
func (r *opsRepository) MaintainConnectionTracePartitions(ctx context.Context, now time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, "SET LOCAL lock_timeout = '500ms'"); err != nil {
		return err
	}
	var locked bool
	if err = tx.QueryRowContext(ctx, "SELECT pg_try_advisory_xact_lock(1927438661)").Scan(&locked); err != nil || !locked {
		return err
	}
	day := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	for offset := -(service.ConnectionTraceRetentionDays - 1); offset <= 2; offset++ {
		from := day.AddDate(0, 0, offset)
		to := from.AddDate(0, 0, 1)
		name := pq.QuoteIdentifier("connection_traces_p" + from.Format("20060102"))
		q := "CREATE TABLE IF NOT EXISTS " + name + " PARTITION OF connection_traces FOR VALUES FROM ('" + from.Format(time.RFC3339) + "') TO ('" + to.Format(time.RFC3339) + "')"
		if _, err = tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT child.relname FROM pg_inherits i JOIN pg_class child ON child.oid=i.inhrelid WHERE i.inhparent='connection_traces'::regclass`)
	if err != nil {
		return err
	}
	var expired []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			_ = rows.Close()
			return err
		}
		if !connectionPartitionName.MatchString(name) {
			continue
		}
		date, e := time.Parse("20060102", strings.TrimPrefix(name, "connection_traces_p"))
		if e == nil && date.Before(day.AddDate(0, 0, -(service.ConnectionTraceRetentionDays-1))) {
			expired = append(expired, name)
		}
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	for _, name := range expired {
		if _, err = tx.ExecContext(ctx, "DROP TABLE "+pq.QuoteIdentifier(name)); err != nil {
			return err
		}
	}
	return tx.Commit()
}
