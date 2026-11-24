package detectors

import (
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"strings"
	"testing"
	"time"
)

func TestUploadDominatedLatencyIsReportedWithoutBlamingChannel(t *testing.T) {
	now := time.Now()
	rows := []domain.Observation{}
	for n := 0; n < 3; n++ {
		o := observation("usage.recorded", now.Add(time.Duration(n)*time.Second))
		o.Source = "postgres.usage"
		o.EventID = fmt.Sprint(n)
		o.RequestID = fmt.Sprint(n)
		o.Metrics = map[string]float64{"client_first_token_ms": 175713, "upload_ms": 160087, "pre_forward_ms": 160909, "first_token_ms": 14804}
		rows = append(rows, o)
	}
	fs := Evaluate(rows, now.Add(time.Minute), domain.DefaultConfig())
	if !hasRule(fs, "L02") {
		t.Fatal("missing user experience warning")
	}
	if hasRule(fs, "L03") {
		t.Fatal("upload delay incorrectly treated as channel slowness")
	}
	found := false
	for _, f := range fs {
		if f.Rule == "L02" && !f.Healthy && strings.Contains(strings.Join(f.Explanation.Facts, " "), "160.1秒") {
			found = true
		}
	}
	if !found {
		t.Fatal("upload duration missing from explanation")
	}
}
