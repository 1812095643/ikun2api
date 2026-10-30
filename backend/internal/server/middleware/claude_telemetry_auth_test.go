//go:build unit

package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/config"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestClaudeTelemetryAuthDoesNotRelaxModelBilling(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		group := &service.Group{ID: 42, Status: service.StatusActive, Hydrated: true}
		user := &service.User{ID: 7, Status: service.StatusActive, Balance: 0}
		key := &service.APIKey{ID: 100, Key: "telemetry-test", UserID: user.ID, User: user, Group: group, GroupID: &group.ID, Status: service.StatusAPIKeyQuotaExhausted, Quota: 1, QuotaUsed: 1}
		if disabled {
			key.Status = service.StatusDisabled
		}
		touches := 0
		repo := &stubApiKeyRepo{
			getByKey:       func(context.Context, string) (*service.APIKey, error) { value := *key; return &value, nil },
			updateLastUsed: func(context.Context, int64, time.Time) error { touches++; return nil },
		}
		cfg := &config.Config{RunMode: config.RunModeStandard}
		svc := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg)
		r := gin.New()
		r.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(svc, nil, cfg)))
		for _, path := range []string{"/api/event_logging/v2/batch", "/v1/messages"} {
			r.POST(path, func(c *gin.Context) { c.Status(http.StatusOK) })
		}
		for _, path := range []string{"/api/event_logging/v2/batch", "/v1/messages"} {
			req := httptest.NewRequest(http.MethodPost, path, nil)
			req.Header.Set("x-api-key", key.Key)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if !disabled && path == "/api/event_logging/v2/batch" {
				require.Equal(t, http.StatusOK, w.Code)
			} else {
				require.NotEqual(t, http.StatusOK, w.Code)
			}
		}
		require.Zero(t, touches)
	}
}
