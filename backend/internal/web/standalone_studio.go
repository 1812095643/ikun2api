//go:build embed

package web

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/MACOS-DO/sub4api/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

// The studio keeps its own page and router, but shares installation branding.
func (s *FrontendServer) serveStandaloneStudio(c *gin.Context) bool {
	path := c.Request.URL.Path
	if path != "/image-studio" && !strings.HasPrefix(path, "/image-studio/") {
		return false
	}
	if path == "/image-studio" {
		c.Redirect(http.StatusTemporaryRedirect, "/image-studio/")
		c.Abort()
		return true
	}
	clean := strings.TrimPrefix(path, "/")
	if info, err := fs.Stat(s.distFS, clean); err == nil && !info.IsDir() && !strings.HasSuffix(clean, ".html") {
		applyStaticAssetCacheHeaders(c.Writer.Header(), clean)
		s.fileServer.ServeHTTP(c.Writer, c.Request)
		c.Abort()
		return true
	}
	if strings.HasPrefix(clean, "image-studio/static/") {
		c.AbortWithStatus(http.StatusNotFound)
		return true
	}
	html, err := fs.ReadFile(s.distFS, "image-studio/index.html")
	if err != nil {
		c.AbortWithStatus(http.StatusNotFound)
		return true
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	settings, err := s.settings.GetPublicSettingsForInjection(ctx)
	if err == nil {
		if raw, err := json.Marshal(settings); err == nil {
			page := *s
			page.baseHTML = html
			html = page.injectSettings(raw)
		}
	}
	html = replaceNoncePlaceholder(html, middleware.GetNonceFromContext(c))
	c.Header("Cache-Control", "no-cache")
	c.Data(http.StatusOK, "text/html; charset=utf-8", html)
	c.Abort()
	return true
}
