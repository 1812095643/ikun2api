package workspace

import (
	"database/sql"
	"errors"
	"net/http/httptest"
	"testing"

	"context"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/MACOS-DO/sub4api/internal/server/middleware"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestVaultSecurityPolicyDefaultsAndFailure(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  any
		err  error
		want int
	}{
		{"default unprotected", nil, sql.ErrNoRows, 204},
		{"disabled", `{"two_factor_enabled":false}`, nil, 204},
		{"enabled", `{"two_factor_enabled":true}`, nil, 503},
		{"missing field", `{}`, nil, 503},
		{"null field", `{"two_factor_enabled":null}`, nil, 503},
		{"storage down", nil, errors.New("offline"), 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			q := mock.ExpectQuery("SELECT value FROM workspace_runtime WHERE key").WithArgs(vaultSecurityKey)
			if tc.err != nil {
				q.WillReturnError(tc.err)
			} else {
				q.WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(tc.raw))
			}
			h := &Handler{db: db}
			r := gin.New()
			r.POST("/", h.vaultSensitive, func(c *gin.Context) { c.Status(204) })
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("POST", "/", nil))
			require.Equal(t, tc.want, w.Code)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestVaultSecurityPostgresToggleAndGuard(t *testing.T) {
	h, r := toolsPostgresFixture(t)
	r.GET("/security", h.getVaultSecurity)
	r.PUT("/security", h.saveVaultSecurity)
	code, body := toolsRequest(t, r, "GET", "/security", nil)
	require.Equal(t, 200, code)
	require.Equal(t, false, body["data"].(map[string]any)["two_factor_enabled"])
	// Saving the default off policy does not require configuring TOTP first.
	code, _ = toolsRequest(t, r, "PUT", "/security", map[string]any{"two_factor_enabled": false})
	require.Equal(t, 200, code)
	// Never enable a policy that the current session cannot unlock.
	code, _ = toolsRequest(t, r, "PUT", "/security", map[string]any{"two_factor_enabled": true})
	require.Equal(t, 503, code)
	var count int
	require.NoError(t, h.db.QueryRow("SELECT count(*) FROM workspace_vault_audit WHERE action='security_enable'").Scan(&count))
	require.Zero(t, count)
	_, err := h.db.Exec("UPDATE workspace_runtime SET value=$2 WHERE key=$1", vaultSecurityKey, `{"two_factor_enabled":true}`)
	require.NoError(t, err)
	code, body = toolsRequest(t, r, "GET", "/security", nil)
	require.Equal(t, 200, code)
	require.Equal(t, true, body["data"].(map[string]any)["two_factor_enabled"])
	code, _ = toolsRequest(t, r, "PUT", "/security", map[string]any{"two_factor_enabled": false})
	require.Equal(t, 503, code)
	code, _ = toolsRequest(t, r, "PUT", "/security", map[string]any{})
	require.Equal(t, 400, code)
}

type vaultSecurityUserRepo struct{ service.UserRepository }

func (*vaultSecurityUserRepo) GetUserAvatar(context.Context, int64) (*service.UserAvatar, error) {
	return nil, nil
}

func (*vaultSecurityUserRepo) GetByID(context.Context, int64) (*service.User, error) {
	return &service.User{ID: 1, TotpEnabled: false}, nil
}

func TestVaultSecuritySaveDefaultOffWithoutTOTP(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT value FROM workspace_runtime").WithArgs(vaultSecurityKey).WillReturnError(sql.ErrNoRows)
	mock.ExpectExec("INSERT INTO workspace_runtime").WithArgs(vaultSecurityKey, `{"two_factor_enabled":false}`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO workspace_vault_audit").WithArgs(int64(1), "settings", "security_disable").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	h := &Handler{db: db, admin: &toolsFixtureAdmin{}}
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1}) })
	r.PUT("/security", h.saveVaultSecurity)
	code, body := toolsRequest(t, r, "PUT", "/security", map[string]any{"two_factor_enabled": false})
	require.Equal(t, 200, code)
	require.Equal(t, false, body["data"].(map[string]any)["two_factor_enabled"])
	require.NoError(t, mock.ExpectationsWereMet())
}
func TestVaultSecurityCannotEnableBeforeConfiguringTOTP(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT value FROM workspace_runtime").WithArgs(vaultSecurityKey).WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()
	h := &Handler{db: db, vaultTOTP: &service.TotpService{}, vaultUsers: service.NewUserService(&vaultSecurityUserRepo{}, nil, nil, nil)}
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
		c.Set("auth_method", "jwt")
	})
	r.PUT("/security", h.saveVaultSecurity)
	code, body := toolsRequest(t, r, "PUT", "/security", map[string]any{"two_factor_enabled": true})
	require.Equal(t, 403, code)
	require.Contains(t, []any{body["code"], body["reason"]}, "STEP_UP_TOTP_NOT_ENABLED")
	require.NoError(t, mock.ExpectationsWereMet())
}
