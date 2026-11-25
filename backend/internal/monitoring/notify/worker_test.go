package notify

import (
	"context"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/store"
	"path/filepath"
	"testing"
	"time"
)

type stubSender struct {
	calls   int
	failure *Failure
}

func (s *stubSender) Send(context.Context, domain.Delivery, string, string) (string, error) {
	s.calls++
	if s.failure != nil {
		return "", s.failure
	}
	return "om_mock", nil
}
func TestRetryAfterWithoutEmailFallback(t *testing.T) {
	ctx := context.Background()
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	now := time.Now().UTC()
	cfg := domain.DefaultConfig()
	cfg.Shadow = false
	cfg.Actor = "admin"
	cfg.RequestID = "cfg"
	cfg.Rules["U01"] = domain.RuleConfig{Enabled: true, Owner: "monitor", Version: 1}
	cfg.FeishuTargets = []string{"oc_test"}
	cfg.EmailTargets = []string{"ops@example.test"}
	if e = s.SaveConfig(ctx, cfg, 0); e != nil {
		t.Fatal(e)
	}
	i := domain.Incident{ID: "incident", Fingerprint: "fp", Rule: "U01", Revision: 1, Severity: "P2", Status: "firing", LastSeen: now}
	d := domain.Delivery{ID: "delivery", IncidentID: i.ID, Revision: 1, Channel: "feishu", Target: "oc_test", Kind: "firing", NextAttempt: now}
	if e = s.SaveIncident(ctx, i, []domain.Delivery{d}); e != nil {
		t.Fatal(e)
	}
	sender := &stubSender{failure: &Failure{Class: "rate_limited", RetryAfter: time.Minute}}
	w := &Worker{Store: s, Senders: map[string]Sender{"feishu": sender, "email": &stubSender{}}}
	for n := 0; n < 3; n++ {
		if e = w.Tick(ctx, now.Add(time.Duration(n)*2*time.Minute)); e != nil {
			t.Fatal(e)
		}
	}
	ds, _ := s.Deliveries(ctx)
	if len(ds) != 1 || ds[0].Channel != "feishu" {
		t.Fatal("unexpected fallback", ds)
	}
	for _, row := range ds {
		if row.Channel == "feishu" && row.NextAttempt.Before(now.Add(5*time.Minute)) {
			t.Fatal("Retry-After ignored", row)
		}
	}
	sender.failure = nil
	if e = w.Tick(ctx, now.Add(8*time.Minute)); e != nil {
		t.Fatal(e)
	}
	ds, _ = s.Deliveries(ctx)
	if len(ds) != 1 || ds[0].State != "accepted" {
		t.Fatal(ds)
	}
}
func TestShadowAndOwnershipChangesStopQueuedSends(t *testing.T) {
	ctx := context.Background()
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	now := time.Now()
	i := domain.Incident{ID: "i", Fingerprint: "f", Rule: "U01", Revision: 1, LastSeen: now}
	if e = s.SaveIncident(ctx, i, []domain.Delivery{{ID: "d", IncidentID: "i", Revision: 1, Channel: "feishu", Target: "oc_x", NextAttempt: now}}); e != nil {
		t.Fatal(e)
	}
	sender := &stubSender{}
	w := &Worker{Store: s, Senders: map[string]Sender{"feishu": sender}}
	if e = w.Tick(ctx, now); e != nil {
		t.Fatal(e)
	}
	if sender.calls != 0 {
		t.Fatal("default shadow leaked message")
	}
}

func TestPermanentDenialNoFallbackAndSelfAlertDoesNotRecurse(t *testing.T) {
	ctx := context.Background()
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	now := time.Now()
	cfg := domain.DefaultConfig()
	cfg.Shadow = false
	cfg.Actor = "a"
	cfg.RequestID = "c"
	cfg.FeishuTargets = []string{"oc_test"}
	cfg.EmailTargets = []string{"ops@example.test"}
	cfg.Rules["M02"] = domain.RuleConfig{Enabled: true, Owner: "monitor", Version: 1}
	if e = s.SaveConfig(ctx, cfg, 0); e != nil {
		t.Fatal(e)
	}
	i := domain.Incident{ID: "self", Fingerprint: "self", Rule: "M02", Revision: 1, Severity: "P1", LastSeen: now}
	if e = s.SaveIncident(ctx, i, []domain.Delivery{{ID: "self-delivery", IncidentID: i.ID, Revision: 1, Channel: "feishu", Target: "oc_test", Kind: "firing", NextAttempt: now}}); e != nil {
		t.Fatal(e)
	}
	w := &Worker{Store: s, Senders: map[string]Sender{"feishu": &stubSender{failure: &Failure{Class: "permission_denied", Permanent: true}}}}
	if e = w.Tick(ctx, now); e != nil {
		t.Fatal(e)
	}
	ds, _ := s.Deliveries(ctx)
	if len(ds) != 1 || ds[0].State != "dead" {
		t.Fatal("permanent denial not recorded", ds)
	}
	rows, _ := s.Observations(ctx, time.Time{}, 100)
	if len(rows) != 0 {
		t.Fatal("self-alert recursively observed", rows)
	}
}

func TestSlowFirstOutputNeverEmailsEvenOnFailureOrOldQueue(t *testing.T) {
	ctx := context.Background()
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	now := time.Now()
	cfg := domain.DefaultConfig()
	cfg.Shadow = false
	cfg.Actor = "a"
	cfg.RequestID = "config"
	cfg.FeishuTargets = []string{"ou_test"}
	cfg.EmailTargets = []string{"ops@example.test"}
	cfg.Rules["L02"] = domain.RuleConfig{Enabled: true, Owner: "monitor"}
	if e = s.SaveConfig(ctx, cfg, 0); e != nil {
		t.Fatal(e)
	}
	i := domain.Incident{ID: "slow", Fingerprint: "slow", Rule: "L02", Revision: 1, Severity: "P1", LastSeen: now}
	ds := []domain.Delivery{{ID: "a", IncidentID: i.ID, Revision: 1, Channel: "email", Target: "ops@example.test", Kind: "firing", NextAttempt: now}, {ID: "b", IncidentID: i.ID, Revision: 1, Channel: "feishu", Target: "ou_test", Kind: "firing", NextAttempt: now}}
	if e = s.SaveIncident(ctx, i, ds); e != nil {
		t.Fatal(e)
	}
	mail := &stubSender{}
	w := &Worker{Store: s, Senders: map[string]Sender{"email": mail, "feishu": &stubSender{failure: &Failure{Class: "permission_denied", Permanent: true}}}}
	if e = w.Tick(ctx, now); e != nil {
		t.Fatal(e)
	}
	if e = w.Tick(ctx, now.Add(time.Second)); e != nil {
		t.Fatal(e)
	}
	out, e := s.Deliveries(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if mail.calls != 0 {
		t.Fatal("old queued email delivered")
	}
	if len(out) != 2 {
		t.Fatalf("unexpected fallback deliveries: %d", len(out))
	}
}

// Email is delivered when routed; a Feishu failure never creates an email fallback.
func TestEmailDeliversAndFeishuFailureCreatesNoFallback(t *testing.T) {
	ctx := context.Background()
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	now := time.Now()
	cfg := domain.DefaultConfig()
	cfg.Shadow = false
	cfg.Actor = "a"
	cfg.RequestID = "config"
	cfg.FeishuTargets = []string{"ou_test"}
	cfg.EmailTargets = []string{"ops@example.test"}
	cfg.Rules["U01"] = domain.RuleConfig{Enabled: true, Owner: "monitor"}
	if e = s.SaveConfig(ctx, cfg, 0); e != nil {
		t.Fatal(e)
	}
	i := domain.Incident{ID: "up", Fingerprint: "up", Rule: "U01", Revision: 1, Severity: "P1", LastSeen: now}
	ds := []domain.Delivery{{ID: "a", IncidentID: i.ID, Revision: 1, Channel: "email", Target: "ops@example.test", NextAttempt: now}, {ID: "b", IncidentID: i.ID, Revision: 1, Channel: "feishu", Target: "ou_test", NextAttempt: now}}
	if e = s.SaveIncident(ctx, i, ds); e != nil {
		t.Fatal(e)
	}
	mail := &stubSender{}
	w := Worker{Store: s, Senders: map[string]Sender{"email": mail, "feishu": &stubSender{failure: &Failure{Class: "denied", Permanent: true}}}}
	if e = w.Tick(ctx, now); e != nil {
		t.Fatal(e)
	}
	if e = w.Tick(ctx, now.Add(time.Second)); e != nil {
		t.Fatal(e)
	}
	out, e := s.Deliveries(ctx)
	if e != nil || len(out) != 2 || mail.calls != 1 {
		t.Fatalf("email must be delivered once with no fallback: calls=%d deliveries=%d", mail.calls, len(out))
	}
}

func TestQuietCloseNeverPostsANewMessage(t *testing.T) {
	ctx := context.Background()
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	now := time.Now().UTC()
	cfg := domain.DefaultConfig()
	cfg.Shadow = false
	cfg.Actor = "admin"
	cfg.RequestID = "cfg"
	cfg.Rules["U01"] = domain.RuleConfig{Enabled: true, Owner: "monitor", Version: 1}
	cfg.FeishuTargets = []string{"oc_test"}
	if e = s.SaveConfig(ctx, cfg, 0); e != nil {
		t.Fatal(e)
	}
	// No card was ever recorded for this incident, so there is nothing to edit.
	i := domain.Incident{ID: "quiet", Fingerprint: "quiet", Rule: "U01", Revision: 2, Severity: "P1", Status: "resolved", ClosedBy: "quiet", LastSeen: now}
	d := domain.Delivery{ID: "quiet-edit", IncidentID: i.ID, Revision: 2, Channel: "feishu", Target: "oc_test", Kind: "closed", NextAttempt: now, Payload: i}
	if e = s.SaveIncident(ctx, i, []domain.Delivery{d}); e != nil {
		t.Fatal(e)
	}
	sender := &stubSender{}
	w := &Worker{Store: s, Senders: map[string]Sender{"feishu": sender}}
	if e = w.Tick(ctx, now); e != nil {
		t.Fatal(e)
	}
	ds, _ := s.Deliveries(ctx)
	if sender.calls != 0 || len(ds) != 1 || ds[0].State != "suppressed" {
		t.Fatalf("calls=%d deliveries=%+v", sender.calls, ds)
	}
}
