package notify

import (
	"encoding/json"
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"strings"
	"testing"
	"time"
)

func TestSlowCardExplainsRequestTimingAndResolvedScope(t *testing.T) {
	now := time.Date(2026, 11, 14, 4, 45, 22, 0, time.UTC)
	i := domain.Incident{Rule: "L02", Scope: "site:req-1", Status: "resolved", Severity: "P1", FirstSeen: now.Add(31 * time.Second), LastSeen: now.Add(155 * time.Second), Sample: &domain.IncidentSample{RequestID: "req-1", RequestStartedAt: now, FirstOutputAt: now.Add(145 * time.Second), RequestEndedAt: now.Add(155 * time.Second), RequestOutcome: "success"}}
	raw, err := Card(i, "")
	if err != nil {
		t.Fatal(err)
	}
	var card any
	if err = json.Unmarshal(raw, &card); err != nil {
		t.Fatal(err)
	}
	txt := fmt.Sprint(card)
	for _, want := range []string{"该请求等待已结束", "调用开始", "首次有效输出", "请求结束", "告警触发", "12:45:22", "12:47:47", "12:47:57"} {
		if !strings.Contains(txt, want) {
			t.Fatalf("missing %s", want)
		}
	}
}

func TestSlowCardTerminalOutcomeLabels(t *testing.T) {
	for _, tc := range []struct{ outcome, want string }{{"success", "成功完成"}, {"failed", "失败"}, {"cancelled", "已取消"}, {"unknown", "未知"}} {
		t.Run(tc.outcome, func(t *testing.T) {
			now := time.Now()
			i := domain.Incident{Rule: "L02", Scope: "site:r", Status: "resolved", Sample: &domain.IncidentSample{RequestID: "r", RequestEndedAt: now, RequestOutcome: tc.outcome}}
			s := timingDetails(i)
			if !strings.Contains(s, tc.want) || incidentStatusText(i) != "该请求等待已结束" {
				t.Fatal(s)
			}
		})
	}
}

func TestSlowAggregateResolutionDoesNotClaimChannelRecovered(t *testing.T) {
	i := domain.Incident{Rule: "L02", Scope: "account:31/group:23/model:gpt", Status: "resolved", Sample: &domain.IncidentSample{RequestID: "other"}}
	if got := incidentStatusText(i); got != "该范围出现较快请求，仍需观察" {
		t.Fatal(got)
	}
}
