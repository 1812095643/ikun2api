package sources

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPostgresReadOnlyContractAndLateCommit(t *testing.T) {
	dsn := os.Getenv("TEST_MONITOR_PG_DSN")
	if dsn == "" {
		t.Skip("isolated PostgreSQL DSN required")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// This fixture is disposable and deliberately has secret columns populated with sentinel values.
	_, err = db.Exec(`DROP SCHEMA public CASCADE;CREATE SCHEMA public;
 CREATE TABLE ops_error_logs(id bigserial,created_at timestamptz,client_request_id text,request_id text,user_id bigint,api_key_id bigint,account_id bigint,group_id bigint,platform text,model text,status_code int,upstream_status_code int,error_message text,provider_error_code text,error_type text,error_phase text,network_error_type text,duration_ms int,time_to_first_token_ms bigint,stream bool,request_body text,error_body text,upstream_errors jsonb,is_business_limited bool DEFAULT false);
 CREATE TABLE usage_logs(id bigserial,created_at timestamptz,request_id text,user_id bigint,api_key_id bigint,account_id bigint,group_id bigint,channel_id bigint,model text,duration_ms int,first_token_ms bigint,client_first_token_ms bigint,pre_forward_ms bigint,gateway_wait_ms bigint,upload_ms bigint,account_switch_count int,upstream_model_mismatch bool,actual_cost numeric);
 CREATE TABLE accounts(id bigserial,updated_at timestamptz,platform text,status text,temp_unschedulable_until timestamptz,schedulable bool,deleted_at timestamptz,credentials text);
 CREATE TABLE users(id bigserial,updated_at timestamptz,balance numeric,frozen_balance numeric,deleted_at timestamptz,status text,password_hash text);
	 CREATE TABLE api_keys(id bigserial,user_id bigint,deleted_at timestamptz,status text,funding_source text,key text);
	 CREATE TABLE usage_billing_dedup(id bigserial,created_at timestamptz,request_id text,api_key_id bigint);
	 CREATE TABLE universal_subscription_charges(id bigserial,created_at timestamptz,request_id text,api_key_id bigint,amount numeric);
 CREATE TABLE universal_subscriptions(id bigserial,updated_at timestamptz,user_id bigint,total_credit_usd numeric,total_used_usd numeric,daily_limit_usd numeric,daily_used_usd numeric,weekly_limit_usd numeric,weekly_used_usd numeric,monthly_limit_usd numeric,monthly_used_usd numeric,daily_window_start timestamptz,weekly_window_start timestamptz,monthly_window_start timestamptz,status text,starts_at timestamptz,expires_at timestamptz);
 CREATE TABLE payment_orders(id bigserial,updated_at timestamptz,user_id bigint,paid_at timestamptz,completed_at timestamptz,status text);
 CREATE TABLE universal_subscription_holds(id uuid,updated_at timestamptz,user_id bigint,api_key_id bigint,state text,expires_at timestamptz);
 CREATE TABLE ops_system_logs(id bigserial,created_at timestamptz,message text,account_id bigint,user_id bigint,platform text,model text,client_request_id text,request_id text);
 INSERT INTO accounts(id,updated_at,credentials) VALUES(1,now(),'secret sentinel');`)
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../../migrations/274_ops_monitor_readonly_views.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(migration)); err != nil {
		t.Fatal(err)
	}
	grants, err := os.ReadFile("../../../../deploy/ops-monitor/grants.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(grants)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("ALTER ROLE ops_monitor LOGIN"); err != nil {
		t.Fatal(err)
	}
	readonly, err := OpenPostgres(strings.Replace(dsn, "postgres@", "ops_monitor@", 1))
	if err != nil {
		t.Fatal(err)
	}
	defer readonly.Close()
	for _, q := range []string{"SELECT credentials FROM accounts", "SELECT password_hash FROM users", "INSERT INTO accounts(id) VALUES(2)", "SELECT request_body FROM ops_error_logs"} {
		if _, err = readonly.Exec(q); err == nil {
			t.Fatalf("forbidden query accepted: %s", q)
		}
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	_, err = db.Exec("INSERT INTO ops_error_logs(created_at,client_request_id,upstream_status_code,error_message,request_body) VALUES($1,'safe-id',429,'raw sensitive error','secret prompt')", now.Add(-time.Second))
	if err != nil {
		t.Fatal(err)
	}
	p := Postgres{DB: readonly, Site: "test", View: "errors", ActivatedAt: now.Add(-time.Minute)}
	batch, err := p.Poll(context.Background(), domain.Cursor{}, now)
	if err != nil || len(batch.Observations) != 1 {
		t.Fatalf("initial poll %+v %v", batch, err)
	}
	o := batch.Observations[0]
	if o.UpstreamStatus != 429 || o.Reason != "unclassified" {
		t.Fatalf("unsafe guessed cause %+v", o)
	}
	// Simulate a late commit with an older occurrence timestamp: bounded overlap must see it.
	_, err = db.Exec("INSERT INTO ops_error_logs(created_at,client_request_id,error_message) VALUES($1,'late-id','api key 日限额已用完')", now.Add(-30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	batch, err = p.Poll(context.Background(), batch.Cursor, now.Add(time.Second))
	if err != nil || len(batch.Observations) != 2 {
		t.Fatalf("late commit missed %+v %v", batch, err)
	}
	for _, v := range ViewNames {
		p.View = v
		if _, err = p.Poll(context.Background(), domain.Cursor{}, now.Add(time.Second)); err != nil {
			t.Fatalf("view %s: %v", v, err)
		}
	}
	if _, err = db.Exec("INSERT INTO ops_error_logs(created_at,request_id,upstream_errors) VALUES($1,'attempt-contract',$2::jsonb)", now, `[{"kind":"failover","account_id":3,"upstream_status_code":429,"message":"secret sentinel","upstream_url":"https://private.example/token"},{"kind":"request_error","account_id":"invalid","message":"secret sentinel"}]`); err != nil {
		t.Fatal(err)
	}
	attempts := Postgres{DB: readonly, Site: "test", View: "attempts", ActivatedAt: now.Add(-time.Minute)}
	ab, err := attempts.Poll(context.Background(), domain.Cursor{}, now.Add(time.Second))
	if err != nil || len(ab.Observations) != 2 {
		t.Fatalf("attempt view: %d %v", len(ab.Observations), err)
	}
	if ab.Observations[0].UpstreamStatus != 429 || ab.Observations[1].Reason != "transport_unknown" {
		t.Fatal("attempt semantics lost")
	}
	safe, _ := json.Marshal(ab.Observations)
	if strings.Contains(string(safe), "sentinel") || strings.Contains(string(safe), "private.example") {
		t.Fatal("attempt view leaked sensitive fields")
	}
	assertSubscriptionWindowReset(t, db, readonly)
	// Current snapshots must include unchanged old rows; event lookback is not a snapshot substitute.
	if _, err = db.Exec("INSERT INTO users VALUES(1,now()-interval '40 days',0,0,NULL,'active','secret');INSERT INTO api_keys(user_id,status,funding_source) VALUES(1,'active','balance')"); err != nil {
		t.Fatal(err)
	}
	p.View = "balances"
	snap, err := p.Poll(context.Background(), domain.Cursor{}, now)
	if err != nil || len(snap.Observations) != 1 || snap.Observations[0].Quality != domain.Fresh {
		t.Fatalf("old current balance missing %+v %v", snap, err)
	}
}

// Called by the isolated PG contract test after fixture setup.
func assertSubscriptionWindowReset(t *testing.T, db *sql.DB, reader *sql.DB) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO universal_subscriptions(id,updated_at,user_id,total_credit_usd,total_used_usd,daily_limit_usd,daily_used_usd,weekly_limit_usd,weekly_used_usd,monthly_limit_usd,monthly_used_usd,daily_window_start,weekly_window_start,monthly_window_start,status,starts_at,expires_at)
 VALUES(1,now(),1,100,20,10,10,20,20,30,30,now()-interval '48 hours',now()-interval '169 hours',now()-interval '721 hours','active',now()-interval '40 days',now()+interval '5 days'),
 (2,now(),2,NULL,0,10,10,20,20,30,30,now(),now(),now(),'active',now()-interval '1 day',now()+interval '5 days')`)
	if err != nil {
		t.Fatal(err)
	}
	p := Postgres{DB: reader, Site: "test", View: "subscriptions"}
	b, err := p.Poll(context.Background(), domain.Cursor{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range b.Observations {
		if o.UserID == 1 {
			if o.N("daily_remaining") != 10 || o.N("weekly_remaining") != 20 || o.N("monthly_remaining") != 30 {
				t.Fatal("expired windows still exhausted", o)
			}
		}
		if o.UserID == 2 {
			if o.N("daily_remaining") != 0 || o.N("weekly_remaining") != 0 || o.N("monthly_remaining") != 0 || o.Has("remaining") {
				t.Fatal("active windows or unlimited total misread", o)
			}
		}
	}
}
