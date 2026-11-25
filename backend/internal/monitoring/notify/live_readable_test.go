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
	_ "github.com/lib/pq"
)

// An opt-in one-shot synthetic card using real, read-only identity data.
func TestLiveReadableIdentity(t *testing.T) {
	dir := os.Getenv("OPS_READABLE_LIVE_DIR")
	if dir == "" {
		t.Skip("not enabled")
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		t.Fatal("private test directory required")
	}
	raw, err := os.ReadFile("/etc/ikun-ops-monitor/monitor.env")
	if err != nil {
		t.Fatal("service config unavailable")
	}
	cfg := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		cfg[k], err = strconv.Unquote(v)
		if err != nil {
			t.Fatal("invalid config")
		}
	}
	target, err := os.ReadFile(filepath.Join(dir, "recipient.txt"))
	if err != nil {
		t.Fatal("recipient unavailable")
	}
	db, err := sql.Open("postgres", cfg["OPS_POSTGRES_DSN"])
	if err != nil {
		t.Fatal("database open failed")
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var kid int64
	if err = db.QueryRowContext(ctx, "SELECT id FROM ops_monitor_v1_identity WHERE kind='key' AND owner_id=$1 ORDER BY id LIMIT 1", 3).Scan(&kid); err != nil {
		t.Fatal("test identity unavailable")
	}
	marker, err := os.OpenFile(filepath.Join(dir, "attempted"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal("existing attempt: do not resend automatically")
	}
	marker.Close()
	now := time.Now()
	i := domain.Incident{ID: "readability-live-test", Rule: "Q02", RuleVersion: 1, Revision: 1, Severity: "P3", Status: "firing", Scope: "user:3/key:" + strconv.FormatInt(kid, 10), FirstSeen: now, LastSeen: now, Count: 1, Explanation: domain.Explanation{Conclusion: "可读性联调测试：不是实际故障", Facts: []string{"仅验证用户、邮箱和 Key 名称展示。"}, Advice: []string{"无需处理。"}}}
	d := domain.Delivery{ID: "readability-live-test-v1", Target: strings.TrimSpace(string(target)), Payload: i}
	var email, keyName string
	if err = db.QueryRowContext(ctx, "SELECT email FROM ops_monitor_v1_identity WHERE kind='user' AND id=$1", 3).Scan(&email); err != nil || email == "" {
		t.Fatal("user email unavailable")
	}
	if err = db.QueryRowContext(ctx, "SELECT name FROM ops_monitor_v1_identity WHERE kind='key' AND id=$1 AND owner_id=$2", kid, 3).Scan(&keyName); err != nil || keyName == "" {
		t.Fatal("key display name unavailable")
	}
	probe := &recordingSender{}
	if _, err = (ReadableSender{DB: db, Next: probe}).Send(ctx, d, "", ""); err != nil || !strings.Contains(probe.delivery.Payload.DisplayScope, email) || !strings.Contains(probe.delivery.Payload.DisplayScope, keyName) {
		t.Fatal("identity rendering check failed")
	}
	s := ReadableSender{DB: db, Next: NewFeishu(cfg["OPS_FEISHU_APP_ID"], cfg["OPS_FEISHU_APP_SECRET"])}
	id, err := s.Send(ctx, d, "", "")
	if err != nil || id == "" {
		t.Fatal("create failed; inspect platform before retry")
	}
	if err = os.WriteFile(filepath.Join(dir, "created"), []byte(id), 0600); err != nil {
		t.Fatal("cannot record accepted message")
	}
	d.ID = "readability-live-test-v2"
	d.Payload.Revision = 2
	d.Payload.Explanation.Conclusion = "可读性联调测试：原卡已更新"
	updated, err := s.Send(ctx, d, id, "")
	if err != nil || updated != id {
		t.Fatal("update failed; inspect created card")
	}
	if err = os.WriteFile(filepath.Join(dir, "updated"), []byte("accepted\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Log("synthetic create and update accepted; same card")
}
