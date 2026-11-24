package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func openAIRecoveryPostgres(t *testing.T) (*accountRepository, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("IKUN_RELOGIN_TEST_DSN")
	if dsn == "" {
		t.Skip("未配置隔离 PostgreSQL")
	}
	base, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	schema := "relogin_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = base.Exec("CREATE SCHEMA " + schema)
	require.NoError(t, err)
	db, err := sql.Open("postgres", dsn+" search_path="+schema)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close(); _, _ = base.Exec("DROP SCHEMA " + schema + " CASCADE"); base.Close() })
	_, err = db.Exec(`CREATE TABLE accounts(
	 id bigint PRIMARY KEY, platform text DEFAULT 'openai',type text DEFAULT 'oauth',parent_account_id bigint,
	 status text DEFAULT 'active',schedulable boolean DEFAULT TRUE,credentials jsonb DEFAULT '{}',extra jsonb DEFAULT '{}',
	 proxy_id bigint,error_message text DEFAULT '',deleted_at timestamptz,updated_at timestamptz DEFAULT NOW(),
	 rate_limit_reset_at timestamptz,overload_until timestamptz,temp_unschedulable_reason text);
	 CREATE TABLE scheduler_outbox(id bigserial,event_type text,account_id bigint,group_id bigint,payload jsonb);
	 INSERT INTO accounts(id,credentials,extra,rate_limit_reset_at,temp_unschedulable_reason) VALUES
	 (1,'{"email":"synthetic@example.test","access_token":"old","refresh_token":"old-rt","model_mapping":{"allowed":"allowed"}}',
	 '{"custom_setting":true}',NOW()+INTERVAL '1 hour','proxy guard');`)
	require.NoError(t, err)
	return newAccountRepositoryWithSQL(nil, db, nil), db
}

func readOpenAIRecoveryAccount(t *testing.T, db *sql.DB) *service.Account {
	t.Helper()
	a := &service.Account{}
	var credentials, extra []byte
	err := db.QueryRow(`SELECT id,platform,type,parent_account_id,status,schedulable,credentials,extra,proxy_id,error_message,updated_at
	 FROM accounts WHERE id=1`).Scan(&a.ID, &a.Platform, &a.Type, &a.ParentAccountID, &a.Status, &a.Schedulable, &credentials, &extra, &a.ProxyID, &a.ErrorMessage, &a.UpdatedAt)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(credentials, &a.Credentials))
	require.NoError(t, json.Unmarshal(extra, &a.Extra))
	return a
}

func TestOpenAIRecoveryPostgresPauseApplyAndManualBoundaries(t *testing.T) {
	for _, mutation := range []string{"正常恢复", "重新授权", "更换代理", "手动暂停", "新增故障", "免费套餐"} {
		t.Run(mutation, func(t *testing.T) {
			repo, db := openAIRecoveryPostgres(t)
			ctx := context.Background()
			before := readOpenAIRecoveryAccount(t, db)
			applied, err := repo.PauseOpenAIOAuthIfUnchanged(ctx, before, service.OpenAIOAuthRejectedMessage, true)
			require.NoError(t, err)
			require.True(t, applied)
			paused := readOpenAIRecoveryAccount(t, db)
			require.False(t, paused.Schedulable)
			require.True(t, service.OpenAIOAuthRecoveryPending(paused))
			replacement := service.MergeCredentials(paused.Credentials, map[string]any{"access_token": "new", "refresh_token": "new-rt"})
			var query string
			switch mutation {
			case "重新授权":
				query = `UPDATE accounts SET credentials=credentials||'{"access_token":"another"}'::jsonb`
			case "更换代理":
				query = `UPDATE accounts SET proxy_id=7`
			case "手动暂停":
				query = `UPDATE accounts SET extra=jsonb_set(extra,'{openai_oauth_recovery}','null'::jsonb)`
			case "新增故障":
				query = `UPDATE accounts SET error_message='another failure'`
			case "免费套餐":
				replacement["plan_type"] = "free"
			}
			if query != "" {
				_, err = db.Exec(query)
				require.NoError(t, err)
			}
			applied, err = repo.ApplyOpenAIOAuthReloginIfUnchanged(ctx, paused, replacement)
			require.NoError(t, err)
			current := readOpenAIRecoveryAccount(t, db)
			if mutation == "正常恢复" || mutation == "免费套餐" {
				require.True(t, applied)
				require.Equal(t, "active", current.Status)
				require.Equal(t, mutation == "正常恢复", current.Schedulable)
				require.Equal(t, "new", current.GetOpenAIAccessToken())
				require.Equal(t, map[string]any{"allowed": "allowed"}, current.Credentials["model_mapping"])
				require.Equal(t, true, current.Extra["custom_setting"])
				require.NotContains(t, current.Extra, service.OpenAIOAuthRecoveryExtraKey)
			} else {
				require.False(t, applied)
				require.False(t, current.Schedulable)
				require.NotEqual(t, "new", current.GetOpenAIAccessToken())
			}
			var reason string
			var reset time.Time
			require.NoError(t, db.QueryRow("SELECT temp_unschedulable_reason,rate_limit_reset_at FROM accounts WHERE id=1").Scan(&reason, &reset))
			require.Equal(t, "proxy guard", reason)
			require.True(t, reset.After(time.Now()))
			var events int
			require.NoError(t, db.QueryRow("SELECT count(*) FROM scheduler_outbox").Scan(&events))
			if applied {
				require.Equal(t, 2, events)
			} else {
				require.Equal(t, 1, events)
			}
		})
	}
}

func TestOpenAIRecoveryPostgresOldFailureCannotPauseNewAuthorization(t *testing.T) {
	repo, db := openAIRecoveryPostgres(t)
	before := readOpenAIRecoveryAccount(t, db)
	_, err := db.Exec(`UPDATE accounts SET credentials=credentials||'{"access_token":"reauthorized"}'::jsonb`)
	require.NoError(t, err)
	applied, err := repo.PauseOpenAIOAuthIfUnchanged(context.Background(), before, service.OpenAIOAuthRejectedMessage, true)
	require.NoError(t, err)
	require.False(t, applied)
	require.True(t, readOpenAIRecoveryAccount(t, db).Schedulable)
	var events int
	require.NoError(t, db.QueryRow("SELECT count(*) FROM scheduler_outbox").Scan(&events))
	require.Zero(t, events)
}

func TestOpenAIRecoveryPostgresTerminalAndManualReloginPreservePauseOwnership(t *testing.T) {
	repo, db := openAIRecoveryPostgres(t)
	ctx := context.Background()
	before := readOpenAIRecoveryAccount(t, db)
	applied, err := repo.PauseOpenAIOAuthIfUnchanged(ctx, before, service.OpenAIOAuthRejectedMessage, true)
	require.NoError(t, err)
	require.True(t, applied)
	paused := readOpenAIRecoveryAccount(t, db)
	applied, err = repo.PauseOpenAIOAuthIfUnchanged(ctx, paused, "account_disabled", false)
	require.NoError(t, err)
	require.True(t, applied)
	terminal := readOpenAIRecoveryAccount(t, db)
	require.False(t, service.OpenAIOAuthRecoveryPending(terminal))
	require.False(t, terminal.Schedulable)
	applied, err = repo.PauseOpenAIOAuthIfUnchanged(ctx, terminal, service.OpenAIOAuthRejectedMessage, true)
	require.NoError(t, err)
	require.False(t, applied)
	_, err = db.Exec(`UPDATE accounts SET extra=jsonb_set(extra,'{openai_oauth_recovery}','null'::jsonb),error_message='OpenAI OAuth credentials permanently rejected'`)
	require.NoError(t, err)
	manual := readOpenAIRecoveryAccount(t, db)
	applied, err = repo.ApplyOpenAIOAuthReloginIfUnchanged(ctx, manual, service.MergeCredentials(manual.Credentials, map[string]any{"access_token": "manual-new"}))
	require.NoError(t, err)
	require.True(t, applied)
	current := readOpenAIRecoveryAccount(t, db)
	require.Equal(t, "manual-new", current.GetOpenAIAccessToken())
	require.Equal(t, "active", current.Status)
	require.False(t, current.Schedulable)
}

func TestOpenAIRecoveryPostgresOutboxFailureRollsBackPause(t *testing.T) {
	repo, db := openAIRecoveryPostgres(t)
	before := readOpenAIRecoveryAccount(t, db)
	_, err := db.Exec(`ALTER TABLE scheduler_outbox ADD CHECK (event_type='reject-test-event')`)
	require.NoError(t, err)
	applied, err := repo.PauseOpenAIOAuthIfUnchanged(context.Background(), before, service.OpenAIOAuthRejectedMessage, true)
	require.Error(t, err)
	require.False(t, applied)
	current := readOpenAIRecoveryAccount(t, db)
	require.Equal(t, "active", current.Status)
	require.True(t, current.Schedulable)
	require.NotContains(t, current.Extra, service.OpenAIOAuthRecoveryExtraKey)
}
