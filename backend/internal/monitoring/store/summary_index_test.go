package store

import (
	"path/filepath"
	"strings"
	"testing"
)

// Summary queries must seek the newest rows before reading their JSON payloads.
// Sorting the full retained outbox becomes slow under the monitor CPU quota.
func TestSummaryQueriesUseOrderedIndexesAfterReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	for pass := 0; pass < 2; pass++ {
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, query := range []string{
			"SELECT body FROM incidents ORDER BY updated DESC LIMIT 1000",
			"SELECT json_set(body,'$.state',state,'$.attempts',attempts) FROM notification_outbox ORDER BY next_attempt DESC LIMIT 1000",
		} {
			rows, err := s.db.Query("EXPLAIN QUERY PLAN " + query)
			if err != nil {
				t.Fatal(err)
			}
			indexed := false
			for rows.Next() {
				var id, parent, unused int
				var detail string
				if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
					t.Fatal(err)
				}
				if strings.Contains(detail, "TEMP B-TREE") {
					t.Errorf("summary sorts retained history: %s", detail)
				}
				indexed = indexed || strings.Contains(detail, "USING INDEX")
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			rows.Close()
			if !indexed {
				t.Error("summary lacks an ordered index")
			}
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
