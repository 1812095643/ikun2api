package notify

import (
	"context"
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/detectors"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/incidents"
	"github.com/MACOS-DO/sub4api/internal/monitoring/store"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSignaturePipelineCreatesOnceUpdatesOriginalAndRestartDedups(t *testing.T) {
	creates, patches := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "tenant_access_token"):
			fmt.Fprint(w, `{"code":0,"tenant_access_token":"fixture","expire":7200}`)
		case r.Method == "PATCH":
			patches++
			if !strings.HasSuffix(r.URL.Path, "/om_signature") {
				t.Error("update targeted another message")
			}
			fmt.Fprint(w, `{"code":0}`)
		default:
			creates++
			fmt.Fprint(w, `{"code":0,"data":{"message_id":"om_signature"}}`)
		}
	}))
	defer server.Close()
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	cfg := domain.DefaultConfig()
	cfg.Shadow = false
	cfg.Actor = "admin:1"
	cfg.RequestID = "test"
	cfg.FeishuTargets = []string{"ou_test"}
	cfg.Rules["F02"] = domain.RuleConfig{Enabled: true, Owner: "monitor", Version: 1}
	if err = s.SaveConfig(ctx, cfg, 0); err != nil {
		t.Fatal(err)
	}
	cfg, err = s.Config(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sender := NewFeishu("fixture", "fixture")
	sender.base = server.URL
	sender.client = server.Client()
	worker := &Worker{Store: s, Senders: map[string]Sender{"feishu": sender}}
	manager := &incidents.Manager{Store: s}
	start := time.Now()
	// The card opens at the 5th request and gets its hourly update an hour later.
	for n := 0; n <= 65; n++ {
		now := start.Add(time.Duration(n) * time.Minute)
		o := domain.Observation{Schema: 1, Site: "test", Source: "gateway", Kind: "signature.rejected", EventID: fmt.Sprint(n), RequestID: fmt.Sprint(n), TaskID: "session:hash", Reason: "thinking_signature_invalid", UserID: 1, KeyID: 1, AccountID: int64(1 + n%2), OccurredAt: now, ObservedAt: now, Quality: domain.Fresh, Metrics: map[string]float64{"signature_session_known": 1, "signature_forward_unchanged": 1}}
		findings := detectors.Evaluate([]domain.Observation{o}, now, cfg)
		if len(findings) != 1 {
			t.Fatal(findings)
		}
		if err = manager.Apply(ctx, findings[0], cfg, now); err != nil {
			t.Fatal(err)
		}
		if err = worker.Tick(ctx, now); err != nil {
			t.Fatal(err)
		}
		manager = &incidents.Manager{Store: s}
		if err = manager.Apply(ctx, findings[0], cfg, now); err != nil {
			t.Fatal(err)
		}
		if err = worker.Tick(ctx, now); err != nil {
			t.Fatal(err)
		}
	}
	if creates != 1 || patches != 1 {
		t.Fatal(creates, patches)
	}
	ds, _ := s.Deliveries(ctx)
	for _, d := range ds {
		if d.State != "accepted" || d.Channel != "feishu" {
			t.Fatal(d)
		}
	}
}
