package detectors

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"io"
	"os"
	"testing"
	"time"
)

func TestAuditReal48Hours(t *testing.T) {
	path := os.Getenv("MONITOR_AUDIT_SNAPSHOT")
	if path == "" {
		t.Skip("opt-in real audit")
	}
	f, e := os.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	z, e := gzip.NewReader(f)
	if e != nil {
		t.Fatal(e)
	}
	defer z.Close()
	dec := json.NewDecoder(z)
	var rows []domain.Observation
	cfg := domain.DefaultConfig()
	for {
		var x struct {
			Table string          `json:"table"`
			Data  json.RawMessage `json:"data"`
		}
		e = dec.Decode(&x)
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		if x.Table == "observations" {
			var o domain.Observation
			if e = json.Unmarshal(x.Data, &o); e != nil {
				t.Fatal(e)
			}
			rows = append(rows, o)
		}
		if x.Table == "config" {
			if e = json.Unmarshal(x.Data, &cfg); e != nil {
				t.Fatal(e)
			}
		}
	}
	misses := map[string]int{}
	errors, excluded := 0, 0
	for _, o := range rows {
		if o.Kind != "request.error" {
			continue
		}
		errors++
		if o.Reason == "key_daily_limit" {
			excluded++
			continue
		}
		fs := Evaluate([]domain.Observation{o}, o.OccurredAt, cfg)
		if len(fs) == 0 {
			misses[o.Reason]++
		}
	}
	scopes := map[string]map[string]bool{"L03": {}, "L04": {}}
	windows := map[string]int{}
	start, end := 0, 0
	for now := rows[0].OccurredAt; !now.After(rows[len(rows)-1].OccurredAt); now = now.Add(time.Minute) {
		for end < len(rows) && !rows[end].OccurredAt.After(now) {
			end++
		}
		for start < end && rows[start].OccurredAt.Before(now.Add(-20*time.Minute)) {
			start++
		}
		for _, f := range Evaluate(rows[start:end], now, cfg) {
			if !f.Healthy && (f.Rule == "L03" || f.Rule == "L04") {
				scopes[f.Rule][f.Scope] = true
				windows[f.Rule]++
			}
		}
	}
	t.Logf("observations=%d errors=%d daily_excluded=%d missing=%v aggregated_scopes=%v windows=%v", len(rows), errors, excluded, misses, scopes, windows)
	if len(misses) > 0 {
		t.Fatal(fmt.Sprint(misses))
	}
}
