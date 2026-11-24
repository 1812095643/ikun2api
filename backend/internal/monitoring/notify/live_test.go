package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/store"
)

// This explicit, opt-in test sends ONE synthetic revision to the configured
// recipient. Keep the directory between invocations: accepted revisions are
// never resent, and later invocations exercise persisted message-ID recovery.
func TestLiveFeishuDelivery(t *testing.T) {
	dir := os.Getenv("OPS_FEISHU_LIVE_DIR")
	if dir == "" {
		t.Skip("real delivery requires explicit private configuration")
	}
	stage, err := strconv.Atoi(os.Getenv("OPS_FEISHU_LIVE_STAGE"))
	if err != nil || stage < 1 || stage > 3 {
		t.Fatal("set stage 1=create, 2=update, 3=recovery")
	}
	read := func(name string) string {
		p := filepath.Join(dir, name)
		info, err := os.Lstat(p)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			t.Fatal("configuration must be a private regular file:", name)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal("cannot read private configuration")
		}
		return strings.TrimSpace(string(b))
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		t.Fatal("live directory must have mode 0700")
	}
	values := map[string]string{}
	for _, line := range strings.Split(read("feishu.env"), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok {
			values[k] = v
		}
	}
	target := read("recipient.txt")
	if !platformID.MatchString(target) || !strings.HasPrefix(target, "ou_") {
		t.Fatal("this acceptance test requires an explicit private-chat recipient")
	}
	ctx := context.Background()
	s, err := store.Open(filepath.Join(dir, "live-acceptance.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cfg, err := s.Config(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Version == 0 {
		cfg = domain.DefaultConfig()
		cfg.Site, cfg.Actor, cfg.RequestID = "live-test", "admin-live-test", "live-config-v1"
		cfg.Shadow = false
		cfg.FeishuTargets = []string{target}
		cfg.Rules["U01"] = domain.RuleConfig{Enabled: true, Owner: "monitor", Version: 1}
		if err = s.SaveConfig(ctx, cfg, 0); err != nil {
			t.Fatal(err)
		}
	}
	if len(cfg.FeishuTargets) != 1 || cfg.FeishuTargets[0] != target || len(cfg.EmailTargets) != 0 {
		t.Fatal("unexpected recipients in isolated acceptance database")
	}
	const incidentID = "feishu-live-acceptance-v1"
	deliveryID := fmt.Sprintf("%s-%d", incidentID, stage)
	rows, err := s.Deliveries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	previousAccepted := stage == 1
	for _, d := range rows {
		if d.ID == deliveryID {
			if d.State == "accepted" {
				t.Log("revision already accepted; no request sent")
				return
			}
			t.Fatal("existing unaccepted delivery requires investigation; no automatic resend", d.State, d.ErrorClass)
		}
		if d.IncidentID == incidentID && d.Revision == stage-1 && d.State == "accepted" {
			previousAccepted = true
		}
	}
	if !previousAccepted {
		t.Fatal("previous revision must be accepted first")
	}
	priorMessage := s.MessageID(ctx, incidentID, "feishu", target)
	if stage > 1 && priorMessage == "" {
		t.Fatal("persisted message mapping missing")
	}
	now := time.Now().UTC()
	i := domain.Incident{ID: incidentID, Fingerprint: incidentID, Rule: "U01", RuleVersion: 1, Revision: stage, Severity: "P2", Status: "firing", Scope: "仅本人私聊；合成联调，不代表生产异常", FirstSeen: now, LastSeen: now, Count: stage, Quality: domain.Fresh}
	if old, _ := s.Incident(ctx, incidentID); old != nil {
		i.FirstSeen = old.FirstSeen
	}
	i.Explanation = domain.Explanation{Conclusion: "联调测试：验证飞书私聊通知", Confidence: "high", Facts: []string{"这是合成测试，无真实用户请求或生产故障。", "发送使用监控模块正式 Feishu 适配器和 SQLite outbox。"}, Inferences: []string{"平台接收回执不等于人工已读。"}, Advice: []string{"无需处理业务；请查看卡片通知版本和状态变化。"}, Missing: []string{"不验证生产自动采集或 AI 回复。"}}
	kind := "firing"
	if stage == 2 {
		i.Explanation.Conclusion = "联调测试：同一张卡片已更新至版本 2"
		kind = "update"
	}
	if stage == 3 {
		i.Status = "resolved"
		i.Explanation.Conclusion = "联调测试完成：发送、原卡更新、恢复状态更新均已通过"
		kind = "resolved"
	}
	d := domain.Delivery{ID: deliveryID, IncidentID: i.ID, Revision: stage, Channel: "feishu", Target: target, Kind: kind, State: "pending", NextAttempt: now, Payload: i}
	if err = s.SaveIncident(ctx, i, []domain.Delivery{d}); err != nil {
		t.Fatal(err)
	}
	transport := &liveReceiptTransport{base: &http.Transport{Proxy: nil}}
	w := Worker{Store: s, Senders: map[string]Sender{"feishu": NewFeishuWithTransport(values["OPS_FEISHU_APP_ID"], values["OPS_FEISHU_APP_SECRET"], transport)}}
	if err = w.Tick(ctx, now); err != nil {
		t.Fatal(err)
	}
	rows, err = s.Deliveries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, got := range rows {
		if got.ID == deliveryID {
			report := map[string]any{"stage": stage, "state": got.State, "error_class": got.ErrorClass, "message_id": got.MessageID, "same_message": stage == 1 || got.MessageID == priorMessage, "http": transport.receipts, "at": now}
			b, _ := json.MarshalIndent(report, "", "  ")
			if err = os.WriteFile(filepath.Join(dir, fmt.Sprintf("receipt-%d.json", stage)), b, 0600); err != nil {
				t.Fatal(err)
			}
			t.Logf("stage=%d state=%s error_class=%s same_message=%v", stage, got.State, got.ErrorClass, report["same_message"])
			if got.State != "accepted" || got.MessageID == "" || stage > 1 && got.MessageID != priorMessage {
				t.Fatal("real Feishu acceptance failed; inspect sanitized receipt")
			}
			return
		}
	}
	t.Fatal("delivery not found")
}

type liveReceiptTransport struct {
	base     http.RoundTripper
	receipts []map[string]any
}

func (r *liveReceiptTransport) RoundTrip(q *http.Request) (*http.Response, error) {
	res, err := r.base.RoundTrip(q)
	if err != nil {
		return nil, err
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	res.Body.Close()
	if err != nil {
		return nil, err
	}
	res.Body = io.NopCloser(bytes.NewReader(b))
	var out struct {
		Code int `json:"code"`
	}
	_ = json.Unmarshal(b, &out)
	r.receipts = append(r.receipts, map[string]any{"method": q.Method, "path": q.URL.Path, "status": res.StatusCode, "code": out.Code, "log_id": res.Header.Get("X-Tt-Logid")})
	return res, nil
}
