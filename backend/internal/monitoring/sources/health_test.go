package sources

import (
	"context"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLocalHealthDoesNotInventHostFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer srv.Close()
	h := LocalHealth{Site: "test", DiskPath: t.TempDir(), GatewayURL: srv.URL, RedisAddress: "127.0.0.1:1"}
	batch, err := h.Poll(context.Background(), domain.Cursor{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, o := range batch.Observations {
		if err = o.Validate(); err != nil {
			t.Fatal(err)
		}
		if o.Kind == "dependency.health" && o.TaskID == "gateway" {
			found = true
			if o.N("healthy") != 0 {
				t.Fatal("503 marked healthy")
			}
		}
	}
	if !found {
		t.Fatal("missing local gateway evidence")
	}
}
