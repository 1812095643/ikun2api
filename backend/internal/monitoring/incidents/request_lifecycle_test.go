package incidents

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/store"
)

func TestRequestAccountAndTerminalEnrichStoredEvidence(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	m := &Manager{Store: s}
	now := time.Now().UTC()
	cfg := domain.DefaultConfig()
	cfg.Shadow = false
	cfg.FeishuTargets = []string{"ou_test"}
	cfg.Rules["L02"] = domain.RuleConfig{Enabled: true, Owner: "monitor", Version: 1}
	f := domain.Finding{Rule: "L02", RuleVersion: 1, Fingerprint: "same", Scope: "site:req-1", RequestID: "req-1", UserID: 1, KeyID: 2, GroupID: 3, At: now, EvidenceID: "fault1", Severity: "P2", Quality: domain.Fresh, RequestStartedAt: now.Add(-31 * time.Second)}
	if err = m.Apply(ctx, f, cfg, now); err != nil {
		t.Fatal(err)
	}
	f.At = now.Add(5 * time.Second)
	f.EvidenceID = "fault2"
	f.AccountID = 9
	if err = m.Apply(ctx, f, cfg, f.At); err != nil {
		t.Fatal(err)
	}
	ds, _ := s.Deliveries(ctx)
	if len(ds) != 0 {
		t.Fatalf("single latency must remain observation-only: %+v", ds)
	}
	f.At = now.Add(10 * time.Second)
	f.EvidenceID = "terminal"
	f.Healthy = true
	f.FirstOutputAt = now.Add(8 * time.Second)
	f.RequestEndedAt = now.Add(10 * time.Second)
	f.RequestOutcome = "success"
	if err = m.Apply(ctx, f, cfg, f.At); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.Incidents(ctx)
	if len(rows) != 1 {
		t.Fatal(rows)
	}
	i := rows[0]
	if i.Status != "resolved" || i.Sample == nil || i.Sample.AccountID != 9 || !i.Sample.RequestStartedAt.Equal(now.Add(-31*time.Second)) || !i.Sample.FirstOutputAt.Equal(now.Add(8*time.Second)) || !i.Sample.RequestEndedAt.Equal(f.At) || i.Sample.RequestOutcome != "success" {
		t.Fatalf("incorrect terminal sample: %+v", i)
	}
}
func TestDifferentRequestCannotEnrichPriorSample(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	m := &Manager{Store: s}
	now := time.Now()
	cfg := domain.DefaultConfig()
	cfg.Rules["L02"] = domain.RuleConfig{Enabled: true, Owner: "monitor", Version: 1}
	f := domain.Finding{Rule: "L02", RuleVersion: 1, Fingerprint: "same", Scope: "group:2/model:gpt", RequestID: "r1", At: now, EvidenceID: "one", Severity: "P2", Quality: domain.Fresh, UserID: 1, AccountID: 0}
	if err = m.Apply(ctx, f, cfg, now); err != nil {
		t.Fatal(err)
	}
	f.Healthy = true
	f.RequestID = "r2"
	f.AccountID = 99
	f.At = now.Add(time.Second)
	f.EvidenceID = "two"
	if err = m.Apply(ctx, f, cfg, f.At); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.Incidents(ctx)
	if rows[0].Sample.AccountID != 0 {
		t.Fatal("other request account contaminated sample")
	}
}
