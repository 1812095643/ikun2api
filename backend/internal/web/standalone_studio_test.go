//go:build embed

package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type studioSettingsFixture struct{}

func (studioSettingsFixture) GetPublicSettingsForInjection(context.Context) (any, error) {
	return map[string]any{"site_name": "Another <site>", "site_logo": "/brand.png"}, nil
}

func TestStandaloneStudioUsesOwnEntryWithSharedBrand(t *testing.T) {
	files := fstest.MapFS{
		"image-studio/index.html":              {Data: []byte(`<html><head><title>Studio</title><link rel="icon" href="/logo.svg"></head><body><div id="root"></div></body></html>`)},
		"image-studio/static/test-12345678.js": {Data: []byte(`console.log("studio")`)},
	}
	server := &FrontendServer{distFS: files, fileServer: http.FileServer(http.FS(files)), settings: studioSettingsFixture{}}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if !server.serveStandaloneStudio(c) {
			c.Next()
		}
	})
	for _, path := range []string{"/image-studio/", "/image-studio/canvas/example", "/image-studio/config"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusOK, w.Code)
		require.Contains(t, w.Body.String(), `id="root"`)
		require.Contains(t, w.Body.String(), `Another &lt;site&gt;`)
		require.Contains(t, w.Body.String(), `window.__APP_CONFIG__=`)
		require.Contains(t, w.Body.String(), `href="/brand.png"`)
		require.Equal(t, "no-cache", w.Header().Get("Cache-Control"))
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/image-studio", nil))
	require.Equal(t, http.StatusTemporaryRedirect, w.Code)
	require.Equal(t, "/image-studio/", w.Header().Get("Location"))
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/image-studio/static/missing.js", nil))
	require.Equal(t, http.StatusNotFound, w.Code)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/image-studio/static/test-12345678.js", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, staticAssetsCacheControl, w.Header().Get("Cache-Control"))
}
