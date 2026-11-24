package repository

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/requesttiming"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func connectionFixtureDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("OPS_CONNECTION_TEST_DSN")
	if dsn == "" {
		t.Skip("OPS_CONNECTION_TEST_DSN requires a disposable local database")
	}
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Contains(t, []string{"127.0.0.1", "localhost"}, u.Hostname())
	require.Equal(t, "/schedulable_timer_fixture", u.Path)
	root, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	schema := fmt.Sprintf("connections_%d", time.Now().UnixNano())
	_, err = root.Exec("CREATE SCHEMA " + pq.QuoteIdentifier(schema))
	require.NoError(t, err)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = db.Close()
		_, err := root.Exec("DROP SCHEMA " + pq.QuoteIdentifier(schema) + " CASCADE")
		require.NoError(t, err)
		_ = root.Close()
	})
	raw, err := os.ReadFile("../../migrations/280_connection_diagnostics.sql")
	require.NoError(t, err)
	_, err = db.Exec(string(raw))
	require.NoError(t, err)
	return db
}

func TestConnectionDiagnosticsPostgresRoundTripCursorAndRetention(t *testing.T) {
	db := connectionFixtureDB(t)
	repo := &opsRepository{db: db}
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, repo.MaintainConnectionTracePartitions(ctx, now))
	rows := make([]service.ConnectionTrace, 0, 120)
	for i := 0; i < 120; i++ {
		rows = append(rows, service.ConnectionTrace{TraceID: uuid.NewString(), StartedAt: now, RequestID: fmt.Sprintf("request-%03d", i), UserID: 7, AccountID: 9, GroupID: 3, State: "completed", Status: 200, TotalMS: 500, Model: "fixture-model"})
	}
	rows[0].State = "failed"
	rows[0].Detail = &requesttiming.Snapshot{Version: 1, TraceID: rows[0].TraceID, Events: map[string]float64{"first_byte": 400}}
	require.NoError(t, repo.InsertConnectionTraces(ctx, rows))
	f := service.ConnectionTraceFilter{Start: now.Add(-time.Hour), End: now.Add(time.Minute), UserID: 7, Limit: 50}
	first, err := repo.ListConnectionTraces(ctx, f)
	require.NoError(t, err)
	require.Len(t, first, 51)
	seen := map[string]bool{}
	for _, r := range first[:50] {
		require.Nil(t, r.Detail)
		seen[r.TraceID] = true
	}
	f.BeforeTime = first[49].StartedAt
	f.BeforeID = first[49].TraceID
	second, err := repo.ListConnectionTraces(ctx, f)
	require.NoError(t, err)
	require.Len(t, second, 51)
	for _, r := range second[:50] {
		require.False(t, seen[r.TraceID])
	}
	detail, err := repo.GetConnectionTrace(ctx, now, rows[0].TraceID)
	require.NoError(t, err)
	require.NotNil(t, detail.Detail)
	require.Equal(t, 400.0, detail.Detail.Events["first_byte"])
	f.RequestID = rows[0].RequestID
	f.BeforeTime = time.Time{}
	f.BeforeID = ""
	exact, err := repo.ListConnectionTraces(ctx, f)
	require.NoError(t, err)
	require.Len(t, exact, 1)
	require.NoError(t, repo.MaintainConnectionTracePartitions(ctx, now.AddDate(0, 0, 4)))
	_, err = repo.GetConnectionTrace(ctx, now, rows[0].TraceID)
	require.Error(t, err)
}

func TestConnectionDiagnosticsPostgresIndexedQuery(t *testing.T) {
	db := connectionFixtureDB(t)
	repo := &opsRepository{db: db}
	ctx := context.Background()
	now := time.Now().UTC()
	require.NoError(t, repo.MaintainConnectionTracePartitions(ctx, now))
	_, err := db.Exec(`INSERT INTO connection_traces (started_at,trace_id,request_id,user_id,account_id,group_id,state,status,total_ms) SELECT NOW()-interval '1 hour'+n*interval '0.02 second',md5(n::text)::uuid,'indexed-'||n,1+n%100,1+n%30,1+n%7,'completed',200,30 FROM generate_series(1,100000) n`)
	require.NoError(t, err)
	_, err = db.Exec("ANALYZE connection_traces")
	require.NoError(t, err)
	var plan string
	require.NoError(t, db.QueryRow(`EXPLAIN (ANALYZE, FORMAT JSON) SELECT trace_id FROM connection_traces WHERE started_at >= NOW()-INTERVAL '2 hours' AND request_id='indexed-50000' ORDER BY started_at DESC,trace_id DESC LIMIT 51`).Scan(&plan))
	require.Contains(t, plan, "Index")
	if len(plan) > 0 {
		t.Log("100000 rows; request lookup uses an index")
	}
	start := time.Now()
	for i := 0; i < 20; i++ {
		rows, err := repo.ListConnectionTraces(ctx, service.ConnectionTraceFilter{Start: now.Add(-2 * time.Hour), End: now.Add(time.Minute), RequestID: "indexed-50000", Limit: 50})
		require.NoError(t, err)
		require.Len(t, rows, 1)
	}
	t.Logf("Indexed lookup average: %s", time.Since(start)/20)
	where, _ := connectionTraceWhere(service.ConnectionTraceFilter{Start: now.Add(-time.Hour), End: now, BeforeTime: now, BeforeID: uuid.NewString(), Limit: 50})
	require.NotContains(t, strings.ToUpper(where), "OFFSET")
}
