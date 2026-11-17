package repository

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/MACOS-DO/sub4api/ent"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/MACOS-DO/sub4api/migrations"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func universalBatchPostgresFixture(t *testing.T) (*sql.DB, *service.UniversalSubscriptionService, int64) {
	t.Helper()
	dsn := os.Getenv("IKUN_UNIVERSAL_TEST_DSN")
	if dsn == "" {
		t.Skip("isolated PostgreSQL not configured")
	}
	base, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	schema := "ikun_batch_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = base.Exec("CREATE SCHEMA " + schema)
	require.NoError(t, err)
	db, err := sql.Open("postgres", dsn+" search_path="+schema)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = db.Close()
		_, _ = base.Exec("DROP SCHEMA " + schema + " CASCADE")
		_ = base.Close()
	})
	_, err = db.Exec(`
		CREATE TABLE users(id bigint PRIMARY KEY,email text,username text,status text,balance numeric(20,8) DEFAULT 100,deleted_at timestamptz,updated_at timestamptz);
		INSERT INTO users(id,email,username,status) VALUES(1,'batch@example.test','Batch','active');
		CREATE TABLE payment_orders(id bigint PRIMARY KEY);
		CREATE TABLE groups(id bigint PRIMARY KEY,subscription_type text);
		CREATE TABLE api_keys(id bigint PRIMARY KEY,user_id bigint,group_id bigint,deleted_at timestamptz,status text DEFAULT 'active',
			quota numeric(20,8) DEFAULT 4,quota_used numeric(20,8) DEFAULT 0,
			usage_5h numeric(20,8) DEFAULT 0,usage_1d numeric(20,8) DEFAULT 0,usage_7d numeric(20,8) DEFAULT 0,
			window_5h_start timestamptz,window_1d_start timestamptz,window_7d_start timestamptz,updated_at timestamptz);
		INSERT INTO api_keys(id,user_id) VALUES(11,1);
		CREATE TABLE usage_logs(id bigint PRIMARY KEY);
		CREATE TABLE redeem_codes(id bigint PRIMARY KEY);
		CREATE TABLE usage_billing_dedup(id bigserial PRIMARY KEY,request_id text,api_key_id bigint,request_fingerprint text,UNIQUE(request_id,api_key_id));
		CREATE TABLE usage_billing_dedup_archive(request_id text,api_key_id bigint,request_fingerprint text,UNIQUE(request_id,api_key_id));
	`)
	require.NoError(t, err)
	for _, name := range []string{"095_subscription_plans.sql", "264_universal_subscriptions.sql"} {
		raw, readErr := migrations.FS.ReadFile(name)
		require.NoError(t, readErr)
		_, err = db.Exec(string(raw))
		require.NoError(t, err)
	}
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	svc := service.NewUniversalSubscriptionService(client)
	credit, daily := 100.0, 10.0
	sub, err := svc.Grant(context.Background(), 1, service.UniversalPlanSnapshot{
		Name: "Batch", ValidityDays: 30, TotalCreditUSD: &credit, DailyLimitUSD: &daily,
	}, nil, nil, "batch-grant", "test")
	require.NoError(t, err)
	return db, svc, sub.ID
}

func universalBatchCommand(t *testing.T, db *sql.DB, svc *service.UniversalSubscriptionService, requestID string, cost float64) *service.UsageBillingCommand {
	t.Helper()
	hold, err := svc.Reserve(context.Background(), 1, 11, cost)
	require.NoError(t, err)
	t.Cleanup(func() { hold.MarkDefinitiveNoCharge(); hold.HandlerDone() })
	var holdID string
	require.NoError(t, db.QueryRow(`SELECT id FROM universal_subscription_holds WHERE user_id=1 AND api_key_id=11 AND state='reserved' ORDER BY created_at DESC LIMIT 1`).Scan(&holdID))
	return &service.UsageBillingCommand{
		RequestID: requestID, UserID: 1, APIKeyID: 11, UniversalHoldID: holdID,
		UniversalCost: cost, APIKeyQuotaCost: cost, APIKeyRateLimitCost: cost,
	}
}

func TestUsageBillingBatchPostgresUniversalAndWalletSettleOnce(t *testing.T) {
	db, svc, subID := universalBatchPostgresFixture(t)
	ctx := context.Background()
	repo := NewUsageBillingRepository(nil, db)
	cmds := []*service.UsageBillingCommand{
		universalBatchCommand(t, db, svc, "universal-one", 1.25),
		universalBatchCommand(t, db, svc, "universal-two", 2.5),
		{RequestID: "wallet", UserID: 1, APIKeyID: 11, BalanceCost: 1, APIKeyQuotaCost: 1, APIKeyRateLimitCost: 1},
	}
	outcomes, err := repo.ApplyBatch(ctx, cmds)
	require.NoError(t, err)
	require.Len(t, outcomes, 3)
	for _, out := range outcomes {
		require.NoError(t, out.Err)
		require.True(t, out.Result.Applied)
	}
	require.False(t, outcomes[0].Result.APIKeyQuotaExhausted)
	require.False(t, outcomes[1].Result.APIKeyQuotaExhausted)
	require.True(t, outcomes[2].Result.APIKeyQuotaExhausted)
	require.Equal(t, 99.0, *outcomes[2].Result.NewBalance)

	check := func() {
		var used, balance, quotaUsed, windowUsed float64
		var charges, dedup int
		require.NoError(t, db.QueryRow(`SELECT total_used_usd FROM universal_subscriptions WHERE id=$1`, subID).Scan(&used))
		require.NoError(t, db.QueryRow(`SELECT balance FROM users WHERE id=1`).Scan(&balance))
		require.NoError(t, db.QueryRow(`SELECT quota_used,usage_5h FROM api_keys WHERE id=11`).Scan(&quotaUsed, &windowUsed))
		require.NoError(t, db.QueryRow(`SELECT count(*) FROM universal_subscription_charges`).Scan(&charges))
		require.NoError(t, db.QueryRow(`SELECT count(*) FROM usage_billing_dedup`).Scan(&dedup))
		require.Equal(t, 3.75, used)
		require.Equal(t, 99.0, balance)
		require.Equal(t, 4.75, quotaUsed)
		require.Equal(t, 4.75, windowUsed)
		require.Equal(t, 2, charges)
		require.Equal(t, 3, dedup)
	}
	check()
	outcomes, err = repo.ApplyBatch(ctx, cmds)
	require.NoError(t, err)
	for _, out := range outcomes {
		require.NoError(t, out.Err)
		require.False(t, out.Result.Applied)
	}
	check()
}

func TestUsageBillingBatchPostgresUniversalFailureRollsBackEveryEffect(t *testing.T) {
	db, svc, subID := universalBatchPostgresFixture(t)
	first := universalBatchCommand(t, db, svc, "valid-universal", 1.25)
	invalid := &service.UsageBillingCommand{
		RequestID: "missing-universal", UserID: 1, APIKeyID: 11, UniversalHoldID: uuid.NewString(), UniversalCost: 1,
	}
	outcomes, err := NewUsageBillingRepository(nil, db).ApplyBatch(context.Background(), []*service.UsageBillingCommand{first, invalid})
	require.Error(t, err)
	require.Nil(t, outcomes)
	var used, balance, quota float64
	var count int
	require.NoError(t, db.QueryRow(`SELECT total_used_usd FROM universal_subscriptions WHERE id=$1`, subID).Scan(&used))
	require.NoError(t, db.QueryRow(`SELECT balance FROM users WHERE id=1`).Scan(&balance))
	require.NoError(t, db.QueryRow(`SELECT quota_used FROM api_keys WHERE id=11`).Scan(&quota))
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM usage_billing_dedup`).Scan(&count))
	require.Zero(t, used)
	require.Equal(t, 100.0, balance)
	require.Zero(t, quota)
	require.Zero(t, count)
}

func TestUsageBillingBatchPostgresDeletedKeyKeepsAcceptedCharges(t *testing.T) {
	db, _, _ := universalBatchPostgresFixture(t)
	_, err := db.Exec(`UPDATE api_keys SET deleted_at=now(),status='disabled' WHERE id=11`)
	require.NoError(t, err)
	cmds := []*service.UsageBillingCommand{
		{RequestID: "deleted-one", UserID: 1, APIKeyID: 11, BalanceCost: 2, APIKeyQuotaCost: 2, APIKeyRateLimitCost: 2},
		{RequestID: "deleted-two", UserID: 1, APIKeyID: 11, BalanceCost: 3, APIKeyQuotaCost: 3, APIKeyRateLimitCost: 3},
	}
	outcomes, err := NewUsageBillingRepository(nil, db).ApplyBatch(context.Background(), cmds)
	require.NoError(t, err)
	for _, out := range outcomes {
		require.True(t, out.Result.Applied)
		require.False(t, out.Result.APIKeyQuotaExhausted)
	}
	var balance, quotaUsed, windowUsed float64
	var status string
	require.NoError(t, db.QueryRow(`SELECT balance FROM users WHERE id=1`).Scan(&balance))
	require.NoError(t, db.QueryRow(`SELECT quota_used,usage_5h,status FROM api_keys WHERE id=11`).Scan(&quotaUsed, &windowUsed, &status))
	require.Equal(t, 95.0, balance)
	require.Equal(t, 5.0, quotaUsed)
	require.Equal(t, 5.0, windowUsed)
	require.Equal(t, "disabled", status)
}
