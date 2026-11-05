package workspace

import (
	"database/sql"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
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
		{"default protected", nil, sql.ErrNoRows, 503},
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
	require.Equal(t, true, body["data"].(map[string]any)["two_factor_enabled"])
	// No verified step-up dependencies means an enabled policy cannot be disabled.
	code, _ = toolsRequest(t, r, "PUT", "/security", map[string]any{"two_factor_enabled": false})
	require.Equal(t, 503, code)
	var count int
	require.NoError(t, h.db.QueryRow("SELECT count(*) FROM workspace_runtime WHERE key=$1", vaultSecurityKey).Scan(&count))
	require.Zero(t, count)
	_, err := h.db.Exec("INSERT INTO workspace_runtime(key,value) VALUES($1,$2)", vaultSecurityKey, `{"two_factor_enabled":false}`)
	require.NoError(t, err)
	code, body = toolsRequest(t, r, "GET", "/security", nil)
	require.Equal(t, 200, code)
	require.Equal(t, false, body["data"].(map[string]any)["two_factor_enabled"])
	code, _ = toolsRequest(t, r, "PUT", "/security", map[string]any{"two_factor_enabled": true})
	require.Equal(t, 200, code)
	require.NoError(t, h.db.QueryRow("SELECT count(*) FROM workspace_vault_audit WHERE action='security_enable'").Scan(&count))
	require.Equal(t, 1, count)
	code, _ = toolsRequest(t, r, "PUT", "/security", map[string]any{})
	require.Equal(t, 400, code)
}
