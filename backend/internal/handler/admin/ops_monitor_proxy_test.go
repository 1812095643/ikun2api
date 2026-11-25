package admin

import (
	"github.com/MACOS-DO/sub4api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestMonitorProxyTrustBoundary(t *testing.T) {
	dir, e := os.MkdirTemp("", "ops-proxy-")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "admin.sock")
	listener, e := net.Listen("unix", socket)
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Ops-Actor") != "admin:7" || r.Header.Get("Authorization") != "" || r.Header.Get("X-Api-Key") != "" {
			t.Error("identity or credential leak")
		}
		io.WriteString(w, `{"code":0,"data":{"ok":true}}`)
	})}
	go srv.Serve(listener)
	defer srv.Close()
	for _, admin := range []bool{false, true} {
		r := gin.New()
		if admin {
			r.Use(func(c *gin.Context) {
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
				c.Set(string(middleware.ContextKeyUserRole), "admin")
			})
		}
		r.Any("/monitor/*path", NewMonitorProxy(socket))
		req := httptest.NewRequest("GET", "/monitor/summary", nil)
		req.Header.Set("X-Ops-Actor", "admin:999")
		req.Header.Set("Authorization", "Bearer private")
		req.Header.Set("X-Api-Key", "private")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		expected := 403
		if admin {
			expected = 200
		}
		if w.Code != expected {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}

func TestMonitorProxyAllowsChannelPathsOnly(t *testing.T) {
	for path, want := range map[string]bool{"/channels": true, "/channels/test": true, "/channels/feishu/chats": true, "/channels/other": false, "/channels/../config": false, "/summary": true} {
		if got := monitorPath.MatchString(path); got != want {
			t.Fatalf("%s: got %v want %v", path, got, want)
		}
	}
}
