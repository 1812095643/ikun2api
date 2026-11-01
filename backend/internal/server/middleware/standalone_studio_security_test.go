package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MACOS-DO/sub4api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestStandaloneStudioPolicyDoesNotWeakenConsole(t *testing.T) {
	r := gin.New()
	r.Use(SecurityHeaders(config.CSPConfig{Enabled: true}, nil))
	r.NoRoute(func(c *gin.Context) { c.Status(http.StatusOK) })
	for _, path := range []string{"/admin/settings", "/home", "/tutorial", "/image-studio-other"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		require.NotContains(t, w.Header().Get("Content-Security-Policy"), "'unsafe-eval'", path)
		require.NotContains(t, w.Header().Get("Content-Security-Policy"), "ws://127.0.0.1:*", path)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/image-studio/canvas/1", nil))
	policy := w.Header().Get("Content-Security-Policy")
	require.Contains(t, policy, "'unsafe-eval'")
	require.Contains(t, policy, "ws://127.0.0.1:*")
	require.Contains(t, policy, "media-src 'self' data: blob: https:")
	require.Contains(t, policy, "'nonce-")
	require.Equal(t, "DENY", w.Header().Get("X-Frame-Options"))
}
