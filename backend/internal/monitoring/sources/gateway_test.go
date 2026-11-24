package sources

import (
	"context"
	"encoding/json"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHandshakeGapAndCompatibility(t *testing.T) {
	snap := domain.Snapshot{Schema: 1, Site: "test", BootID: "new", Sequence: 10, Oldest: 5}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(snap) }))
	defer srv.Close()
	g := Gateway{URL: srv.URL}
	b, err := g.Poll(context.Background(), domain.Cursor{BootID: "old", Watermark: "2"}, time.Now())
	if err != nil || len(b.Observations) != 1 || b.Cursor.Quality != domain.Partial {
		t.Fatalf("gap not visible %+v %v", b, err)
	}
	snap.Schema = 99
	if _, err = g.Poll(context.Background(), domain.Cursor{}, time.Now()); err != ErrIncompatible {
		t.Fatal("unknown schema accepted", err)
	}
	if Authorized("Bearer short", "short") {
		t.Fatal("weak credential accepted")
	}
}
