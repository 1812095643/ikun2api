package repository

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestOpenAIServiceTierPostgresPersistence(t *testing.T) {
	dsn := os.Getenv("IKUN_BUSINESS_TEST_DSN")
	if dsn == "" {
		t.Skip("IKUN_BUSINESS_TEST_DSN is required for an isolated PostgreSQL fixture")
	}
	ctx := context.Background()
	base, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer base.Close()
	schema := fmt.Sprintf("tier_fixture_%d", time.Now().UnixNano())
	_, err = base.ExecContext(ctx, "CREATE SCHEMA "+pq.QuoteIdentifier(schema))
	require.NoError(t, err)
	defer func() { _, _ = base.ExecContext(ctx, "DROP SCHEMA "+pq.QuoteIdentifier(schema)+" CASCADE") }()
	db, err := sql.Open("postgres", dsn+" search_path="+schema)
	require.NoError(t, err)
	defer db.Close()
	_, err = db.ExecContext(ctx, `CREATE TABLE groups(id BIGINT PRIMARY KEY, force_openai_fast BOOLEAN NOT NULL DEFAULT FALSE);
        CREATE TABLE api_keys(id BIGINT PRIMARY KEY);
        INSERT INTO groups(id, force_openai_fast) VALUES(1, TRUE);
        INSERT INTO api_keys(id) VALUES(2);`)
	require.NoError(t, err)
	columns := strings.Split(usageLogSelectColumns, ", ")
	require.Equal(t, len(usageLogInsertArgTypes)+1, len(columns))
	definitions := []string{"id BIGSERIAL PRIMARY KEY"}
	for i, typ := range usageLogInsertArgTypes {
		column := columns[i+1]
		if column == "service_tier_multiplier" {
			continue
		} // created by the forward migration
		definition := pq.QuoteIdentifier(column) + " " + typ
		switch typ {
		case "integer", "bigint", "smallint", "numeric":
			definition += " DEFAULT 0"
		case "boolean":
			definition += " DEFAULT FALSE"
		case "timestamptz":
			definition += " DEFAULT NOW()"
		}
		definitions = append(definitions, definition)
	}
	_, err = db.ExecContext(ctx, "CREATE TABLE usage_logs ("+strings.Join(definitions, ",")+", UNIQUE(request_id,api_key_id))")
	require.NoError(t, err)
	migration, err := os.ReadFile("../../migrations/273_openai_account_service_tiers.sql")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(migration))
	require.NoError(t, err, "migration must be repeatable")
	var fast, ultra bool
	var forced string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT force_openai_fast,force_openai_ultrafast FROM groups WHERE id=1").Scan(&fast, &ultra))
	require.True(t, fast)
	require.False(t, ultra, "existing Fast groups must remain unchanged")
	require.NoError(t, db.QueryRowContext(ctx, "SELECT forced_service_tier FROM api_keys WHERE id=2").Scan(&forced))
	require.Empty(t, forced)
	_, err = db.ExecContext(ctx, "UPDATE groups SET force_openai_ultrafast=TRUE WHERE id=1")
	require.Error(t, err, "mutually exclusive modes enforced in storage")
	_, err = db.ExecContext(ctx, "UPDATE groups SET force_openai_fast=FALSE,force_openai_ultrafast=TRUE WHERE id=1")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "UPDATE api_keys SET forced_service_tier='ultrafast' WHERE id=2")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "UPDATE api_keys SET forced_service_tier='turbo' WHERE id=2")
	require.Error(t, err)

	tier := "ultrafast"
	ratio := 8.0
	makeLog := func(request string) *service.UsageLog {
		return &service.UsageLog{UserID: 1, APIKeyID: 2, AccountID: 7, RequestID: request, Model: "gpt-6.1-sol", InputTokens: 1200, OutputTokens: 300, ServiceTier: &tier, ServiceTierMultiplier: &ratio, RateMultiplier: .7, TotalCost: .08, ActualCost: .056}
	}
	repo := newUsageLogRepositoryWithSQL(nil, db)
	syncLog := makeLog("fixture-sync")
	inserted, err := repo.Create(ctx, syncLog)
	require.NoError(t, err)
	require.True(t, inserted)
	read := func(request string) *service.UsageLog {
		log, err := scanUsageLog(db.QueryRowContext(ctx, "SELECT "+usageLogSelectColumns+" FROM usage_logs WHERE request_id=$1", request))
		require.NoError(t, err)
		require.Equal(t, "ultrafast", *log.ServiceTier)
		require.Equal(t, 8.0, *log.ServiceTierMultiplier)
		require.InDelta(t, .056, log.ActualCost, 1e-12)
		return log
	}
	read("fixture-sync")
	// Exercise the actual batch CTE, not just a hand-written INSERT.
	batchLog := makeLog("fixture-batch")
	prepared := prepareUsageLogInsert(batchLog)
	key := batchLog.RequestID
	query, args := buildUsageLogBatchInsertQuery([]string{key}, map[string]usageLogInsertPrepared{key: prepared})
	var result []byte
	require.NoError(t, db.QueryRowContext(ctx, query, args...).Scan(&result))
	read("fixture-batch")
	bestEffort := makeLog("fixture-best-effort")
	query, args = buildUsageLogBestEffortInsertQuery([]usageLogInsertPrepared{prepareUsageLogInsert(bestEffort)})
	_, err = db.ExecContext(ctx, query, args...)
	require.NoError(t, err)
	read("fixture-best-effort")
	// Exact-once dedup must retain the originally applied multiplier.
	ratio = 11
	_, err = db.ExecContext(ctx, query, args...)
	require.NoError(t, err)
	read("fixture-best-effort")
}
