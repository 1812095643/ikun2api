package runtime

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/incidents"
	"github.com/MACOS-DO/sub4api/internal/monitoring/notify"
	"github.com/MACOS-DO/sub4api/internal/monitoring/store"
	_ "modernc.org/sqlite"
)

// replaySender records what reaches Feishu: a send without a message ID is a
// new chat message (a push); a send with one edits an existing card silently.
type replaySender struct {
	n       int
	pushes  []domain.Delivery
	pushAt  []time.Time
	patches int
	clock   *fakeClock
}

func (s *replaySender) Send(_ context.Context, d domain.Delivery, msg, _ string) (string, error) {
	if msg != "" {
		s.patches++
		return msg, nil
	}
	s.n++
	s.pushes = append(s.pushes, d)
	s.pushAt = append(s.pushAt, s.clock.at)
	return fmt.Sprintf("om_replay%d", s.n), nil
}

// TestNoiseReplay feeds an exported production observation stream through the
// real evaluator, incident manager and notification worker with a fake clock,
// and reports how many Feishu messages would have been pushed.
// MONITOR_NOISE_REPLAY=<obs.jsonl> MONITOR_NOISE_CONFIG=<config.json> MONITOR_NOISE_REPORT=<out.json>
func TestNoiseReplay(t *testing.T) {
	obsPath := os.Getenv("MONITOR_NOISE_REPLAY")
	if obsPath == "" {
		t.Skip("exported production observations not supplied")
	}
	ctx := context.Background()
	raw, err := os.ReadFile(os.Getenv("MONITOR_NOISE_CONFIG"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg domain.Config
	if err = json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	cfg.Shadow = false
	cfg.Actor, cfg.RequestID = "admin:replay", "replay"
	dbPath := filepath.Join(t.TempDir(), "replay.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.SaveConfig(ctx, cfg, 0); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(obsPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 64<<10), 8<<20)
	var rows []domain.Observation
	sources := map[string]bool{}
	for scan.Scan() {
		var o domain.Observation
		if err = json.Unmarshal(scan.Bytes(), &o); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, o)
		sources[o.Source] = true
	}
	if err = scan.Err(); err != nil || len(rows) == 0 {
		t.Fatal("empty or unreadable replay", err)
	}
	sort.SliceStable(rows, func(a, b int) bool { return rows[a].OccurredAt.Before(rows[b].OccurredAt) })
	step := 30 * time.Second
	clock := &fakeClock{rows[0].OccurredAt.Truncate(step).Add(step)}
	end := rows[len(rows)-1].OccurredAt.Add(2 * time.Hour)
	sender := &replaySender{clock: clock}
	eval := &Evaluator{Store: s, Manager: &incidents.Manager{Store: s}, Clock: clock}
	worker := notify.Worker{Store: s, Senders: map[string]notify.Sender{"feishu": sender}}
	// The 1 GiB state budget cannot hold a week of raw evidence; rules only read
	// the last 20 minutes, so a side connection prunes raw rows and history hourly.
	side, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer side.Close()
	if _, err = side.Exec("PRAGMA busy_timeout=10000"); err != nil {
		t.Fatal(err)
	}
	next := 0
	started := time.Now()
	for ; !clock.at.After(end); clock.at = clock.at.Add(step) {
		batch := map[string][]domain.Observation{}
		for next < len(rows) && !rows[next].OccurredAt.After(clock.at) {
			batch[rows[next].Source] = append(batch[rows[next].Source], rows[next])
			next++
		}
		// Every collector reports a successful poll each step, as in production.
		for src := range sources {
			if err = s.Ingest(ctx, batch[src], domain.Cursor{Source: src, LastSuccess: clock.at, Quality: domain.Fresh}); err != nil {
				t.Fatal(err)
			}
		}
		if clock.at.Minute() == 0 && clock.at.Second() == 0 {
			cut := clock.at.Add(-time.Hour).UnixMilli()
			for _, q := range []string{"DELETE FROM observations WHERE occurred<?", "DELETE FROM incident_transitions WHERE created<?"} {
				if _, err = side.Exec(q, cut); err != nil {
					t.Fatal(err)
				}
			}
		}
		if err = eval.Poll(ctx); err != nil {
			t.Fatalf("poll at %s: %v", clock.at, err)
		}
		for n := 0; n < 200; n++ {
			before := sender.n + sender.patches
			if err = worker.Tick(ctx, clock.at); err != nil {
				t.Fatal(err)
			}
			if sender.n+sender.patches == before && n > 20 {
				break
			}
		}
	}
	loc := time.FixedZone("CST", 8*3600)
	byDay, byRule, byHour := map[string]int{}, map[string]int{}, map[string]int{}
	for n, d := range sender.pushes {
		at := sender.pushAt[n].In(loc)
		byDay[at.Format("01-02")]++
		byHour[at.Format("01-02 15")]++
		key := d.Payload.Rule + ":" + d.Kind
		if d.Payload.Rollup {
			key = "rollup:" + key
		}
		byRule[key]++
	}
	maxHour, maxHourKey := 0, ""
	for k, v := range byHour {
		if v > maxHour {
			maxHour, maxHourKey = v, k
		}
	}
	report := map[string]any{"observations": len(rows), "from": rows[0].OccurredAt.In(loc), "to": rows[len(rows)-1].OccurredAt.In(loc), "pushes": sender.n, "patches": sender.patches, "by_day": byDay, "by_rule_kind": byRule, "max_hour": maxHour, "max_hour_at": maxHourKey, "elapsed_seconds": time.Since(started).Seconds()}
	out, _ := json.MarshalIndent(report, "", "  ")
	t.Log(string(out))
	if dest := os.Getenv("MONITOR_NOISE_REPORT"); dest != "" {
		if err = os.WriteFile(dest, out, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
