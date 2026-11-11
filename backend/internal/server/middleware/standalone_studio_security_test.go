package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
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

// Returned image bytes are fetched by built-in and user-authored adapters.
// Permit local result URLs only on the standalone studio, including deep links.
func TestStandaloneStudioImageResultConnections(t *testing.T) {
	r := gin.New()
	r.Use(SecurityHeaders(config.CSPConfig{Enabled: true}, nil))
	r.NoRoute(func(c *gin.Context) { c.Status(http.StatusOK) })
	for _, path := range []string{"/image-studio", "/image-studio/", "/image-studio/canvas/1", "/login", "/admin/settings", "/image-studio-other"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		var connections []string
		for _, directive := range strings.Split(w.Header().Get("Content-Security-Policy"), ";") {
			fields := strings.Fields(directive)
			if len(fields) > 0 && fields[0] == "connect-src" {
				connections = fields[1:]
			}
		}
		require.NotEmpty(t, connections, path)
		studio := path == "/image-studio" || strings.HasPrefix(path, "/image-studio/")
		for _, source := range []string{"data:", "blob:"} {
			if studio {
				require.Contains(t, connections, source, path)
			} else {
				require.NotContains(t, connections, source, path)
			}
		}
	}
}
