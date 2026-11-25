//go:build embed

package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// Exercises the real build output: seo.json must ship with the frontend.
func TestFrontendServerServesSEOPagesAndFiles(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &mockSettingsProvider{settings: map[string]any{"site_name": "心链词元", "api_base_url": "https://sub.example.com", "home_content": "<h1>连接模型</h1><p>为开发者提供模型 API 接入</p>"}}
	server, err := NewFrontendServer(provider)
	if err != nil {
		t.Fatal(err)
	}
	if server.seo == nil || len(server.seo.Docs) == 0 {
		t.Fatal("seo.json missing from the embedded build")
	}
	r := gin.New()
	r.Use(server.Middleware())
	get := func(path string) (int, http.Header, string) {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = "token.example.com"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code, w.Header(), w.Body.String()
	}
	_, h1, home := get("/")
	for _, want := range []string{`<link rel="canonical" href="https://token.example.com/">`, "FAQPage", `<div id="app"><div class="seo-static"><h1>连接模型</h1>`, "window.__APP_CONFIG__"} {
		if !strings.Contains(home, want) {
			t.Fatalf("home missing %s", want)
		}
	}
	_, h2, art := get("/tutorial/claude")
	if !strings.Contains(art, "<title>Claude Code / Anthropic - 心链词元新手教程</title>") || !strings.Contains(art, "https://sub.example.com") || strings.Contains(art, "__IKUN_API_BASE__") {
		t.Fatal("article page not rendered")
	}
	if h1.Get("ETag") == "" || h1.Get("ETag") == h2.Get("ETag") {
		t.Fatal("each page needs its own ETag", h1.Get("ETag"), h2.Get("ETag"))
	}
	_, _, dash := get("/dashboard")
	if !strings.Contains(dash, `content="noindex, nofollow"`) || strings.Contains(dash, "seo-static") {
		t.Fatal("console routes must be noindex")
	}
	code, hdr, robots := get("/robots.txt")
	if code != 200 || !strings.HasPrefix(hdr.Get("Content-Type"), "text/plain") || !strings.Contains(robots, "Sitemap: https://token.example.com/sitemap.xml") {
		t.Fatal(code, robots)
	}
	_, hdr, sm := get("/sitemap.xml")
	if !strings.Contains(hdr.Get("Content-Type"), "xml") || !strings.Contains(sm, "https://token.example.com/tutorial/claude") {
		t.Fatal(sm)
	}
	_, _, llms := get("/llms.txt")
	if !strings.HasPrefix(llms, "# 心链词元") || !strings.Contains(llms, "https://token.example.com/tutorial/claude") {
		t.Fatal(llms)
	}
	_, _, full := get("/llms-full.txt")
	if len(full) < 10000 || strings.Contains(full, "<svg") {
		t.Fatal("llms-full too small or contains markup", len(full))
	}
}
