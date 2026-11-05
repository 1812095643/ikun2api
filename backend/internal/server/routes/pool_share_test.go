//go:build unit

package routes

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/handler"
	"github.com/MACOS-DO/sub4api/internal/pkg/pagination"
	"github.com/MACOS-DO/sub4api/internal/pkg/usagestats"
	"github.com/MACOS-DO/sub4api/internal/server/middleware"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type poolShareRouteRepo struct {
	service.UsageLogRepository
	user    int64
	hash    string
	expires time.Time
	active  bool
	err     error
	queries int
	filters usagestats.UsageLogFilters
	params  pagination.PaginationParams
}

func (s *poolShareRouteRepo) SavePoolShare(_ context.Context, user int64, hash string, expires time.Time) error {
	s.user, s.hash, s.expires = user, hash, expires
	return nil
}
func (s *poolShareRouteRepo) ResolvePoolShare(_ context.Context, hash string, now time.Time) (int64, error) {
	if s.err != nil {
		return 0, s.err
	}
	if s.active && s.hash == hash && now.Before(s.expires) {
		return s.user, nil
	}
	return 0, nil
}
func (s *poolShareRouteRepo) RevokePoolShare(_ context.Context, user int64) error {
	if s.user == user {
		s.hash = ""
	}
	return nil
}
func (s *poolShareRouteRepo) ListWithFilters(_ context.Context, params pagination.PaginationParams, filters usagestats.UsageLogFilters) ([]service.UsageLog, *pagination.PaginationResult, error) {
	s.queries++
	s.params, s.filters = params, filters
	name, endpoint := "abcdefghi@example.test", "/v1/messages"
	rows := []service.UsageLog{{UserID: 42, AccountID: 9, APIKeyID: 7, AccountNameSnapshot: &name, RequestedModel: "client-model", Model: "upstream-model", InboundEndpoint: &endpoint, Group: &service.Group{ID: 2, Name: "Visible group"}, InputTokens: 468, OutputTokens: 142, CacheReadTokens: 100900, ActualCost: 99}}
	return rows, &pagination.PaginationResult{Page: params.Page, PageSize: params.PageSize, Total: 1}, nil
}

type poolShareRouteSettings struct {
	service.SettingRepository
	groups string
	err    error
}

func (s *poolShareRouteSettings) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{service.SettingKeyPoolUserVisibleGroupIDs: s.groups}, s.err
}
func (s *poolShareRouteSettings) GetValue(_ context.Context, key string) (string, error) {
	if key == service.SettingKeyPanelRateLimitSettings {
		return `{"enabled":true,"user_rpm":100,"heavy_rpm":1,"public_ip_rpm":2,"exempt_admin":true}`, nil
	}
	return "", service.ErrSettingNotFound
}
func newPoolShareRouteTest(t *testing.T, rateLimit bool) (*gin.Engine, *poolShareRouteRepo, *poolShareRouteSettings, *service.AccountPoolService, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	repo := &poolShareRouteRepo{active: true}
	settings := &poolShareRouteSettings{groups: "[2,3]"}
	settingService := service.NewSettingService(settings, nil)
	pool := service.NewAccountPoolService(nil, nil, nil, repo, settingService)
	link, err := pool.CreateShareLink(context.Background(), 42)
	require.NoError(t, err)
	var limiter *middleware.PanelRateLimiter
	if rateLimit {
		cache := miniredis.RunT(t)
		client := redis.NewClient(&redis.Options{Addr: cache.Addr()})
		t.Cleanup(func() { _ = client.Close() })
		limiter = middleware.NewPanelRateLimiter(client, settingService)
	}
	r := gin.New()
	jwt := middleware.JWTAuthMiddleware(func(c *gin.Context) { c.AbortWithStatus(http.StatusUnauthorized) })
	audit := middleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() })
	RegisterUserRoutes(r.Group("/api/v1"), &handler.Handlers{Pool: handler.NewPoolHandler(pool, service.NewUsageService(repo, nil, nil, nil))}, jwt, audit, nil, limiter)
	return r, repo, settings, pool, link.Token
}
func poolShareRequest(r *gin.Engine, method, path, token string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, nil)
	if token != "" {
		request.Header.Set("X-Pool-Share-Token", token)
	}
	request.Header.Set("Authorization", "Bearer unrelated-login-token")
	request.RemoteAddr = "8.8.4.4:12345"
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	return response
}
func TestPoolShareUsageOwnerGroupsAndPublicFields(t *testing.T) {
	r, repo, settings, _, token := newPoolShareRouteTest(t, false)
	for _, groups := range []string{"[2,3]", "[5]", "[]"} {
		settings.groups = groups
		w := poolShareRequest(r, "GET", "/api/v1/pool-share/usage?page=2&page_size=999&group_id=999&group_ids=999&account_id=99&api_key_id=99", token)
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Equal(t, int64(42), repo.filters.UserID)
		require.JSONEq(t, groups, mustPoolShareJSON(t, repo.filters.GroupIDs))
		require.Zero(t, repo.filters.GroupID)
		require.Zero(t, repo.filters.AccountID)
		require.Zero(t, repo.filters.APIKeyID)
		require.Equal(t, 2, repo.params.Page)
		require.Equal(t, 100, repo.params.PageSize)
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		require.Equal(t, "no-referrer", w.Header().Get("Referrer-Policy"))
		require.Equal(t, "noindex, noarchive", w.Header().Get("X-Robots-Tag"))
		require.Contains(t, w.Body.String(), "abc***ghi@example.test")
		require.Contains(t, w.Body.String(), `"model":"client-model"`)
		for _, field := range []string{"input_tokens", "output_tokens", "cache_read_tokens", "cache_creation_tokens", "group_name", "inbound_endpoint", "created_at", "duration_ms", "first_token_ms", "reasoning_effort"} {
			require.Contains(t, w.Body.String(), `"`+field+`"`)
		}
		for _, field := range []string{"abcdefghi@example.test", "upstream-model", "account_id", "user_id", "api_key_id", "group_id", "actual_cost", "total_cost", "credentials", "upstream_endpoint"} {
			require.NotContains(t, w.Body.String(), field)
		}
	}
}
func TestPoolShareUsageRejectsScopeEscapesAndSettingsFailure(t *testing.T) {
	r, repo, settings, _, token := newPoolShareRouteTest(t, false)
	for _, tc := range []struct {
		query string
		code  int
	}{{"?user_id=43", 403}, {"?user_id=abc", 400}, {"?start_time=bad", 400}} {
		w := poolShareRequest(r, "GET", "/api/v1/pool-share/usage"+tc.query, token)
		require.Equal(t, tc.code, w.Code, w.Body.String())
	}
	settings.err = errors.New("settings unavailable")
	w := poolShareRequest(r, "GET", "/api/v1/pool-share/usage", token)
	require.Equal(t, 503, w.Code, w.Body.String())
	require.Zero(t, repo.queries)
}
func TestPoolShareUsageAuthenticationLifecycle(t *testing.T) {
	for _, scenario := range []string{"missing", "wrong", "expired", "revoked", "rotated", "disabled", "storage-error"} {
		t.Run(scenario, func(t *testing.T) {
			r, repo, _, pool, token := newPoolShareRouteTest(t, false)
			want := 401
			switch scenario {
			case "missing":
				token = ""
			case "wrong":
				token = "pool_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789abcdefg"
			case "expired":
				repo.expires = time.Now().Add(-time.Second)
			case "revoked":
				require.NoError(t, pool.RevokeShareLink(context.Background(), 42))
			case "rotated":
				_, err := pool.CreateShareLink(context.Background(), 42)
				require.NoError(t, err)
			case "disabled":
				repo.active = false
			case "storage-error":
				repo.err = errors.New("share store unavailable")
				want = 503
			}
			w := poolShareRequest(r, "GET", "/api/v1/pool-share/usage?token="+token, token)
			require.Equal(t, want, w.Code, w.Body.String())
			require.Zero(t, repo.queries)
		})
	}
}
func TestPoolShareUsageDoesNotGrantOtherRoutesOrWrites(t *testing.T) {
	r, repo, _, _, token := newPoolShareRouteTest(t, false)
	for _, tc := range []struct {
		method, path string
		code         int
	}{
		{"GET", "/api/v1/pool/usage", 401}, {"GET", "/api/v1/usage", 401},
		{"GET", "/api/v1/user/profile", 401}, {"POST", "/api/v1/pool/share-link", 401},
		{"DELETE", "/api/v1/pool/share-link", 401}, {"POST", "/api/v1/pool-share/usage", 404},
		{"GET", "/api/v1/pool-share/accounts/9/requests", 404},
	} {
		w := poolShareRequest(r, tc.method, tc.path, token)
		require.Equal(t, tc.code, w.Code, tc.path)
	}
	// Tokens supplied through URL parameters are not accepted.
	require.Equal(t, 401, poolShareRequest(r, "GET", "/api/v1/pool-share/usage?token="+token, "").Code)
	require.Zero(t, repo.queries)
}
func TestPoolShareUsageAndAccountsShareRateLimits(t *testing.T) {
	r, repo, _, _, token := newPoolShareRouteTest(t, true)
	require.Equal(t, 200, poolShareRequest(r, "GET", "/api/v1/pool-share/usage", token).Code)
	w := poolShareRequest(r, "GET", "/api/v1/pool-share/accounts", token)
	require.Equal(t, 429, w.Code, w.Body.String())
	require.NotEmpty(t, w.Header().Get("Retry-After"))
	require.Equal(t, 1, repo.queries)
}
func TestPoolShareUsageInvalidTokensAreIPRateLimited(t *testing.T) {
	r, repo, _, _, _ := newPoolShareRouteTest(t, true)
	for _, want := range []int{401, 401, 429} {
		require.Equal(t, want, poolShareRequest(r, "GET", "/api/v1/pool-share/usage", "").Code)
	}
	require.Zero(t, repo.queries)
}

func mustPoolShareJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return string(data)
}
