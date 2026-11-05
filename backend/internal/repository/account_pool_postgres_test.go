package repository

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPoolShareAndGroupScopePostgres(t *testing.T) {
	dsn := os.Getenv("IKUN_TOOLS_TEST_DSN")
	if dsn == "" {
		t.Skip("isolated PostgreSQL required")
	}
	base, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	schema := "pool_fixture_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = base.Exec("CREATE SCHEMA " + schema)
	require.NoError(t, err)
	db, err := sql.Open("postgres", dsn+" search_path="+schema)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close(); base.Exec("DROP SCHEMA " + schema + " CASCADE"); base.Close() })
	_, err = db.Exec(`CREATE TABLE users(id BIGINT PRIMARY KEY,status TEXT,deleted_at TIMESTAMPTZ);
	INSERT INTO users VALUES(42,'active',NULL),(43,'active',NULL);
	CREATE TABLE usage_logs(user_id BIGINT,account_id BIGINT,group_id BIGINT,created_at TIMESTAMPTZ,input_tokens INT,output_tokens INT,cache_creation_tokens INT,cache_read_tokens INT,account_stats_cost NUMERIC,total_cost NUMERIC,account_rate_multiplier NUMERIC,actual_cost NUMERIC);
	INSERT INTO usage_logs VALUES(42,9,1,NOW(),1,2,0,0,1,1,1,1),(42,9,2,NOW(),100,200,0,0,100,100,1,100),(42,10,2,NOW(),20,30,0,0,10,10,1,10),(43,9,1,NOW(),900,800,0,0,900,900,1,900);`)
	require.NoError(t, err)
	migration, err := migrations.FS.ReadFile("265_pool_share_links.sql")
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		_, err = db.Exec(string(migration))
		require.NoError(t, err)
	}
	repo := &usageLogRepository{sql: db}
	ctx := context.Background()
	start := time.Now().Add(-time.Hour)
	ids, err := repo.ListAccountIDsByUser(ctx, 42, start, []int64{1})
	require.NoError(t, err)
	require.Equal(t, []int64{9}, ids)
	stats, err := repo.GetUserAccountWindowStatsBatch(ctx, 42, ids, start, []int64{1})
	require.NoError(t, err)
	require.Equal(t, int64(1), stats[9].Requests)
	require.Equal(t, int64(3), stats[9].Tokens)
	require.Equal(t, float64(1), stats[9].UserCost)
	require.NoError(t, repo.SavePoolShare(ctx, 42, "HASH_A", time.Now().Add(time.Hour)))
	id, err := repo.ResolvePoolShare(ctx, "HASH_A", time.Now())
	require.NoError(t, err)
	require.Equal(t, int64(42), id)
	require.NoError(t, repo.SavePoolShare(ctx, 42, "HASH_B", time.Now().Add(time.Hour)))
	id, err = repo.ResolvePoolShare(ctx, "HASH_A", time.Now())
	require.NoError(t, err)
	require.Zero(t, id)
	_, err = db.Exec("UPDATE users SET status='disabled' WHERE id=42")
	require.NoError(t, err)
	id, err = repo.ResolvePoolShare(ctx, "HASH_B", time.Now())
	require.NoError(t, err)
	require.Zero(t, id)
	_, err = db.Exec("UPDATE users SET status='active' WHERE id=42")
	require.NoError(t, err)
	id, err = repo.ResolvePoolShare(ctx, "HASH_B", time.Now().Add(2*time.Hour))
	require.NoError(t, err)
	require.Zero(t, id)
	require.NoError(t, repo.RevokePoolShare(ctx, 42))
	id, err = repo.ResolvePoolShare(ctx, "HASH_B", time.Now())
	require.NoError(t, err)
	require.Zero(t, id)
}
