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

func TestPoolSharePageCrossSiteFramingIsReadOnlyAndPathScoped(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(map[bool]string{true: "CSP-enabled", false: "CSP-disabled"}[enabled], func(t *testing.T) {
			r := gin.New()
			r.Use(SecurityHeaders(config.CSPConfig{Enabled: enabled}, nil))
			r.NoRoute(func(c *gin.Context) { c.Status(http.StatusOK) })
			for _, path := range []string{"/share/pool?share_token=pool_TEST&token=foreign-jwt&ui_mode=embedded", "/share/pool/"} {
				for _, method := range []string{http.MethodGet, http.MethodHead} {
					w := httptest.NewRecorder()
					r.ServeHTTP(w, httptest.NewRequest(method, path, nil))
					require.Empty(t, w.Header().Get("X-Frame-Options"), path)
					require.Equal(t, "no-referrer", w.Header().Get("Referrer-Policy"), path)
					require.Equal(t, "no-store", w.Header().Get("Cache-Control"), path)
					require.Equal(t, "noindex, noarchive", w.Header().Get("X-Robots-Tag"), path)
					policy := w.Header().Get("Content-Security-Policy")
					if enabled {
						require.Contains(t, policy, "frame-ancestors http: https:")
						require.NotContains(t, policy, "frame-ancestors 'none'")
						require.NotContains(t, policy, "'unsafe-eval'")
						require.Contains(t, policy, "'nonce-")
					} else {
						require.Empty(t, policy)
					}
				}
			}
			for _, path := range []string{"/admin/accounts", "/login?ui_mode=embedded", "/api/v1/pool-share/usage", "/share/pool/extra", "/share/pool/../admin", "/share%2fpool", "/share/p%6fol", "/share/pool%2f"} {
				w := httptest.NewRecorder()
				r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
				require.Equal(t, "DENY", w.Header().Get("X-Frame-Options"), path)
				if enabled {
					require.Contains(t, w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'", path)
				}
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/share/pool?ui_mode=embedded", nil))
			require.Equal(t, "DENY", w.Header().Get("X-Frame-Options"))
		})
	}
}

func TestPoolShareReplacesOnlyItsFrameAncestorsIncludingDuplicateCustomDirectives(t *testing.T) {
	policy := poolShareCSP("default-src 'self'; frame-src https://parent.example; frame-ancestors 'none'; FRAME-ANCESTORS 'self'; script-src 'self'")
	require.Equal(t, 1, strings.Count(strings.ToLower(policy), "frame-ancestors"))
	require.Contains(t, policy, "frame-ancestors http: https:")
	require.Contains(t, policy, "frame-src https://parent.example")
	require.Contains(t, policy, "script-src 'self'")
}
