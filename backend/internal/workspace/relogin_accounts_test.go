package workspace

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestReloginAccountsCarriesQuotaSnapshotWithoutCredentials(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectQuery("SELECT email FROM workspace_relogin_accounts").
		WillReturnRows(sqlmock.NewRows([]string{"email"}).AddRow("owner@example.test"))
	now := time.Now().UTC()
	account := &service.Account{
		ID: 41, Name: "OAuth", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Credentials: map[string]any{
			"email": "Owner@example.test", "plan_type": "pro",
			"access_token": "synthetic-sensitive-access", "refresh_token": "synthetic-sensitive-refresh",
		},
		Extra:  map[string]any{"codex_5h_used_percent": 23.0, "codex_7d_used_percent": 67.0},
		Status: service.StatusActive, Schedulable: true, UpdatedAt: now, LastUsedAt: &now,
	}
	h := &Handler{db: db, admin: reloginRecoveryAdmin{account: account}}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/workspace/relogin/accounts", nil)
	h.reloginAccounts(c)

	require.Equal(t, http.StatusOK, w.Code)
	var envelope struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.Len(t, envelope.Data.Items, 1)
	item := envelope.Data.Items[0]
	require.Equal(t, "owner@example.test", item["email"])
	require.Equal(t, true, item["has_imported"])
	require.Equal(t, "pro", item["plan_type"])
	require.Equal(t, service.PlatformOpenAI, item["platform"])
	require.Equal(t, service.AccountTypeOAuth, item["type"])
	require.Equal(t, 23.0, item["extra"].(map[string]any)["codex_5h_used_percent"])
	require.Equal(t, 67.0, item["extra"].(map[string]any)["codex_7d_used_percent"])
	require.Equal(t, now.Format(time.RFC3339Nano), item["last_used_at"])
	require.NotContains(t, item["credentials"], "access_token")
	require.NotContains(t, item["credentials"], "refresh_token")
	require.NotContains(t, w.Body.String(), "synthetic-sensitive")
	require.NoError(t, mock.ExpectationsWereMet())
}
