package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

func TestPoolHandlerUsageDisplayFieldsKeepPublicAllowlist(t *testing.T) {
	name, effort, forwarded := "abcdefghi@example.test", "max", "high"
	endpoint, upstream := "/v1/messages", "https://private-upstream.example.test"
	duration, firstToken := 10000, 9999
	now := time.Now().UTC().Truncate(time.Second)
	repo := &userUsageRepoCapture{listRows: []service.UsageLog{{
		UserID: 42, AccountID: 9, APIKeyID: 7, AccountNameSnapshot: &name,
		Model: "internal-model", RequestedModel: "client-model",
		ReasoningEffort: &forwarded, RequestedReasoningEffort: &effort,
		InboundEndpoint: &endpoint, UpstreamEndpoint: &upstream,
		Group:     &service.Group{ID: 1, Name: "Selected group"},
		CreatedAt: now, DurationMs: &duration, FirstTokenMs: &firstToken, Stream: true,
		InputTokens: 468, OutputTokens: 500, CacheReadTokens: 100900,
		CacheCreationTokens: 281, CacheCreation5mTokens: 200, CacheCreation1hTokens: 81,
		ActualCost: 99, TotalCost: 100,
	}, {Model: "legacy-model", ReasoningEffort: &forwarded}}}
	router := newPoolTestRouter(repo, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/pool/usage", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	var body struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Data.Items, 2)
	row := body.Data.Items[0]
	require.Equal(t, map[string]any{
		"name": "abc***ghi@example.test", "model": "client-model",
		"reasoning_effort": "max", "inbound_endpoint": endpoint, "group_name": "Selected group",
		"created_at": now.Format(time.RFC3339), "duration_ms": float64(duration), "first_token_ms": float64(firstToken), "stream": true,
		"input_tokens": float64(468), "output_tokens": float64(500), "cache_read_tokens": float64(100900),
		"cache_creation_tokens": float64(281), "cache_creation_5m_tokens": float64(200), "cache_creation_1h_tokens": float64(81),
		"image_input_tokens": float64(0), "image_output_tokens": float64(0),
	}, row)
	legacy := body.Data.Items[1]
	require.Equal(t, "legacy-model", legacy["model"])
	require.Equal(t, "high", legacy["reasoning_effort"])
	require.Empty(t, legacy["group_name"])
	require.Nil(t, legacy["inbound_endpoint"])
	require.Nil(t, legacy["first_token_ms"])
	require.Equal(t, &effort, repo.listRows[0].RequestedReasoningEffort)
}

func TestPoolHandlerUsageUsesSamePrimaryEmailAsAccountList(t *testing.T) {
	name := "Internal Claude account"
	repo := &userUsageRepoCapture{listRows: []service.UsageLog{{UserID: 42, AccountID: 9, AccountNameSnapshot: &name, Account: &service.Account{ID: 9, Name: name, Extra: map[string]any{"email_address": "abcdefghi@example.test"}, Credentials: map[string]any{"access_token": "DO_NOT_EXPOSE"}}}}}
	rec := httptest.NewRecorder()
	newPoolTestRouter(repo, true).ServeHTTP(rec, httptest.NewRequest("GET", "/pool/usage", nil))
	require.Equal(t, 200, rec.Code)
	require.Contains(t, rec.Body.String(), "abc***ghi@example.test")
	for _, value := range []string{name, "abcdefghi@example.test", "DO_NOT_EXPOSE", "credentials", "email_address"} {
		require.NotContains(t, rec.Body.String(), value)
	}
}
