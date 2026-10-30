//go:build unit || integration

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"os"
	"testing"
	"time"
)

// Run only against a dedicated empty database created for this test. The DSN and
// source migration tree are explicit; production databases are never inferred.
func TestUpgradeOfficialSnapshotsPreserveData(t *testing.T) {
	dsn := os.Getenv("IKUN_UPGRADE_TEST_DSN")
	root := os.Getenv("IKUN_UPGRADE_TEST_MIGRATIONS")
	if dsn == "" || root == "" {
		t.Skip("dedicated rehearsal inputs not supplied")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	var tables int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM information_schema.tables WHERE table_schema='public'").Scan(&tables))
	require.Zero(t, tables, "rehearsal requires a new empty database")
	require.NoError(t, applyMigrationsFS(ctx, db, os.DirFS(root)))
	hash, err := bcrypt.GenerateFromPassword([]byte("MigrationFixtureOnly!23"), bcrypt.MinCost)
	require.NoError(t, err)
	var user, group, account int64
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO users(email,password_hash,role,status,balance) VALUES('migration-fixture@example.invalid',$1,'admin','active',123.456789) RETURNING id", string(hash)).Scan(&user))
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO groups(name,platform) VALUES('migration fixture','anthropic') RETURNING id").Scan(&group))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO accounts(name,platform,type,credentials,status) VALUES('fixture','anthropic','apikey','{"api_key":"fixture-only","base_url":"https://upstream.example.invalid"}','disabled') RETURNING id`).Scan(&account))
	_, err = db.ExecContext(ctx, "INSERT INTO api_keys(user_id,key,name,group_id,status) VALUES($1,'sk-ikun-migration-fixture','fixture',$2,'active')", user, group)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "INSERT INTO settings(key,value) VALUES('migration.fixture.preserved','自定义中文邮件与站点配置')")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "INSERT INTO user_subscriptions(user_id,group_id,starts_at,expires_at,status,daily_usage_usd,weekly_usage_usd,monthly_usage_usd) VALUES($1,$2,NOW()-INTERVAL '1 day',NOW()+INTERVAL '20 days','active',1.25,4.5,9.75)", user, group)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "INSERT INTO usage_logs(user_id,api_key_id,account_id,request_id,model,input_tokens,output_tokens,total_cost) VALUES($1,(SELECT id FROM api_keys WHERE user_id=$1 LIMIT 1),$2,'migration-history-fixture','fixture-model',123,456,0.125)", user, account)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "INSERT INTO redeem_codes(code,type,value,status,used_by,used_at) VALUES('MIGRATION-FIXTURE','balance',12.5,'used',$1,NOW())", user)
	require.NoError(t, err)
	snapshot := func() string {
		var data string
		q := `SELECT json_build_array((SELECT json_agg(json_build_array(id,email,password_hash,role,status,balance) ORDER BY id) FROM users),(SELECT json_agg(json_build_array(id,platform,type,credentials,status) ORDER BY id) FROM accounts),(SELECT json_agg(json_build_array(id,user_id,key,group_id,status) ORDER BY id) FROM api_keys),(SELECT value FROM settings WHERE key='migration.fixture.preserved'),(SELECT json_agg(json_build_array(user_id,group_id,status,starts_at,expires_at,daily_usage_usd,weekly_usage_usd,monthly_usage_usd) ORDER BY id) FROM user_subscriptions),(SELECT json_agg(json_build_array(user_id,api_key_id,account_id,request_id,model,input_tokens,output_tokens,total_cost) ORDER BY id) FROM usage_logs),(SELECT json_agg(json_build_array(code,type,value,status,used_by,used_at) ORDER BY id) FROM redeem_codes))::text`
		require.NoError(t, db.QueryRowContext(ctx, q).Scan(&data))
		return data
	}
	before := snapshot()
	if os.Getenv("IKUN_UPGRADE_TEST_BASELINE_ONLY") == "1" {
		fmt.Println("OFFICIAL_BASELINE_READY")
		return
	}
	require.NoError(t, ValidateUpgradeSchema(ctx, db))
	require.NoError(t, ApplyMigrations(ctx, db))
	require.Equal(t, before, snapshot())
	require.NoError(t, ApplyMigrations(ctx, db))
	require.Equal(t, before, snapshot())
	var migrations int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations").Scan(&migrations))
	report, _ := json.Marshal(map[string]any{"data_preserved": true, "repeat_migration_idempotent": true, "migrations": migrations, "source": root})
	fmt.Println(string(report))
}
