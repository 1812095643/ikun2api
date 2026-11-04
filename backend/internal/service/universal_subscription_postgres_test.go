package service

import (
	"context"
	"database/sql"
	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/MACOS-DO/sub4api/ent"
	"github.com/MACOS-DO/sub4api/migrations"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func universalPostgresFixture(t *testing.T) (*UniversalSubscriptionService, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("IKUN_UNIVERSAL_TEST_DSN")
	if dsn == "" {
		t.Skip("isolated PostgreSQL not configured")
	}
	base, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	schema := "ikun_universal_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = base.Exec("CREATE SCHEMA " + schema)
	require.NoError(t, err)
	db, err := sql.Open("postgres", dsn+" search_path="+schema)
	require.NoError(t, err)
	db.SetMaxOpenConns(20)
	t.Cleanup(func() { db.Close(); base.Exec("DROP SCHEMA " + schema + " CASCADE"); base.Close() })
	_, err = db.Exec(`CREATE TABLE users(id bigint PRIMARY KEY,email text,username text,status text,balance numeric(20,8) DEFAULT 100,deleted_at timestamptz);INSERT INTO users(id,email,username,status)VALUES(1,'one@example.test','One','active'),(2,'two@example.test','Two','active');
 CREATE TABLE payment_orders(id bigint PRIMARY KEY);INSERT INTO payment_orders(id)VALUES(101),(102);
 CREATE TABLE groups(id bigint PRIMARY KEY,subscription_type text);INSERT INTO groups VALUES(9,'subscription');
 CREATE TABLE api_keys(id bigint PRIMARY KEY,user_id bigint,group_id bigint,deleted_at timestamptz);INSERT INTO api_keys(id,user_id,group_id)VALUES(11,1,NULL),(12,1,9),(21,2,NULL);
 CREATE TABLE usage_logs(id bigint PRIMARY KEY);CREATE TABLE redeem_codes(id bigint PRIMARY KEY);`)
	require.NoError(t, err)
	raw, err := migrations.FS.ReadFile("095_subscription_plans.sql")
	require.NoError(t, err)
	_, err = db.Exec(string(raw))
	require.NoError(t, err)
	_, err = db.Exec(`ALTER TABLE subscription_plans ADD COLUMN currency varchar(3) NOT NULL DEFAULT '';INSERT INTO subscription_plans(group_id,name,price)VALUES(9,'Legacy',1)`)
	require.NoError(t, err)
	raw, err = migrations.FS.ReadFile("264_universal_subscriptions.sql")
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		_, err = db.Exec(string(raw))
		require.NoError(t, err)
	}
	var sale bool
	err = db.QueryRow(`SELECT for_sale FROM subscription_plans WHERE name='Legacy'`).Scan(&sale)
	require.NoError(t, err)
	require.False(t, sale)
	var funding string
	require.NoError(t, db.QueryRow(`SELECT funding_source FROM api_keys WHERE id=12`).Scan(&funding))
	require.Equal(t, FundingSourceUniversal, funding, "old subscription keys must never silently start charging wallets")
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	svc := NewUniversalSubscriptionService(client)
	return svc, db
}
func universalTestPlan() UniversalPlanSnapshot {
	return UniversalPlanSnapshot{Name: "Universal", ValidityDays: 30, Price: 10, TotalCreditUSD: universalFloat(100), DailyLimitUSD: universalFloat(10), WeeklyLimitUSD: universalFloat(50), MonthlyLimitUSD: universalFloat(100)}
}
func TestUniversalPostgresGrantQueueIdempotencyAndSnapshot(t *testing.T) {
	s, db := universalPostgresFixture(t)
	ctx := context.Background()
	p := universalTestPlan()
	order := int64(101)
	first, err := s.Grant(ctx, 1, p, &order, nil, "payment:101", "purchase")
	require.NoError(t, err)
	again, err := s.Grant(ctx, 1, p, &order, nil, "payment:101", "purchase")
	require.NoError(t, err)
	require.Equal(t, first.ID, again.ID)
	changed := p
	changed.TotalCreditUSD = universalFloat(200)
	_, err = s.Grant(ctx, 1, changed, &order, nil, "payment:101", "purchase")
	require.Error(t, err)
	second, err := s.Grant(ctx, 1, p, nil, nil, "admin:queue-two", "renew")
	require.NoError(t, err)
	require.Equal(t, first.ExpiresAt, second.StartsAt)
	list, total, err := s.List(ctx, 1, 0, 20)
	require.NoError(t, err)
	require.Equal(t, 2, total)
	require.Equal(t, "scheduled", list[0].Status)
	other, _, err := s.List(ctx, 2, 0, 20)
	require.NoError(t, err)
	require.Empty(t, other)
	var balance float64
	require.NoError(t, db.QueryRow(`SELECT balance FROM users WHERE id=1`).Scan(&balance))
	require.Equal(t, float64(100), balance)
}
func TestUniversalPostgresConcurrentKeysShareQuotaAndSettleOnce(t *testing.T) {
	s, db := universalPostgresFixture(t)
	ctx := context.Background()
	p := universalTestPlan()
	sub, err := s.Grant(ctx, 1, p, nil, nil, "admin:concurrency", "test")
	require.NoError(t, err)
	var wg sync.WaitGroup
	var mu sync.Mutex
	holds := []*InflightReservation{}
	failures := 0
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			h, e := s.Reserve(ctx, 1, int64(11+i%2), 2)
			mu.Lock()
			defer mu.Unlock()
			if e != nil {
				failures++
			} else {
				holds = append(holds, h)
			}
		}(i)
	}
	wg.Wait()
	require.Len(t, holds, 5)
	require.Equal(t, 7, failures)
	defer func() {
		for _, h := range holds {
			h.MarkDefinitiveNoCharge()
			h.HandlerDone()
		}
	}()
	h := holds[0]
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	cmd := &UsageBillingCommand{UserID: 1, APIKeyID: h.userID + 10, RequestID: "test-charge", UniversalHoldID: h.universalHoldID, UniversalCost: 1.5}
	// Account-wide holds retain the actual initiating Key identity.
	require.NoError(t, db.QueryRow(`SELECT api_key_id FROM universal_subscription_holds WHERE id=$1`, h.universalHoldID).Scan(&cmd.APIKeyID))
	require.NoError(t, ApplyUniversalCharge(ctx, tx, cmd))
	require.NoError(t, tx.Commit())
	tx, err = db.BeginTx(ctx, nil)
	require.NoError(t, err)
	require.NoError(t, ApplyUniversalCharge(ctx, tx, cmd))
	require.NoError(t, tx.Commit())
	var used, balance float64
	require.NoError(t, db.QueryRow(`SELECT total_used_usd FROM universal_subscriptions WHERE id=$1`, sub.ID).Scan(&used))
	require.Equal(t, 1.5, used)
	require.NoError(t, db.QueryRow(`SELECT balance FROM users WHERE id=1`).Scan(&balance))
	require.Equal(t, float64(100), balance)
	_, err = s.Reserve(ctx, 1, 11, 1)
	require.Error(t, err, "remaining 0.5 daily quota must include the other Key's holds")
	_, err = s.Check(ctx, 2)
	require.Error(t, err)
}
func TestUniversalPostgresRevocationExpiryAndUnknownHold(t *testing.T) {
	s, db := universalPostgresFixture(t)
	ctx := context.Background()
	p := universalTestPlan()
	sub, err := s.Grant(ctx, 1, p, nil, nil, "admin:revocation", "test")
	require.NoError(t, err)
	h, err := s.Reserve(ctx, 1, 11, 2)
	require.NoError(t, err)
	h.HandlerDone()
	var state string
	require.NoError(t, db.QueryRow(`SELECT state FROM universal_subscription_holds WHERE id=$1`, h.universalHoldID).Scan(&state))
	require.Equal(t, "unknown", state)
	records, err := s.Records(ctx, sub.ID)
	require.NoError(t, err)
	require.Len(t, records["holds"], 1)
	require.NoError(t, s.ResolveHold(ctx, h.universalHoldID, 2, "upstream confirmed no usage"))
	require.NoError(t, s.Manage(ctx, sub.ID, sub.Version, 2, "suspended", "stop"))
	_, err = s.Reserve(ctx, 1, 11, 1)
	require.Error(t, err)
	require.Error(t, s.Manage(ctx, sub.ID, sub.Version, 2, "active", "stale"))
	require.NoError(t, s.Manage(ctx, sub.ID, sub.Version+1, 2, "active", "resume"))
	s.now = func() time.Time { return sub.ExpiresAt }
	_, err = s.Check(ctx, 1)
	require.ErrorIs(t, err, ErrUniversalNotActive)
}
func TestUniversalPostgresBatchFundingSurvivesKeyChanges(t *testing.T) {
	s, db := universalPostgresFixture(t)
	ctx := context.Background()
	sub, err := s.Grant(ctx, 1, universalTestPlan(), nil, nil, "admin:batch-source", "test")
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE api_keys SET funding_source='universal_subscription' WHERE id=11`)
	require.NoError(t, err)
	cmd := &BatchImageBalanceHoldCommand{UserID: 1, APIKeyID: 11, BatchID: "batch-universal", RequestID: BatchImageHoldRequestID("batch-universal"), HoldAmount: 4}
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	handled, _, err := ApplyUniversalBatchHold(ctx, tx, cmd)
	require.NoError(t, err)
	require.True(t, handled)
	require.NoError(t, tx.Commit())
	_, err = db.Exec(`UPDATE api_keys SET funding_source='balance',deleted_at=now() WHERE id=11`)
	require.NoError(t, err)
	cmd.RequestID = BatchImageCaptureRequestID(cmd.BatchID)
	cmd.ActualAmount = 2.5
	tx, err = db.BeginTx(ctx, nil)
	require.NoError(t, err)
	handled, _, err = ApplyUniversalBatchHold(ctx, tx, cmd)
	require.NoError(t, err)
	require.True(t, handled)
	require.NoError(t, tx.Commit())
	var used, balance float64
	require.NoError(t, db.QueryRow(`SELECT total_used_usd FROM universal_subscriptions WHERE id=$1`, sub.ID).Scan(&used))
	require.Equal(t, 2.5, used)
	require.NoError(t, db.QueryRow(`SELECT balance FROM users WHERE id=1`).Scan(&balance))
	require.Equal(t, float64(100), balance)
}
func TestUniversalPostgresRefundHoldsOnlyOriginalOrder(t *testing.T) {
	s, db := universalPostgresFixture(t)
	ctx := context.Background()
	p := universalTestPlan()
	one, two := int64(101), int64(102)
	first, err := s.Grant(ctx, 1, p, &one, nil, "payment:101", "purchase")
	require.NoError(t, err)
	second, err := s.Grant(ctx, 1, p, &two, nil, "payment:102", "renew")
	require.NoError(t, err)
	require.NoError(t, s.HoldRefund(ctx, one))
	_, err = s.Check(ctx, 1)
	require.Error(t, err)
	require.NoError(t, s.FinishRefund(ctx, one, true))
	_, err = s.Check(ctx, 1)
	require.NoError(t, err)
	require.NoError(t, s.HoldRefund(ctx, one))
	require.NoError(t, s.FinishRefund(ctx, one, false))
	var status string
	require.NoError(t, db.QueryRow(`SELECT status FROM universal_subscriptions WHERE id=$1`, first.ID).Scan(&status))
	require.Equal(t, "revoked", status)
	require.NoError(t, db.QueryRow(`SELECT status FROM universal_subscriptions WHERE id=$1`, second.ID).Scan(&status))
	require.Equal(t, "active", status)
}

func TestUniversalPostgresPlanSnapshotSurvivesEditsAndTransactionalRollback(t *testing.T) {
	s, db := universalPostgresFixture(t)
	ctx := context.Background()
	config := NewPaymentConfigService(s.client, nil, nil)
	plan, err := config.CreatePlan(ctx, CreatePlanRequest{Name: "First plan", Price: 29, ValidityDays: 30, ValidityUnit: "day", TotalCreditUSD: universalFloat(100), DailyLimitUSD: universalFloat(10), ForSale: true})
	require.NoError(t, err)
	require.Equal(t, UniversalPlanType, plan.PlanType)
	require.Zero(t, plan.GroupID)
	snapshot := UniversalSnapshot(plan)
	sub, err := s.Grant(ctx, 1, snapshot, nil, nil, "admin:immutable-plan", "test")
	require.NoError(t, err)
	changed, err := config.UpdatePlan(ctx, plan.ID, UpdatePlanRequest{TotalCreditUSD: universalFloat(500), DailyLimitUSD: universalFloat(0)})
	require.NoError(t, err)
	require.Equal(t, 500.0, *changed.TotalCreditUsd)
	require.Nil(t, changed.DailyLimitUsd)
	var total float64
	require.NoError(t, db.QueryRow(`SELECT total_credit_usd FROM universal_subscriptions WHERE id=$1`, sub.ID).Scan(&total))
	require.Equal(t, 100.0, total)
	tx, err := s.client.Tx(ctx)
	require.NoError(t, err)
	txCtx := dbent.NewTxContext(ctx, tx)
	_, err = s.Grant(txCtx, 2, snapshot, nil, nil, "redeem:rollback-test", "rollback")
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())
	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM universal_subscriptions WHERE user_id=2`).Scan(&count))
	require.Zero(t, count)
}
func TestUniversalPostgresMediaFundingUsesOriginalUserAndKey(t *testing.T) {
	s, _ := universalPostgresFixture(t)
	ctx := context.Background()
	_, err := s.Grant(ctx, 1, universalTestPlan(), nil, nil, "admin:media-owner", "test")
	require.NoError(t, err)
	billing := &BillingCacheService{universal: s}
	key := &APIKey{ID: 11, UserID: 1, FundingSource: FundingSourceUniversal}
	hold, err := s.Reserve(ctx, 1, 11, 2)
	require.NoError(t, err)
	ctx = WithInflightReservation(ctx, hold)
	require.NoError(t, billing.StoreMediaFunding(ctx, key, "media-test-task"))
	hold.HandlerDone()
	switched := *key
	switched.FundingSource = FundingSourceBalance
	resumed, pinned, done, matched, err := billing.ResumeMediaFunding(context.Background(), &switched, "media-test-task")
	require.NoError(t, err)
	require.True(t, matched)
	require.Equal(t, FundingSourceUniversal, pinned.FundingSource)
	require.NotNil(t, InflightReservationFromContext(resumed))
	done()
	_, _, _, matched, err = billing.ResumeMediaFunding(context.Background(), &APIKey{ID: 21, UserID: 2, FundingSource: FundingSourceUniversal}, "media-test-task")
	require.NoError(t, err)
	require.False(t, matched)
}
func TestUniversalPostgresAdjustmentCannotResetUsageOrOverlap(t *testing.T) {
	s, db := universalPostgresFixture(t)
	ctx := context.Background()
	sub, err := s.Grant(ctx, 1, universalTestPlan(), nil, nil, "admin:adjust-test", "test")
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE universal_subscriptions SET total_used_usd=15 WHERE id=$1`, sub.ID)
	require.NoError(t, err)
	require.Error(t, s.Adjust(ctx, sub.ID, 2, UniversalSubscriptionAdjustment{Version: sub.Version, TotalCreditUSD: universalFloat(10), Reason: "cannot erase spent credit"}))
	require.NoError(t, s.Adjust(ctx, sub.ID, 2, UniversalSubscriptionAdjustment{Version: sub.Version, TotalCreditUSD: universalFloat(150), Reason: "extra credit"}))
	var used float64
	require.NoError(t, db.QueryRow(`SELECT total_used_usd FROM universal_subscriptions WHERE id=$1`, sub.ID).Scan(&used))
	require.Equal(t, 15.0, used)
	_, err = s.Grant(ctx, 1, universalTestPlan(), nil, nil, "admin:adjust-next", "renew")
	require.NoError(t, err)
	end := sub.ExpiresAt.Add(24 * time.Hour)
	require.Error(t, s.Adjust(ctx, sub.ID, 2, UniversalSubscriptionAdjustment{Version: sub.Version + 1, ExpiresAt: &end, Reason: "overlap blocked"}))
}

func TestUniversalPostgresRefundRechecksUsageAfterReview(t *testing.T) {
	s, db := universalPostgresFixture(t)
	ctx := context.Background()
	order := int64(101)
	sub, err := s.Grant(ctx, 1, universalTestPlan(), &order, nil, "payment:101", "test")
	require.NoError(t, err)
	used, err := s.RefundState(ctx, order)
	require.NoError(t, err)
	require.Zero(t, used)
	_, err = db.Exec(`UPDATE universal_subscriptions SET total_used_usd=1 WHERE id=$1`, sub.ID)
	require.NoError(t, err)
	require.Error(t, s.HoldRefund(ctx, order), "new usage after administrator preview must require renewed confirmation")
	require.NoError(t, s.HoldRefund(ctx, order, true))
	require.NoError(t, s.FinishRefund(ctx, order, false))
}

func TestUniversalPostgresUsageEndpointDoesNotReturnWalletAsQuota(t *testing.T) {
	s, db := universalPostgresFixture(t)
	ctx := context.Background()
	_, err := s.Grant(ctx, 1, universalTestPlan(), nil, nil, "admin:usage-endpoint", "test")
	require.NoError(t, err)
	billing := &BillingCacheService{universal: s}
	key := &APIKey{ID: 11, UserID: 1, FundingSource: FundingSourceUniversal}
	data, err := billing.DescribeUniversalFunding(ctx, key)
	require.NoError(t, err)
	require.Equal(t, FundingSourceUniversal, data["funding_source"])
	require.Equal(t, 10.0, data["remaining"])
	require.NotContains(t, data, "balance")
	_, err = db.Exec(`UPDATE universal_subscriptions SET expires_at=starts_at-interval '1 day',starts_at=starts_at-interval '2 days' WHERE user_id=1`)
	require.NoError(t, err)
	data, err = billing.DescribeUniversalFunding(ctx, key)
	require.NoError(t, err)
	require.Equal(t, 0.0, data["remaining"])
	require.Equal(t, false, data["isValid"])
	require.Equal(t, false, data["balance_fallback"])
}
