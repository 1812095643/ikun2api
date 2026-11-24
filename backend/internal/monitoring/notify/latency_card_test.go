package notify

import (
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/detectors"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"strings"
	"testing"
	"time"
)

func TestLatencyCardRetainsContextAndReasoningEvidence(t *testing.T) {
	now := time.Now()
	var rows []domain.Observation
	for n := 0; n < 6; n++ {
		o := domain.Observation{Schema: 1, Site: "test", Source: "gateway", RequestID: fmt.Sprint(n), EventID: fmt.Sprint(n), AccountID: 31, GroupID: 27, Model: "claude-opus-5-5", Kind: "request.finished", Quality: domain.Fresh, Outcome: "success", OccurredAt: now.Add(-time.Duration(n) * time.Minute), Metrics: map[string]float64{"has_output": 1, "first_effective_ms": 52000, "pre_forward_ms": 2000, "reasoning_started": 1, "reasoning_started_ms": 51000}}
		u := o
		u.Source = "postgres.usage"
		u.Kind = "usage.recorded"
		u.RequestID = "client:" + o.RequestID
		u.Metrics = map[string]float64{"context_input_tokens": 1, "context_cache_read_tokens": 749000, "context_cache_write_tokens": 999, "client_first_token_ms": 15000, "first_event_semantics": 1, "request_stream": 1, "reasoning_configured": 1}
		rows = append(rows, o, u)
	}
	for _, f := range detectors.Evaluate(rows, now, domain.DefaultConfig()) {
		if f.Rule != "L03" || f.Healthy {
			continue
		}
		b, e := Card(domain.Incident{Rule: "L03", Severity: "P2", Explanation: f.Explanation}, "")
		if e != nil {
			t.Fatal(e)
		}
		for _, term := range []string{"75.0万token", "协议首响应", "已观测思考标记", "配置档位不代表实际思考时长", "不能将全部等待归因"} {
			if !strings.Contains(string(b), term) {
				t.Fatal("missing card evidence", term)
			}
		}
		return
	}
	t.Fatal("no L03 finding")
}

func TestDigestCardIsLabelledNotRecovered(t *testing.T) {
	b, err := Card(domain.Incident{ID: "d", Rule: "S01", Scope: "digest", Status: "resolved", Severity: "P3", Explanation: domain.Explanation{Conclusion: "每日摘要", Facts: []string{"分组 #23 · 模型 x · 被请求 3 次"}}}, "")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "每日摘要") || strings.Contains(s, "已恢复") || !strings.Contains(s, `"template":"orange"`) {
		t.Fatal(s)
	}
}
