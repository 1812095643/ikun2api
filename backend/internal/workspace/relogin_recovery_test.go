package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/MACOS-DO/sub4api/migrations"
	"github.com/stretchr/testify/require"
)

func TestReloginTerminalFailureRequiresStructuredEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		terminal  bool
	}{
		{"已停用", `{"error":{"code":"account_deactivated"}}`, true},
		{"密码失效", `{"payload":{"error_code":"invalid_credentials"}}`, true},
		{"令牌撤销", `{"error":"token_revoked"}`, true},
		{"普通文案不是封号依据", `{"error":{"message":"account is disabled"}}`, false},
		{"429不触发", `{"error":{"code":"rate_limit_exceeded"}}`, false},
		{"登录服务超时", `{"error_code":"timeout"}`, false},
		{"等待验证码", `{"challenge":{"type":"email_otp"},"error_code":"invalid_credentials"}`, false},
		{"有成功凭据", `{"credential":{"access_token":"synthetic"},"error_code":"token_revoked"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var result map[string]any
			require.NoError(t, json.Unmarshal([]byte(tc.raw), &result))
			require.Equal(t, tc.terminal, reloginTerminalFailure(result) != "")
		})
	}
}

func TestReloginResultRejectsChangedCredentialsAndProxy(t *testing.T) {
	a := &service.Account{ID: 1, Platform: "openai", Type: "oauth", Credentials: map[string]any{"access_token": "old", "refresh_token": "rt"}}
	run := reloginRun{Trigger: "auto", AccountFingerprint: service.OpenAIOAuthRecoveryFingerprint(a)}
	require.True(t, reloginResultMatchesAccount(run, a))
	a.Credentials["access_token"] = "new"
	require.False(t, reloginResultMatchesAccount(run, a))
	a.Credentials["access_token"] = "old"
	proxy := int64(3)
	a.ProxyID = &proxy
	require.False(t, reloginResultMatchesAccount(run, a))
	run.AccountFingerprint = ""
	require.False(t, reloginResultMatchesAccount(run, a))
}

type reloginRecoveryAdmin struct {
	service.AdminService
	account *service.Account
}

func (a reloginRecoveryAdmin) ListAccounts(context.Context, int, int, string, string, string, string, int64, string, string, string) ([]service.Account, int64, error) {
	return []service.Account{*a.account}, 1, nil
}

type reloginRecoveryRepo struct {
	service.AccountRepository
	account *service.Account
	pauses  int
}

func (r *reloginRecoveryRepo) GetByID(context.Context, int64) (*service.Account, error) {
	return r.account, nil
}
func (r *reloginRecoveryRepo) PauseOpenAIOAuthIfUnchanged(_ context.Context, expected *service.Account, message string, auto bool) (bool, error) {
	r.pauses++
	copy := *expected
	copy.Extra = map[string]any{service.OpenAIOAuthRecoveryExtraKey: map[string]any{"fingerprint": service.OpenAIOAuthRecoveryFingerprint(expected), "auto_retry": auto, "reason": message}}
	copy.Status, copy.Schedulable, copy.ErrorMessage = "error", false, message
	r.account = &copy
	return true, nil
}
func (*reloginRecoveryRepo) ApplyOpenAIOAuthReloginIfUnchanged(context.Context, *service.Account, map[string]any) (bool, error) {
	panic("本测试不执行登录写回")
}

func TestAutomaticReloginPausesAtThresholdWithoutSavedLoginAndHonorsCooldown(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		failures, recent, pauses int
	}{
		{"次数不足", 4, 0, 0}, {"达到阈值", 5, 0, 1}, {"重登间隔未到", 5, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			cfg := defaultReloginConfig()
			raw, err := json.Marshal(cfg)
			require.NoError(t, err)
			mock.ExpectQuery("SELECT value FROM workspace_runtime").WithArgs("relogin_settings").WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(raw))
			a := &service.Account{ID: 1, Platform: "openai", Type: "oauth", Status: "active", Schedulable: true, Credentials: map[string]any{"email": "example@example.test", "access_token": "synthetic", "refresh_token": "synthetic"}}
			repo := &reloginRecoveryRepo{account: a}
			recovery := service.NewOAuthRefreshAPI(repo, nil)
			for range tc.failures {
				recovery.RecordOpenAIOAuthAuthFailure(a, 401, nil)
			}
			if tc.failures >= 5 {
				mock.ExpectQuery("SELECT count\\(\\*\\) FROM workspace_relogin_runs").WithArgs(int64(1), 360).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(tc.recent))
				if tc.recent == 0 {
					mock.ExpectQuery("SELECT secret_blob").WithArgs("example@example.test").WillReturnError(sql.ErrNoRows)
				}
			}
			mock.ExpectExec("INSERT INTO workspace_runtime").WithArgs("relogin_status", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
			h := &Handler{db: db, admin: reloginRecoveryAdmin{account: a}, recovery: recovery}
			queued, err := h.automaticRelogin(context.Background())
			require.NoError(t, err)
			require.Zero(t, queued)
			require.Equal(t, tc.pauses, repo.pauses)
			if tc.pauses > 0 {
				require.False(t, repo.account.Schedulable)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestReloginAutoRunReservationDoesNotBypassRunningOrChallenge(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	cfg := defaultReloginConfig()
	raw, _ := json.Marshal(cfg)
	mock.ExpectQuery("SELECT value FROM workspace_runtime").WithArgs("relogin_settings").WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(raw))
	mock.ExpectBegin()
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("INSERT INTO workspace_relogin_runs").WithArgs("example@example.test", int64(1), "fingerprint", 360).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectRollback()
	h := &Handler{db: db}
	id := int64(1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = h.insertReloginRun(ctx, "example@example.test", &id, "auto", "fingerprint")
	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestReloginAutoReservationPostgresSerializesInstances(t *testing.T) {
	h, _ := toolsPostgresFixture(t)
	_, err := h.db.Exec(`CREATE TABLE workspace_relogin_runs(id bigserial PRIMARY KEY,email text,account_id bigint,
	 trigger text,status text DEFAULT 'running',started_at timestamptz DEFAULT NOW());`)
	require.NoError(t, err)
	migration, err := migrations.FS.ReadFile("281_relogin_account_snapshot.sql")
	require.NoError(t, err)
	for range 2 {
		_, err = h.db.Exec(string(migration))
		require.NoError(t, err)
	}
	require.NoError(t, h.setRuntime(context.Background(), "relogin_settings", defaultReloginConfig()))
	var started atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := int64(7)
			_, e := h.insertReloginRun(context.Background(), "synthetic@example.test", &id, "auto", "synthetic")
			if e == nil {
				started.Add(1)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, int32(1), started.Load())
	_, err = h.db.Exec(`UPDATE workspace_relogin_runs SET status='challenge',started_at=NOW()-INTERVAL '2 days'`)
	require.NoError(t, err)
	id := int64(7)
	_, err = h.insertReloginRun(context.Background(), "synthetic@example.test", &id, "auto", "synthetic")
	require.Error(t, err)
	_, err = h.db.Exec(`UPDATE workspace_relogin_runs SET status='failed'`)
	require.NoError(t, err)
	_, err = h.insertReloginRun(context.Background(), "synthetic@example.test", &id, "auto", "synthetic")
	require.NoError(t, err)
}
