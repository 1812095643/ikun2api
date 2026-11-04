package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	middleware2 "github.com/MACOS-DO/sub4api/internal/server/middleware"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newPoolTestRouter(repo *userUsageRepoCapture, authenticated bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	pool := service.NewAccountPoolService(nil, nil, nil, nil, nil)
	h := NewPoolHandler(pool, service.NewUsageService(repo, nil, nil, nil))
	router := gin.New()
	if authenticated {
		router.Use(func(c *gin.Context) {
			c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 42})
		})
	}
	router.GET("/pool/overview", h.Overview)
	router.GET("/pool/accounts", h.Accounts)
	router.GET("/pool/usage", h.Usage)
	router.GET("/pool/accounts/:id/requests", h.AccountRequests)
	return router
}

func TestPoolHandlerAuthenticationAndUserScope(t *testing.T) {
	for _, path := range []string{"/pool/overview", "/pool/accounts", "/pool/usage", "/pool/accounts/9/requests"} {
		t.Run(path, func(t *testing.T) {
			for _, test := range []struct {
				auth   bool
				query  string
				status int
			}{
				{false, "", http.StatusUnauthorized},
				{true, "?user_id=1", http.StatusForbidden},
				{true, "?user_id=abc", http.StatusBadRequest},
			} {
				rec := httptest.NewRecorder()
				newPoolTestRouter(&userUsageRepoCapture{}, test.auth).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path+test.query, nil))
				require.Equal(t, test.status, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestPoolHandlerUsageMaskingAndAccountFilter(t *testing.T) {
	original := "private-account@example.test"
	for _, path := range []string{"/pool/usage?user_id=42&account_id=9", "/pool/accounts/9/requests?user_id=42"} {
		t.Run(path, func(t *testing.T) {
			repo := &userUsageRepoCapture{listRows: []service.UsageLog{{UserID: 42, AccountID: 9, AccountNameSnapshot: &original}}}
			router := newPoolTestRouter(repo, true)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.Equal(t, int64(42), repo.listFilters.UserID)
			require.Equal(t, int64(9), repo.listFilters.AccountID)
			require.NotContains(t, rec.Body.String(), original)
			require.Contains(t, rec.Body.String(), service.MaskAccountName(original))
			require.Equal(t, &original, repo.listRows[0].AccountNameSnapshot)
		})
	}
}
