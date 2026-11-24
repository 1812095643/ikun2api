package notify

import (
	"context"
	"encoding/json"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFeishuTokenCacheCreateUpdateAndSafeFailure(t *testing.T) {
	auth, sends, updates := 0, 0, 0
	limited := false
	uuids := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "tenant_access_token"):
			auth++
			w.Write([]byte(`{"code":0,"tenant_access_token":"private-token","expire":7200}`))
		case r.Method == "PATCH":
			updates++
			w.Write([]byte(`{"code":0}`))
		default:
			sends++
			var b map[string]any
			json.NewDecoder(r.Body).Decode(&b)
			uuids = append(uuids, b["uuid"].(string))
			if r.Header.Get("Authorization") != "Bearer private-token" {
				t.Error("authentication absent")
			}
			if limited {
				w.Header().Set("Retry-After", "120")
				w.WriteHeader(429)
				w.Write([]byte(`{"code":99991400,"msg":"private-token"}`))
				return
			}
			w.Write([]byte(`{"code":0,"data":{"message_id":"om_test"}}`))
		}
	}))
	defer server.Close()
	f := NewFeishu("app", "secret")
	f.base = server.URL
	f.client = server.Client()
	d := domain.Delivery{ID: strings.Repeat("d", 64), Target: "oc_test", Payload: domain.Incident{ID: "incident", Severity: "P1", Status: "firing"}}
	id, err := f.Send(context.Background(), d, "", "")
	if err != nil || id != "om_test" {
		t.Fatal(id, err)
	}
	if _, err = f.Send(context.Background(), d, id, ""); err != nil {
		t.Fatal(err)
	}
	limited = true
	_, err = f.Send(context.Background(), d, "", "")
	fail, ok := err.(*Failure)
	if !ok || fail.Class != "rate_limited" || fail.RetryAfter != 2*time.Minute || strings.Contains(err.Error(), "private") {
		t.Fatal(err)
	}
	if auth != 1 || sends != 2 || updates != 1 || uuids[0] != uuids[1] || len(uuids[0]) > 50 {
		t.Fatal(auth, sends, updates, uuids)
	}
}
func TestCardHasEvidenceAndNoExecutionCallbacks(t *testing.T) {
	i := domain.Incident{ID: "id", Severity: "P1", Rule: "U01", Revision: 4, Scope: "account:1", Explanation: domain.Explanation{Conclusion: "上游429", Facts: []string{"已切换成功"}, Missing: []string{"原账号未验证"}, Advice: []string{"核对供应商限流"}}}
	raw, err := Card(i, "https://monitor.example")
	if err != nil {
		t.Fatal(err)
	}
	var card map[string]any
	if json.Unmarshal(raw, &card) != nil || card["schema"] != "2.0" {
		t.Fatal(string(raw))
	}
	if strings.Contains(string(raw), "callback") || !strings.Contains(string(raw), "原账号未验证") || len(raw) > 28000 {
		t.Fatal(string(raw))
	}
}

func TestFeishuReplyHasStableUUIDAndThreadParent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "tenant_access_token") {
			w.Write([]byte(`{"code":0,"tenant_access_token":"fixture","expire":7200}`))
			return
		}
		if r.Method != "POST" || r.URL.Path != "/open-apis/im/v1/messages/om_parent/reply" {
			t.Error("wrong reply endpoint")
		}
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		if b["reply_in_thread"] != true || b["uuid"] != "stable-reply" || b["msg_type"] != "interactive" {
			t.Error("reply contract", b)
		}
		w.Write([]byte(`{"code":0,"data":{"message_id":"om_reply"}}`))
	}))
	defer server.Close()
	f := NewFeishu("fixture", "fixture")
	f.base = server.URL
	f.client = server.Client()
	id, err := f.Reply(context.Background(), domain.Delivery{ID: "stable-reply", Payload: domain.Incident{ID: "incident", Rule: "U01"}}, "om_parent", "")
	if err != nil || id != "om_reply" {
		t.Fatal(id, err)
	}
}

func TestDetailLinkUsesIndependentMonitorPage(t *testing.T) {
	got := detailLink("https://example.test", "inc-test")
	if !strings.Contains(got, "/ops-monitor-ui/") || !strings.Contains(got, "monitor_incident=inc-test") {
		t.Fatal(got)
	}
}
