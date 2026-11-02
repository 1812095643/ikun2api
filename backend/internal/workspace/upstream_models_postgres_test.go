package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

// Runs only against an explicitly named disposable schema. It does not start
// background services, send mail, or use any real provider credentials.
func TestUpstreamModelsPostgresRoundTrip(t *testing.T) {
	dsn := os.Getenv("IKUN_MODEL_TEST_DSN")
	if dsn == "" {
		t.Skip("isolated PostgreSQL fixture not configured")
	}
	schema := os.Getenv("IKUN_MODEL_TEST_SCHEMA")
	if !strings.HasPrefix(schema, "ikun_model_test_") {
		t.Fatal("disposable schema required")
	}
	for _, c := range schema {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' {
			t.Fatal("invalid schema")
		}
	}
	db, err := sql.Open("postgres", dsn+" search_path="+schema)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := db.ExecContext(ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec("CREATE SCHEMA " + schema)
	defer db.ExecContext(ctx, "DROP SCHEMA "+schema+" CASCADE")
	exec(`CREATE TABLE accounts(id BIGINT PRIMARY KEY,name TEXT,platform TEXT,type TEXT,credentials JSONB,proxy_id BIGINT,deleted_at TIMESTAMPTZ,updated_at TIMESTAMPTZ);
 CREATE TABLE scheduler_outbox(id BIGSERIAL PRIMARY KEY,event_type TEXT,account_id BIGINT,group_id BIGINT,payload JSONB);
 CREATE TABLE workspace_upstream_balances(id VARCHAR(64) PRIMARY KEY,url TEXT UNIQUE,name TEXT,config JSONB,secret_blob TEXT DEFAULT '',source_hash VARCHAR(64),revision BIGINT DEFAULT 1,snapshot JSONB DEFAULT '{}',alert JSONB DEFAULT '{}',refresh_requested BOOLEAN DEFAULT FALSE,requested_at TIMESTAMPTZ,lease_until TIMESTAMPTZ,last_checked_at TIMESTAMPTZ,next_check_at TIMESTAMPTZ DEFAULT NOW(),updated_at TIMESTAMPTZ DEFAULT NOW());`)
	exec(`INSERT INTO accounts VALUES(1,'fixture','openai','apikey','{"base_url":"https://up.example","api_key":"test-only","model_mapping":{"a":"a"},"preserved":"yes"}',NULL,NULL,NOW());
 INSERT INTO accounts VALUES(2,'oauth-fixture','anthropic','oauth','{"access_token":"test-only"}',NULL,NULL,NOW());`)
	id := upstreamBalanceID("https://up.example")
	exec("INSERT INTO workspace_upstream_balances(id,url,name,config,source_hash) VALUES($1,'https://up.example','Custom name','{}','before')", id)
	migration, err := os.ReadFile(os.Getenv("IKUN_MODEL_TEST_MIGRATION"))
	if err != nil {
		t.Fatal(err)
	}
	exec(string(migration))
	exec(string(migration))
	h := &Handler{db: db}
	groups, records, err := h.prepareUpstreamBalances(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || len(records) != 1 || records[0].Name != "Custom name" {
		t.Fatal("migration/discovery changed existing data or included OAuth")
	}
	r, err := h.loadModelRecord(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if r.Config.Enabled || r.Config.Notify {
		t.Fatal("monitor unexpectedly enabled")
	}
	now := time.Now().UTC()
	state := observeModelCatalog(upstreamModelAccountState{}, groups[0].Accounts[0], []string{"a"}, nil, now)
	state = observeModelCatalog(state, groups[0].Accounts[0], []string{"a", "b"}, nil, now)
	raw, _ := json.Marshal([]upstreamModelAccountState{state})
	exec("UPDATE workspace_upstream_models SET state=$2 WHERE upstream_id=$1", id, string(raw))
	if err = h.applyUpstreamModelAction(ctx, id, 9, upstreamModelAction{Revision: r.Revision, AccountID: 1, Action: "apply", Added: []string{"b"}}); err != nil {
		t.Fatal(err)
	}
	var preserved, mapping string
	var outbox int
	if err = db.QueryRow("SELECT credentials->>'preserved',credentials->'model_mapping'->>'b' FROM accounts WHERE id=1").Scan(&preserved, &mapping); err != nil {
		t.Fatal(err)
	}
	if preserved != "yes" || mapping != "b" {
		t.Fatal("mapping mutation lost unrelated credentials")
	}
	if err = db.QueryRow("SELECT count(*) FROM scheduler_outbox").Scan(&outbox); err != nil || outbox != 1 {
		t.Fatal("scheduler notification missing")
	}
	r, err = h.loadModelRecord(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(modelChanges(r.Accounts)) != 0 || len(r.History) != 1 {
		t.Fatal("applied change still pending")
	}
	err = h.applyUpstreamModelAction(ctx, id, 9, upstreamModelAction{Revision: r.Revision - 1, AccountID: 1, Action: "apply", Added: []string{"b"}})
	if err == nil {
		t.Fatal("replay succeeded")
	}
	t.Log(fmt.Sprintf("real SQL: migration twice, preserved data, OAuth exclusion, selected apply, outbox, history, stale replay passed (schema %s)", schema))
}
