package workspace

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func serveTestFeed(h *Handler, target string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/workspace/news/latest", nil)
	c.Request.Header.Set("Authorization", "Bearer private-user-token")
	c.Request.Header.Set("Cookie", "session=private-user-session")
	h.serveFeed(c, target)
	return w
}

func TestPublicFeedCachesSuccessWithoutForwardingCredentials(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("private credentials reached the feed source")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"id":7}],"hasMore":false}`))
	}))
	defer server.Close()
	h := &Handler{client: server.Client()}
	first := serveTestFeed(h, server.URL+"?limit=20")
	second := serveTestFeed(h, server.URL+"?limit=20")
	if first.Code != 200 || second.Code != 200 || calls.Load() != 1 {
		t.Fatalf("unexpected status or upstream calls: %d %d %d", first.Code, second.Code, calls.Load())
	}
	if second.Header().Get("X-Workspace-Cache") != "hit" || first.Header().Get("X-Workspace-Fetched-At") == "" || second.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("missing cache freshness metadata")
	}
}

func TestPublicFeedServesStaleDataBeforeBackgroundRefreshCompletes(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_, _ = w.Write([]byte(`{"items":[{"id":8}]}`))
	}))
	defer server.Close()
	defer close(release)
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	target := server.URL + "?limit=20"
	h := &Handler{client: server.Client(), feedCache: map[string]cachedFeed{target: {data: json.RawMessage(`{"items":[{"id":7}]}`), fetchedAt: old}}}
	response := serveTestFeed(h, target)
	if response.Code != 200 || response.Header().Get("X-Workspace-Cache") != "stale" || response.Header().Get("X-Workspace-Fetched-At") != old.UTC().Format(time.RFC3339) {
		t.Fatal("stale snapshot was not returned with its original timestamp")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("background refresh did not start")
	}
}

func TestPublicFeedUnavailableBacksOffWithoutErasingSnapshot(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(522)
	}))
	defer server.Close()
	target := server.URL + "?limit=20"
	old := cachedFeed{data: json.RawMessage(`{"items":[{"id":7}]}`), fetchedAt: time.Now().Add(-time.Hour)}
	h := &Handler{client: server.Client(), feedCache: map[string]cachedFeed{target: old}}
	if _, err := h.refreshPublicFeed(target); err == nil {
		t.Fatal("upstream error was treated as success")
	}
	if _, err := h.refreshPublicFeed(target); err == nil || calls.Load() != 1 {
		t.Fatal("failed endpoint was retried without backoff")
	}
	response := serveTestFeed(h, target)
	if response.Code != 200 || response.Body.String() != string(old.data) || response.Header().Get("X-Workspace-Cache") != "stale" {
		t.Fatal("upstream failure erased the last successful feed")
	}
}

func TestPublicFeedNoSnapshotReturnsUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(522) }))
	defer server.Close()
	h := &Handler{client: server.Client()}
	response := serveTestFeed(h, server.URL+"?limit=20")
	if response.Code != http.StatusServiceUnavailable || response.Header().Get("Retry-After") != "60" {
		t.Fatal("missing truthful unavailable response")
	}
}

func TestPublicFeedConcurrentRequestsShareFetch(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer server.Close()
	h := &Handler{client: server.Client()}
	var workers sync.WaitGroup
	for i := 0; i < 16; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if serveTestFeed(h, server.URL+"?limit=20").Code != 200 {
				t.Error("concurrent request failed")
			}
		}()
	}
	workers.Wait()
	if calls.Load() != 1 {
		t.Fatalf("duplicate upstream fetches: %d", calls.Load())
	}
}

func TestPublicFeedRejectsInvalidSuccessBodies(t *testing.T) {
	for _, body := range []string{`<html>unavailable</html>`, `null`, `[]`, `{"code":503,"message":"unavailable"}`} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
			defer server.Close()
			h := &Handler{client: server.Client()}
			if _, err := h.fetchPublicFeed(context.Background(), server.URL); err == nil {
				t.Fatal("invalid feed payload was accepted")
			}
		})
	}
}

func TestPublicFeedPersistsOnlyBoundedPublicPages(t *testing.T) {
	if !persistFeed("https://news.macos.do/api/v1/news/latest?limit=20") || !persistFeed("https://news.macos.do/api/v1/news/latest?limit=30") || !persistFeed("https://reset.macos.do/api/monitor/summary?limit=20") {
		t.Fatal("fixed first page cannot be persisted")
	}
	for _, target := range []string{"https://news.macos.do/api/v1/news/latest?limit=20&cursor=x", "https://news.macos.do/api/v1/news/latest?limit=20&categoryId=7", "https://reset.macos.do/api/monitor/posts/1/reply-context?limit=20"} {
		if persistFeed(target) {
			t.Fatal("unbounded query persisted")
		}
	}
}
