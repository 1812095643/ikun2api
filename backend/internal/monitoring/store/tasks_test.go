package store

import (
	"context"
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"path/filepath"
	"testing"
	"time"
)

func TestTaskPageKeepsCompleteLifecyclesAndPriority(t *testing.T) {
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	now := time.Now()
	rows := []domain.Observation{}
	for n := 0; n < 4; n++ {
		for _, kind := range []string{"media.completed", "billing.applied"} {
			rows = append(rows, domain.Observation{Schema: 1, Site: "test", Source: "gateway", EventID: fmt.Sprintf("%d-%s", n, kind), TaskID: fmt.Sprint(n), Kind: kind, Quality: domain.Fresh, OccurredAt: now})
		}
	}
	if e = s.Ingest(ctx, rows, domain.Cursor{Source: "gateway"}); e != nil {
		t.Fatal(e)
	}
	page, next, e := s.TaskPage(ctx, "", 1, []string{TaskKey(rows[6])})
	if e != nil || len(page) != 4 || next == "" {
		t.Fatal(len(page), next, e)
	}
	seen := map[string]bool{}
	for _, o := range page {
		seen[o.TaskID] = true
	}
	if !seen["0"] || !seen["3"] {
		t.Fatal(seen)
	}
	page, next, e = s.TaskPage(ctx, next, 3, nil)
	if e != nil || len(page) != 6 || next != "" {
		t.Fatal(len(page), next, e)
	}
}
