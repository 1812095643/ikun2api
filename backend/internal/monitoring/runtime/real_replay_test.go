package runtime

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/detectors"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"os"
	"sort"
	"testing"
	"time"
)

func TestRealDatabaseErrorCoverage(t *testing.T) {
	rows := readRealReplay(t)
	cfg := domain.DefaultConfig()
	cfg.Site = "replay"
	for _, r := range detectors.Catalog {
		cfg.Rules[r.ID] = domain.RuleConfig{Enabled: true, Owner: "monitor", Version: 1}
	}
	seen := map[string]bool{}
	covered, excluded, missing := 0, 0, 0
	byRule := map[string]int{}
	misses := map[string]int{}
	for _, o := range rows {
		if o.Source != "postgres.errors" {
			continue
		}
		seen[o.EventID] = true
		if o.Reason == "key_daily_limit" {
			excluded++
			continue
		}
		fs := detectors.Evaluate([]domain.Observation{o}, o.OccurredAt, cfg)
		rules := map[string]bool{}
		for _, f := range fs {
			if !f.Healthy {
				rules[f.Rule] = true
			}
		}
		if len(rules) == 0 {
			missing++
			misses[o.Reason]++
		} else {
			covered++
			for r := range rules {
				byRule[r]++
			}
		}
	}
	t.Logf("records=%d covered=%d normal_daily_excluded=%d missed=%d rules=%v missing_reasons=%v", len(seen), covered, excluded, missing, byRule, misses)
	if missing > 0 {
		t.Errorf("real error records without findings: %d", missing)
	}
	if len(seen) != 666 || excluded != 57 {
		t.Fatalf("unexpected fixed production fixture counts %d/%d", len(seen), excluded)
	}
}

// Input is produced by the exact migration SELECTs in a production READ ONLY
// transaction, not invented observations. No schema is installed remotely.
func readRealReplay(t *testing.T) []domain.Observation {
	t.Helper()
	path := os.Getenv("TEST_MONITOR_REPLAY_JSON")
	if path == "" {
		t.Skip("sanitized production SELECT projection required")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows := []domain.Observation{}
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 4096), 1<<20)
	for scan.Scan() {
		var r struct {
			Source   string             `json:"source"`
			ID       string             `json:"id"`
			At       time.Time          `json:"at"`
			Evidence domain.Observation `json:"evidence"`
		}
		if err = json.Unmarshal(scan.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		o := r.Evidence
		o.Schema = 1
		o.Site = "replay"
		o.Source = "postgres." + r.Source
		o.EventID = fmt.Sprintf("%s:%d", r.ID, r.At.UnixMicro())
		o.OccurredAt = r.At
		o.ObservedAt = r.At
		o.Quality = domain.Partial
		domain.NormalizeIdentifiers(&o)
		if err = o.Validate(); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, o)
	}
	if err = scan.Err(); err != nil {
		t.Fatal(err)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].OccurredAt.Before(rows[j].OccurredAt) })
	return rows
}

func TestRealSystemBillingErrorsWithoutTaskIDStillNotify(t *testing.T) {
	rows := readRealReplay(t)
	seen, hit := 0, 0
	for _, o := range rows {
		if o.Source != "postgres.system" || o.Kind != "billing.metadata_failed" {
			continue
		}
		seen++
		for _, f := range detectors.Evaluate([]domain.Observation{o}, o.OccurredAt, domain.DefaultConfig()) {
			if f.Rule == "B03" && !f.Healthy {
				hit++
			}
		}
	}
	if seen != 2 || hit != 2 {
		t.Fatalf("real billing metadata failures detected=%d expected=%d (fixture=2)", hit, seen)
	}
}

func TestRealUpstreamAttemptsAreNotReducedToLastError(t *testing.T) {
	rows := readRealReplay(t)
	attempts, limited, missed := 0, 0, 0
	for _, o := range rows {
		if o.Source != "postgres.attempts" {
			continue
		}
		attempts++
		if o.UpstreamStatus == 429 {
			limited++
		}
		found := false
		for _, f := range detectors.Evaluate([]domain.Observation{o}, o.OccurredAt, domain.DefaultConfig()) {
			if !f.Healthy {
				found = true
			}
		}
		if !found {
			missed++
		}
	}
	if attempts != 373 || limited != 157 || missed != 0 {
		t.Fatalf("attempts=%d expected373; 429=%d expected157; missed=%d", attempts, limited, missed)
	}
}
