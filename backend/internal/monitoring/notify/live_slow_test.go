package notify

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/incidents"
	"github.com/MACOS-DO/sub4api/internal/monitoring/store"
	_ "github.com/lib/pq"
)

// One explicitly opted-in synthetic slow request, sent only to the private test recipient.
func TestLiveSlowNotification(t *testing.T) {
	dir := os.Getenv("OPS_SLOW_LIVE_DIR")
	if dir == "" {
		t.Skip("not enabled")
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		t.Fatal("private test directory required")
	}
	raw, err := os.ReadFile("/etc/ikun-ops-monitor/monitor.env")
	if err != nil {
		t.Fatal("config unavailable")
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[k], err = strconv.Unquote(v)
		if err != nil {
			t.Fatal("invalid config")
		}
	}
	target, err := os.ReadFile(filepath.Join(dir, "recipient.txt"))
	if err != nil {
		t.Fatal("missing recipient")
	}
	recipient := strings.TrimSpace(string(target))
	marker, err := os.OpenFile(filepath.Join(dir, "attempted"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal("existing attempt: inspect before retry")
	}
	marker.Close()
	db, err := sql.Open("postgres", values["OPS_POSTGRES_DSN"])
	if err != nil {
		t.Fatal("database unavailable")
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var kid, aid, gid int64
	for _, q := range []struct {
		query string
		dest  *int64
	}{{"SELECT id FROM ops_monitor_v1_identity WHERE kind='key' AND owner_id=3 ORDER BY id LIMIT 1", &kid}, {"SELECT id FROM ops_monitor_v1_identity WHERE kind='account' ORDER BY id LIMIT 1", &aid}, {"SELECT id FROM ops_monitor_v1_identity WHERE kind='group' ORDER BY id LIMIT 1", &gid}} {
		if err = db.QueryRowContext(ctx, q.query).Scan(q.dest); err != nil {
			t.Fatal("test identities unavailable")
		}
	}
	s, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cfg := domain.DefaultConfig()
	cfg.Site = "test"
	cfg.Shadow = false
	cfg.Actor = "admin:1"
	cfg.RequestID = "slow-test-config"
	cfg.FeishuTargets = []string{recipient}
	cfg.EmailTargets = []string{"not-sent@example.test"}
	cfg.Rules["L02"] = domain.RuleConfig{Enabled: true, Owner: "monitor", Version: 1}
	if err = s.SaveConfig(ctx, cfg, 0); err != nil {
		t.Fatal(err)
	}
	cfg, err = s.Config(ctx)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	f := domain.Finding{Rule: "L02", RuleVersion: cfg.Rule("L02").Version, Fingerprint: "slow-readable-test", Scope: "synthetic-request", Severity: "P2", At: now, EvidenceID: "one", Quality: domain.Fresh, UserID: 3, KeyID: kid, AccountID: aid, GroupID: gid, Explanation: domain.Explanation{Conclusion: "合成联调：首字等待告警（非真实故障）", Facts: []string{"仅验证飞书通知、身份展示和原卡更新。"}, Advice: []string{"无需处理业务。"}}}
	manager := &incidents.Manager{Store: s}
	if err = manager.Apply(ctx, f, cfg, now); err != nil {
		t.Fatal(err)
	}
	mail := &stubSender{}
	sender := ReadableSender{DB: db, Next: NewFeishu(values["OPS_FEISHU_APP_ID"], values["OPS_FEISHU_APP_SECRET"])}
	w := Worker{Store: s, Senders: map[string]Sender{"feishu": sender, "email": mail}}
	if err = w.Tick(ctx, now); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Incidents(ctx)
	if err != nil || len(rows) != 1 {
		t.Fatal("incident missing")
	}
	first := s.MessageID(ctx, rows[0].ID, "feishu", recipient)
	if first == "" {
		t.Fatal("first card not accepted")
	}
	f.At = now.Add(time.Second)
	f.EvidenceID = "two"
	f.Severity = "P1"
	f.Explanation.Conclusion = "合成联调：慢首字卡片已更新（非真实故障）"
	if err = manager.Apply(ctx, f, cfg, f.At); err != nil {
		t.Fatal(err)
	}
	if err = w.Tick(ctx, f.At); err != nil {
		t.Fatal(err)
	}
	second := s.MessageID(ctx, rows[0].ID, "feishu", recipient)
	deliveries, err := s.Deliveries(ctx)
	if err != nil || second != first || len(deliveries) != 2 || mail.calls != 0 {
		t.Fatal("delivery routing or card update failed")
	}
	for _, d := range deliveries {
		if d.Channel != "feishu" || d.State != "accepted" {
			t.Fatal("unexpected delivery")
		}
	}
	if err = os.WriteFile(filepath.Join(dir, "created"), []byte(first), 0600); err != nil {
		t.Fatal(err)
	}
	t.Log("synthetic slow alert created and updated same card; no email created")
}

func TestLiveRequestTimingAndAccountUpdate(t *testing.T) {
	dir := os.Getenv("OPS_TIMING_LIVE_DIR")
	if dir == "" {
		t.Skip("not enabled")
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		t.Fatal("private test directory required")
	}
	raw, err := os.ReadFile("/etc/ikun-ops-monitor/monitor.env")
	if err != nil {
		t.Fatal("config unavailable")
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[k], err = strconv.Unquote(v)
		if err != nil {
			t.Fatal("invalid config")
		}
	}
	target, err := os.ReadFile(filepath.Join(dir, "recipient.txt"))
	if err != nil {
		t.Fatal("missing recipient")
	}
	recipient := strings.TrimSpace(string(target))
	marker, err := os.OpenFile(filepath.Join(dir, "attempted"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal("existing attempt: inspect before retry")
	}
	marker.Close()
	db, err := sql.Open("postgres", values["OPS_POSTGRES_DSN"])
	if err != nil {
		t.Fatal("database unavailable")
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	var kid, aid, gid int64
	for _, q := range []struct {
		query string
		dest  *int64
	}{{"SELECT id FROM ops_monitor_v1_identity WHERE kind='key' AND owner_id=3 ORDER BY id LIMIT 1", &kid}, {"SELECT id FROM ops_monitor_v1_identity WHERE kind='account' ORDER BY id LIMIT 1", &aid}, {"SELECT id FROM ops_monitor_v1_identity WHERE kind='group' ORDER BY id LIMIT 1", &gid}} {
		if err = db.QueryRowContext(ctx, q.query).Scan(q.dest); err != nil {
			t.Fatal("test identity unavailable")
		}
	}
	s, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cfg := domain.DefaultConfig()
	cfg.Site = "test"
	cfg.Shadow = false
	cfg.Actor = "admin:1"
	cfg.RequestID = "timing-test-config"
	cfg.FeishuTargets = []string{recipient}
	cfg.EmailTargets = []string{"not-sent@example.test"}
	cfg.Rules["L02"] = domain.RuleConfig{Enabled: true, Owner: "monitor", Version: 1}
	if err = s.SaveConfig(ctx, cfg, 0); err != nil {
		t.Fatal(err)
	}
	cfg, err = s.Config(ctx)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	f := domain.Finding{Rule: "L02", RuleVersion: cfg.Rule("L02").Version, Fingerprint: "timing-readable-test", Scope: "test:synthetic-request", RequestID: "synthetic-request", RequestStartedAt: now.Add(-31 * time.Second), Severity: "P2", At: now, EvidenceID: "one", Quality: domain.Fresh, UserID: 3, KeyID: kid, GroupID: gid, Explanation: domain.Explanation{Conclusion: "合成测试：请求等待与账号更新（非真实故障）", Facts: []string{"验证请求开始时间、后续选号与结束。"}}}
	manager := &incidents.Manager{Store: s}
	if err = manager.Apply(ctx, f, cfg, now); err != nil {
		t.Fatal(err)
	}
	mail := &stubSender{}
	sender := ReadableSender{DB: db, Next: NewFeishu(values["OPS_FEISHU_APP_ID"], values["OPS_FEISHU_APP_SECRET"])}
	w := Worker{Store: s, Senders: map[string]Sender{"feishu": sender, "email": mail}}
	if err = w.Tick(ctx, now); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Incidents(ctx)
	if err != nil || len(rows) != 1 {
		t.Fatal("incident missing")
	}
	id := s.MessageID(ctx, rows[0].ID, "feishu", recipient)
	if id == "" {
		t.Fatal("first card not accepted")
	}
	f.At = now.Add(5 * time.Second)
	f.EvidenceID = "selected"
	f.AccountID = aid
	if err = manager.Apply(ctx, f, cfg, f.At); err != nil {
		t.Fatal(err)
	}
	if err = w.Tick(ctx, f.At); err != nil {
		t.Fatal(err)
	}
	if s.MessageID(ctx, rows[0].ID, "feishu", recipient) != id {
		t.Fatal("account update did not retain card")
	}
	f.At = now.Add(10 * time.Second)
	f.EvidenceID = "finished"
	f.Healthy = true
	f.FirstOutputAt = now.Add(8 * time.Second)
	f.RequestEndedAt = f.At
	f.RequestOutcome = "success"
	f.Explanation.Facts = []string{"合成测试：该请求已成功结束。"}
	if err = manager.Apply(ctx, f, cfg, f.At); err != nil {
		t.Fatal(err)
	}
	if err = w.Tick(ctx, f.At); err != nil {
		t.Fatal(err)
	}
	ds, err := s.Deliveries(ctx)
	if err != nil || len(ds) != 3 || mail.calls != 0 || s.MessageID(ctx, rows[0].ID, "feishu", recipient) != id {
		t.Fatal("delivery count, channel or same card mismatch")
	}
	for _, d := range ds {
		if d.Channel != "feishu" || d.State != "accepted" {
			t.Fatal("unexpected delivery")
		}
	}
	if err = os.WriteFile(filepath.Join(dir, "created"), []byte(id), 0600); err != nil {
		t.Fatal(err)
	}
	t.Log("synthetic card created, account updated, terminal updated; no email")
}
