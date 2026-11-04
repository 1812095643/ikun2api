package repository

import (
	"context"
	"database/sql"
	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/MACOS-DO/sub4api/ent"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/MACOS-DO/sub4api/migrations"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"os"
	"strings"
	"sync"
	"testing"
)

type universalRedeemUserFixture struct{ service.UserRepository }

func (u universalRedeemUserFixture) GetByID(_ context.Context, id int64) (*service.User, error) {
	return &service.User{ID: id, Status: service.StatusActive}, nil
}
func TestUniversalPostgresRealRedeemRepositoryAtomicityAndConcurrentUse(t *testing.T) {
	dsn := os.Getenv("IKUN_UNIVERSAL_TEST_DSN")
	if dsn == "" {
		t.Skip("isolated PostgreSQL not configured")
	}
	base, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	schema := "ikun_universal_redeem_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = base.Exec("CREATE SCHEMA " + schema)
	require.NoError(t, err)
	db, err := sql.Open("postgres", dsn+" search_path="+schema)
	require.NoError(t, err)
	db.SetMaxOpenConns(20)
	t.Cleanup(func() { db.Close(); base.Exec("DROP SCHEMA " + schema + " CASCADE"); base.Close() })
	_, err = db.Exec(`CREATE TABLE users(id bigint PRIMARY KEY,email text,username text,status text,balance numeric(20,8) DEFAULT 100,deleted_at timestamptz);INSERT INTO users(id,email,username,status)VALUES(1,'one@example.test','One','active'),(2,'two@example.test','Two','disabled');CREATE TABLE payment_orders(id bigint PRIMARY KEY);CREATE TABLE api_keys(id bigint PRIMARY KEY,user_id bigint,deleted_at timestamptz);CREATE TABLE usage_logs(id bigint PRIMARY KEY);
 CREATE TABLE redeem_codes(id bigserial PRIMARY KEY,code varchar(32) NOT NULL UNIQUE,type varchar(20) NOT NULL,value numeric(20,8) DEFAULT 0,status varchar(20) NOT NULL,used_by bigint REFERENCES users(id),used_at timestamptz,notes text,created_at timestamptz DEFAULT now(),expires_at timestamptz,group_id bigint,validity_days integer DEFAULT 30);`)
	require.NoError(t, err)
	raw, err := migrations.FS.ReadFile("095_subscription_plans.sql")
	require.NoError(t, err)
	_, err = db.Exec(string(raw))
	require.NoError(t, err)
	_, err = db.Exec(`ALTER TABLE subscription_plans ADD COLUMN currency varchar(3) NOT NULL DEFAULT ''`)
	require.NoError(t, err)
	raw, err = migrations.FS.ReadFile("264_universal_subscriptions.sql")
	require.NoError(t, err)
	_, err = db.Exec(string(raw))
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	subs := service.NewSubscriptionService(nil, nil, nil, client, nil)
	defer subs.Stop()
	config := service.NewPaymentConfigService(client, nil, nil)
	credit, daily := 100.0, 10.0
	plan, err := config.CreatePlan(context.Background(), service.CreatePlanRequest{Name: "Coupon plan", Price: 29, ValidityDays: 30, TotalCreditUSD: &credit, DailyLimitUSD: &daily})
	require.NoError(t, err)
	codesRepo := NewRedeemCodeRepository(client)
	redeem := service.NewRedeemService(codesRepo, universalRedeemUserFixture{}, subs, nil, nil, client, nil, nil)
	codes, err := redeem.GenerateUniversalCodes(context.Background(), plan.ID, 2, nil)
	require.NoError(t, err)
	require.Len(t, codes, 2)
	require.NotZero(t, codes[0].ID)
	require.NotEqual(t, codes[0].ID, codes[1].ID)
	require.Len(t, codes[0].Code, 32)
	increased := 500.0
	_, err = config.UpdatePlan(context.Background(), plan.ID, service.UpdatePlanRequest{TotalCreditUSD: &increased})
	require.NoError(t, err)
	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := redeem.Redeem(context.Background(), 1, codes[0].Code)
			if e == nil {
				mu.Lock()
				success++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	require.Equal(t, 1, success)
	var count int
	var balance, total float64
	require.NoError(t, db.QueryRow(`SELECT count(*),max(total_credit_usd) FROM universal_subscriptions WHERE user_id=1`).Scan(&count, &total))
	require.Equal(t, 1, count)
	require.Equal(t, 100.0, total)
	require.NoError(t, db.QueryRow(`SELECT balance FROM users WHERE id=1`).Scan(&balance))
	require.Equal(t, 100.0, balance)
	_, err = redeem.Redeem(context.Background(), 2, codes[1].Code)
	require.Error(t, err)
	var state string
	require.NoError(t, db.QueryRow(`SELECT status FROM redeem_codes WHERE id=$1`, codes[1].ID).Scan(&state))
	require.Equal(t, service.StatusUnused, state, "failed grant must roll back code use")
	_, err = db.Exec(`UPDATE users SET status='active' WHERE id=2`)
	require.NoError(t, err)
	_, err = redeem.Redeem(context.Background(), 2, codes[1].Code)
	require.NoError(t, err)
}
