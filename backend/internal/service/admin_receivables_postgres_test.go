package service

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/MACOS-DO/sub4api/ent"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

const businessFixtureSchema = `CREATE TABLE users(id bigint PRIMARY KEY,email text DEFAULT '',username text DEFAULT '',status text DEFAULT 'active',balance numeric(20,8) DEFAULT 0,updated_at timestamptz DEFAULT now(),deleted_at timestamptz);
 CREATE TABLE redeem_codes(id bigserial PRIMARY KEY,code text UNIQUE,type text,value numeric(20,8),status text,used_by bigint,used_at timestamptz,notes text,credit_source text DEFAULT 'legacy' CHECK(credit_source IN('legacy','manual','redeem','gift','lottery')));
 CREATE TABLE groups(id bigint PRIMARY KEY,deleted_at timestamptz);
 CREATE TABLE channel_model_pricing(id bigserial PRIMARY KEY);
 INSERT INTO users(id,email) VALUES(1,'admin@example.invalid'),(8,'reseller@example.invalid'),(9,'other@example.invalid');`

func adminLedgerFixture(t *testing.T) (*sql.DB, *dbent.Client) {
	t.Helper()
	dsn := os.Getenv("IKUN_BUSINESS_TEST_DSN")
	if dsn == "" {
		t.Skip("dedicated ikun_business_fixture PostgreSQL required")
	}
	base, e := sql.Open("postgres", dsn)
	require.NoError(t, e)
	var dbname string
	require.NoError(t, base.QueryRow(`SELECT current_database()`).Scan(&dbname))
	require.Equal(t, "ikun_business_fixture", dbname)
	name := "ledger_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, e = base.Exec(`CREATE SCHEMA ` + name)
	require.NoError(t, e)
	db, e := sql.Open("postgres", dsn+" search_path="+name)
	require.NoError(t, e)
	t.Cleanup(func() { db.Close(); base.Exec(`DROP SCHEMA ` + name + ` CASCADE`); base.Close() })
	_, e = db.Exec(businessFixtureSchema)
	require.NoError(t, e)
	migration, e := os.ReadFile("../../migrations/272_admin_receivables_downstreams.sql")
	require.NoError(t, e)
	_, e = db.Exec(string(migration))
	require.NoError(t, e)
	_, e = db.Exec(string(migration))
	require.NoError(t, e)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	return db, client
}

type ledgerUserRepo struct {
	UserRepository
	db *sql.DB
}

func (r *ledgerUserRepo) GetByID(ctx context.Context, id int64) (*User, error) {
	u := &User{ID: id}
	e := r.db.QueryRowContext(ctx, `SELECT balance,is_downstream FROM users WHERE id=$1`, id).Scan(&u.Balance, &u.IsDownstream)
	return u, e
}
func TestAdminReceivablesPostgresAtomicRetryAndCollection(t *testing.T) {
	db, client := adminLedgerFixture(t)
	ctx := context.Background()
	s := &adminServiceImpl{entClient: client, userRepo: &ledgerUserRepo{db: db}}
	key := uuid.NewString()
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.CreditUserOnAccount(ctx, 8, 10.12345678, "first credit", 1, key)
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		require.NoError(t, e)
	}
	var balance, debt string
	var count int
	require.NoError(t, db.QueryRow(`SELECT balance::text FROM users WHERE id=8`).Scan(&balance))
	require.Equal(t, "10.12345678", balance)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*),SUM(amount)::text FROM admin_receivable_entries WHERE user_id=8`).Scan(&count, &debt))
	require.Equal(t, 1, count)
	require.Equal(t, "10.12345678", debt)
	var source string
	require.NoError(t, db.QueryRow(`SELECT credit_source FROM redeem_codes`).Scan(&source))
	require.Equal(t, "on_account", source)
	_, e := s.CreditUserOnAccount(ctx, 8, 20, "first credit", 1, key)
	require.Error(t, e)
	_, e = s.CreditUserOnAccount(ctx, 9, 10.12345678, "first credit", 1, key)
	require.Error(t, e)
	// A failure after crediting balance still rolls the balance and redeem record back.
	_, e = s.CreditUserOnAccount(ctx, 8, 99, "invalid actor", 999, uuid.NewString())
	require.Error(t, e)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM redeem_codes`).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, db.QueryRow(`SELECT balance::text FROM users WHERE id=8`).Scan(&balance))
	require.Equal(t, "10.12345678", balance)
	payKey := uuid.NewString()
	paidAt := time.Now().Add(-time.Hour).Truncate(time.Microsecond)
	for i := 0; i < 2; i++ {
		tx, e := db.Begin()
		require.NoError(t, e)
		entry, e := RecordReceivablePaymentTx(ctx, tx, 8, "3.12345678", "partial", "bank", 1, payKey, &paidAt)
		require.NoError(t, e)
		require.Equal(t, "7", entry.OutstandingAfter[:1])
		require.NoError(t, tx.Commit())
	}
	tx, e := db.Begin()
	require.NoError(t, e)
	_, e = RecordReceivablePaymentTx(ctx, tx, 8, "7.00000001", "overpay", "", 1, uuid.NewString(), nil)
	require.Error(t, e)
	require.NoError(t, tx.Rollback())
	tx, e = db.Begin()
	require.NoError(t, e)
	_, e = RecordReceivablePaymentTx(ctx, tx, 8, "7", "settled", "cash", 1, uuid.NewString(), nil)
	require.NoError(t, e)
	require.NoError(t, tx.Commit())
	require.NoError(t, db.QueryRow(`SELECT SUM(CASE WHEN kind='credit' THEN amount ELSE -amount END)::text FROM admin_receivable_entries WHERE user_id=8`).Scan(&debt))
	require.Equal(t, "0.00000000", debt)
	require.NoError(t, db.QueryRow(`SELECT balance::text FROM users WHERE id=8`).Scan(&balance))
	require.Equal(t, "10.12345678", balance)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM admin_receivable_entries`).Scan(&count))
	require.Equal(t, 3, count)
}
func TestAdminReceivablesPostgresConcurrentReceiptsCannotOverCollect(t *testing.T) {
	db, client := adminLedgerFixture(t)
	ctx := context.Background()
	s := &adminServiceImpl{entClient: client, userRepo: &ledgerUserRepo{db: db}}
	_, e := s.CreditUserOnAccount(ctx, 8, 10, "", 1, uuid.NewString())
	require.NoError(t, e)
	// Soft deletion does not hide or erase an outstanding receivable.
	_, e = db.Exec(`UPDATE users SET deleted_at=NOW() WHERE id=8`)
	require.NoError(t, e)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tx, e := db.Begin()
			if e != nil {
				results <- e
				return
			}
			defer tx.Rollback()
			_, e = RecordReceivablePaymentTx(ctx, tx, 8, "7", "", "", 1, uuid.NewString(), nil)
			if e == nil {
				e = tx.Commit()
			}
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	success, failed := 0, 0
	for e := range results {
		if e == nil {
			success++
		} else {
			failed++
		}
	}
	require.Equal(t, 1, success)
	require.Equal(t, 1, failed)
	var debt string
	require.NoError(t, db.QueryRow(`SELECT SUM(CASE WHEN kind='credit' THEN amount ELSE -amount END)::text FROM admin_receivable_entries WHERE user_id=8`).Scan(&debt))
	require.Equal(t, "3.00000000", debt)
}
func TestValidateReceivableAmount(t *testing.T) {
	for _, raw := range []string{"0", "-1", "NaN", "Inf", "0.000000001", "1000000000000", strings.Repeat("1", 65)} {
		t.Run(fmt.Sprintf("reject_%s", raw), func(t *testing.T) { _, e := ValidateReceivableAmount(raw); require.Error(t, e) })
	}
	for _, raw := range []string{"0.00000001", "1.25", "999999999999"} {
		_, e := ValidateReceivableAmount(raw)
		require.NoError(t, e)
	}
}
