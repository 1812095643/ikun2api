package runtime

import (
	"context"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/incidents"
	"github.com/MACOS-DO/sub4api/internal/monitoring/notify"
	"github.com/MACOS-DO/sub4api/internal/monitoring/sources"
	"github.com/MACOS-DO/sub4api/internal/monitoring/store"
	"github.com/MACOS-DO/sub4api/internal/monitoring/telemetry"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type pipelineSender struct{ sent []domain.Delivery }

func (s *pipelineSender) Send(_ context.Context, d domain.Delivery, _, _ string) (string, error) {
	s.sent = append(s.sent, d)
	return "om_fixture", nil
}
func TestHTTPAttemptThroughCollectorRulesOutboxAndSender(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	h := telemetry.New("ikun2api", "fixture", "test", 16, 128)
	r := gin.New()
	token := strings.Repeat("x", 32)
	r.GET("/snapshot", h.Handler(token))
	r.POST("/chat", h.Middleware(), func(c *gin.Context) {
		tr := telemetry.FromContext(c.Request.Context())
		user, _ := strconv.ParseInt(c.GetHeader("X-User"), 10, 64)
		tr.Update(domain.Observation{UserID: user, GroupID: 2})
		tr.Attempt(1, "fixture", time.Now())
		tr.Event("attempt.failed", "", 429, 1, time.Now())
		tr.Attempt(2, "fixture", time.Now())
		c.Header("Content-Type", "text/event-stream")
		c.Writer.WriteString("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n")
	})
	server := httptest.NewServer(r)
	defer server.Close()
	// Two affected users pass the U01 open-card threshold.
	for _, user := range []string{"1", "2"} {
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/chat", strings.NewReader("{}"))
		req.Header.Set("X-User", user)
		res, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		res.Body.Close()
	}
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	cfg := domain.DefaultConfig()
	cfg.Actor = "admin:1"
	cfg.RequestID = "test"
	cfg.Shadow = false
	cfg.FeishuTargets = []string{"oc_fixture"}
	cfg.Rules["U01"] = domain.RuleConfig{Enabled: true, Owner: "monitor", Version: 1}
	if e = s.SaveConfig(ctx, cfg, 0); e != nil {
		t.Fatal(e)
	}
	clock := &fakeClock{time.Now()}
	c := &Collector{Store: s, Clock: clock, Site: "ikun2api", Source: &sources.Gateway{URL: server.URL + "/snapshot", Token: token}}
	if e = c.Poll(ctx); e != nil {
		t.Fatal(e)
	}
	eval := &Evaluator{Store: s, Manager: &incidents.Manager{Store: s}, Clock: clock}
	if e = eval.Poll(ctx); e != nil {
		t.Fatal(e)
	}
	sender := &pipelineSender{}
	worker := notify.Worker{Store: s, Senders: map[string]notify.Sender{"feishu": sender}}
	if e = worker.Tick(ctx, clock.Now()); e != nil {
		t.Fatal(e)
	}
	if len(sender.sent) != 1 || sender.sent[0].Payload.Rule != "U01" || sender.sent[0].Payload.Severity != "P2" || sender.sent[0].Payload.Status == "resolved" {
		t.Fatal("upstream failure lost or original account prematurely recovered", sender.sent)
	}
	if e = c.Poll(ctx); e != nil {
		t.Fatal(e)
	}
	if e = eval.Poll(ctx); e != nil {
		t.Fatal(e)
	}
	if e = worker.Tick(ctx, clock.Now()); e != nil {
		t.Fatal(e)
	}
	if len(sender.sent) != 1 {
		t.Fatal("duplicate replay notified twice")
	}
}
