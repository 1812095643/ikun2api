package adminapi

import (
	"context"
	"encoding/json"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/incidents"
	"github.com/MACOS-DO/sub4api/internal/monitoring/store"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAdminBoundaryConfigCASAndMissingSourceCoverage(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	api := (&API{Store: s, Manager: &incidents.Manager{Store: s}}).Handler()
	w := httptest.NewRecorder()
	api.ServeHTTP(w, httptest.NewRequest("GET", "/v1/summary", nil))
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	r := httptest.NewRequest("GET", "/v1/summary", nil)
	r.Header.Set("X-Ops-Actor", "admin:1")
	w = httptest.NewRecorder()
	api.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "未配置") {
		t.Fatal(w.Body.String())
	}
	c := domain.DefaultConfig()
	c.RequestID = "save1"
	raw, _ := json.Marshal(c)
	for n := 0; n < 2; n++ {
		r = httptest.NewRequest("PUT", "/v1/config", strings.NewReader(string(raw)))
		r.Header.Set("X-Ops-Actor", "admin:1")
		w = httptest.NewRecorder()
		api.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
	}
	c.RequestID = "stale"
	raw, _ = json.Marshal(c)
	r = httptest.NewRequest("PUT", "/v1/config", strings.NewReader(string(raw)))
	r.Header.Set("X-Ops-Actor", "admin:1")
	w = httptest.NewRecorder()
	api.ServeHTTP(w, r)
	if w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	saved, _ := s.Config(context.Background())
	if saved.Actor != "admin:1" || saved.Version != 1 {
		t.Fatal(saved)
	}
}

func TestConfiguredPostgresDoesNotClaimMissingSource(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.Ingest(context.Background(), nil, domain.Cursor{Source: "postgres.errors", LastSuccess: time.Now(), Quality: domain.Partial}); e != nil {
		t.Fatal(e)
	}
	api := (&API{Store: s, Manager: &incidents.Manager{Store: s}}).Handler()
	r := httptest.NewRequest("GET", "/v1/summary", nil)
	r.Header.Set("X-Ops-Actor", "admin:1")
	w := httptest.NewRecorder()
	api.ServeHTTP(w, r)
	if strings.Contains(w.Body.String(), "只读业务库来源未配置") {
		t.Fatal("configured PG reported absent")
	}
}
