package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	middleware2 "github.com/MACOS-DO/sub4api/internal/server/middleware"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type poolScopeSettingRepo struct{ service.SettingRepository }

func (poolScopeSettingRepo) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{service.SettingKeyPoolUserVisibleGroupID: "1"}, nil
}

func TestPoolHandlerUsageEnforcesGroupAndOmitsAmounts(t *testing.T) {
	repo := &userUsageRepoCapture{listRows: []service.UsageLog{{UserID: 42, AccountID: 9, ActualCost: 99, TotalCost: 100}}}
	svc := service.NewAccountPoolService(nil, nil, nil, nil, service.NewSettingService(poolScopeSettingRepo{}, nil))
	h := NewPoolHandler(svc, service.NewUsageService(repo, nil, nil, nil))
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 42}) })
	r.GET("/pool/usage", h.Usage)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/pool/usage?group_id=2&account_id=999", nil))
	require.Equal(t, 200, w.Code)
	require.Equal(t, []int64{1}, repo.listFilters.GroupIDs)
	require.Equal(t, int64(42), repo.listFilters.UserID)
	for _, field := range []string{"actual_cost", "total_cost", "account_id", "api_key_id", "user_id", "group_id"} {
		require.NotContains(t, w.Body.String(), field)
	}
}

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
			if path == "/pool/usage?user_id=42&account_id=9" {
				require.Equal(t, 200, rec.Code)
				require.Equal(t, int64(42), repo.listFilters.UserID)
			} else {
				require.Equal(t, 403, rec.Code)
				require.Contains(t, rec.Body.String(), "POOL_DETAILS_DISABLED")
			}

			require.Zero(t, repo.listFilters.AccountID)
			require.NotContains(t, rec.Body.String(), original)
			require.NotContains(t, rec.Body.String(), "account_id")
			require.NotContains(t, rec.Body.String(), "actual_cost")
			require.Equal(t, &original, repo.listRows[0].AccountNameSnapshot)
		})
	}
}
