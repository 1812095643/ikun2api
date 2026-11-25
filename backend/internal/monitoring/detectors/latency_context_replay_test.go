package detectors

import (
	"encoding/json"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestRealSlowRequestAttribution(t *testing.T) {
	path := os.Getenv("MONITOR_CONTEXT_REPLAY")
	if path == "" {
		t.Skip("opt-in real usage correlation")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	var data []struct {
		Request string             `json:"request"`
		Usage   map[string]string  `json:"usage"`
		Gateway map[string]float64 `json:"gateway"`
		Outcome string             `json:"outcome"`
	}
	if e = json.Unmarshal(raw, &data); e != nil {
		t.Fatal(e)
	}
	groups := map[int64][]domain.Observation{}
	ev := evaluation{}
	for _, r := range data {
		a, _ := strconv.ParseInt(r.Usage["account_id"], 10, 64)
		o := domain.Observation{Site: "ikun2api", RequestID: r.Request, AccountID: a, GroupID: 27, Model: "claude-opus-5-5", Kind: "request.finished", Outcome: r.Outcome, Quality: domain.Fresh, Metrics: copyMetrics(r.Gateway)}
		o.Metrics["sample_first_ms"] = o.N("first_effective_ms") - o.N("pre_forward_ms")
		if r.Usage["stream"] == "t" {
			groups[a] = append(groups[a], o)
		}
		u := o
		u.Kind = "usage.recorded"
		u.EventID = r.Usage["id"]
		u.RequestID = r.Usage["request_id"]
		u.Metrics = map[string]float64{}
		for k, v := range map[string]string{"context_input_tokens": "input_tokens", "context_cache_read_tokens": "cache_read_tokens", "context_cache_write_tokens": "cache_creation_tokens", "client_first_token_ms": "client_first_token_ms"} {
			if n, err := strconv.ParseFloat(r.Usage[v], 64); err == nil {
				u.Metrics[k] = n
			}
		}
		if r.Usage["reasoning_effort"] != "" {
			u.Metrics["reasoning_configured"] = 1
		}
		u.Metrics["first_event_semantics"] = 1
		ev.rows = append(ev.rows, u)
	}
	for a, rows := range groups {
		f := domain.Finding{}
		ev.explainLatency(&f, rows, 30000)
		t.Logf("account=%d facts=%v inference=%v", a, f.Explanation.Facts, f.Explanation.Inferences)
		if strings.Contains(strings.Join(f.Explanation.Facts, ""), "精确关联用量0个") {
			t.Fatal("usage correlation failed")
		}
	}
	if len(data) != 28 || len(groups[31]) != 5 || len(groups[34]) != 22 {
		t.Fatal("fixture scope changed")
	}
}
