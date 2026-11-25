package notify

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
)

// Explicit opt-in to one synthetic test email. The exclusive marker prevents
// automatic resend when a previous attempt has an ambiguous network outcome.
func TestLiveSMTPDelivery(t *testing.T) {
	dir := os.Getenv("OPS_SMTP_LIVE_DIR")
	if dir == "" {
		t.Skip("live SMTP is not enabled")
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		t.Fatal("private test directory required")
	}
	raw, err := os.ReadFile("/etc/ikun-ops-monitor/monitor.env")
	if err != nil {
		t.Fatal("cannot read service configuration")
	}
	cfg := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v, err = strconv.Unquote(v)
		if err != nil {
			t.Fatal("invalid service configuration")
		}
		cfg[k] = v
	}
	recipient, err := os.ReadFile(filepath.Join(dir, "email-recipient.txt"))
	if err != nil {
		t.Fatal("missing private recipient")
	}
	mark := filepath.Join(dir, "smtp-attempted")
	f, err := os.OpenFile(mark, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal("existing SMTP attempt: inspect result; automatic repeat disabled")
	}
	if _, err = f.WriteString("attempt_started\n"); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err = f.Sync(); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	now := time.Now()
	d := domain.Delivery{ID: "smtp-live-" + strconv.FormatInt(now.UnixNano(), 10), Target: strings.TrimSpace(string(recipient)), Payload: domain.Incident{ID: "smtp-live-test", Rule: "M02", RuleVersion: 1, Revision: 1, Severity: "P1", Status: "firing", FirstSeen: now, LastSeen: now, Count: 1, Scope: "仅备用邮箱联调，不代表生产故障", Explanation: domain.Explanation{Conclusion: "心链词元备用邮件联调测试", Facts: []string{"这是一封测试邮件，无需处理业务。"}, Missing: []string{"SMTP 接收不代表收件箱已展示。"}}}}
	sender := SMTP{Address: cfg["OPS_SMTP_ADDRESS"], Username: cfg["OPS_SMTP_USERNAME"], Password: cfg["OPS_SMTP_PASSWORD"], From: cfg["OPS_SMTP_FROM"], ImplicitTLS: cfg["OPS_SMTP_IMPLICIT_TLS"] == "true"}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	id, err := sender.Send(ctx, d, "", "")
	if err != nil {
		t.Fatal("SMTP delivery failed:", err)
	}
	if id == "" {
		t.Fatal("SMTP acceptance ID missing")
	}
	if err = os.WriteFile(filepath.Join(dir, "smtp-accepted"), []byte("smtp_data_accepted\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Log("SMTP DATA accepted; inbox receipt remains to be verified")
}
