package notify

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/detectors"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/incidents"
	"github.com/MACOS-DO/sub4api/internal/monitoring/store"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLiveRepeatedStalls(t *testing.T) {
	dir := os.Getenv("OPS_STALL_LIVE_DIR")
	if dir == "" {
		t.Skip("explicit private live test directory required")
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		t.Fatal("private directory required")
	}
	b, err := os.ReadFile("/etc/ikun-ops-monitor/monitor.env")
	if err != nil {
		t.Fatal("config unavailable")
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok {
			if json.Unmarshal([]byte(v), new(string)) != nil {
				t.Fatal("invalid config")
			}
			var value string
			json.Unmarshal([]byte(v), &value)
			values[k] = value
		}
	}
	target, err := os.ReadFile(filepath.Join(dir, "recipient.txt"))
	if err != nil {
		t.Fatal("recipient unavailable")
	}
	marker, err := os.OpenFile(filepath.Join(dir, "attempted"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal("existing attempt; inspect before retry")
	}
	marker.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	db, err := sql.Open("postgres", values["OPS_POSTGRES_DSN"])
	if err != nil {
		t.Fatal("database unavailable")
	}
	defer db.Close()
	s, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cfg := domain.DefaultConfig()
	cfg.Shadow = false
	cfg.Actor = "admin:1"
	cfg.RequestID = "synthetic-stall-test"
	cfg.FeishuTargets = []string{strings.TrimSpace(string(target))}
	cfg.EmailTargets = []string{"ignored@example.test"}
	cfg.Rules["L04"] = domain.RuleConfig{Enabled: true, Owner: "monitor", Version: 1}
	if err = s.SaveConfig(ctx, cfg, 0); err != nil {
		t.Fatal(err)
	}
	cfg, err = s.Config(ctx)
	if err != nil {
		t.Fatal(err)
	}
	m := &incidents.Manager{Store: s}
	now := time.Now()
	var rows []domain.Observation
	for n := 0; n < 4; n++ {
		rows = append(rows, domain.Observation{Schema: 1, Site: cfg.Site, Source: "gateway", Kind: "request.active", EventID: fmt.Sprint(n), RequestID: fmt.Sprint("synthetic-stall-", n), UserID: 1, KeyID: 106, AccountID: 31, GroupID: 23, Model: "synthetic-monitor", OccurredAt: now.Add(time.Duration(n) * time.Millisecond), ObservedAt: now, Quality: domain.Fresh, Metrics: map[string]float64{"stall_evidence_v1": 1, "has_output": 1, "idle_ms": 45000}})
	}
	apply := func(rs []domain.Observation, at time.Time) {
		for _, f := range detectors.Evaluate(rs, at, cfg) {
			if f.Rule != "L04" {
				continue
			}
			f.Explanation.Conclusion = "合成验证：多个请求疑似输出停顿（非真实故障）"
			if err = m.Apply(ctx, f, cfg, at); err != nil {
				t.Fatal(err)
			}
		}
	}
	apply(rows[:1], now.Add(time.Second))
	ds, _ := s.Deliveries(ctx)
	if len(ds) != 0 {
		t.Fatal("single request notified")
	}
	apply(rows[:3], now.Add(time.Second))
	ds, _ = s.Deliveries(ctx)
	if len(ds) != 1 || ds[0].Payload.RequestCount != 3 {
		t.Fatal("aggregate first card incorrect")
	}
	mail := &stubSender{}
	sender := NewFeishu(values["OPS_FEISHU_APP_ID"], values["OPS_FEISHU_APP_SECRET"])
	w := Worker{Store: s, Senders: map[string]Sender{"feishu": ReadableSender{DB: db, Next: sender}, "email": mail}}
	if err = w.Tick(ctx, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	is, _ := s.Incidents(ctx)
	id := s.MessageID(ctx, is[0].ID, "feishu", cfg.FeishuTargets[0])
	if id == "" {
		t.Fatal("not accepted")
	}
	apply(rows[:3], now.Add(2*time.Second))
	ds, _ = s.Deliveries(ctx)
	if len(ds) != 1 {
		t.Fatal("poll replay notified again")
	}
	rows[3].OccurredAt = now.Add(6 * time.Minute)
	apply(rows, now.Add(6*time.Minute))
	if err = w.Tick(ctx, now.Add(6*time.Minute)); err != nil {
		t.Fatal(err)
	}
	ds, _ = s.Deliveries(ctx)
	if len(ds) != 2 || mail.calls != 0 || s.MessageID(ctx, is[0].ID, "feishu", cfg.FeishuTargets[0]) != id {
		t.Fatal("update or routing mismatch")
	}
	for _, d := range ds {
		if d.Channel != "feishu" || d.State != "accepted" {
			t.Fatal("delivery not accepted")
		}
	}
	if err = os.WriteFile(filepath.Join(dir, "created"), []byte(id), 0600); err != nil {
		t.Fatal(err)
	}
	t.Log("single stall suppressed; three requests one real Feishu card; same-card update to four; replay deduplicated; no email")
}
