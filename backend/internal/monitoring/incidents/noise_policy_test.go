package incidents

import (
	"context"
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/store"
	"path/filepath"
	"testing"
	"time"
)

func TestNoStaleRemindersAndHourlyP2(t *testing.T) {
	for _, severity := range []string{"P1", "P2"} {
		t.Run(severity, func(t *testing.T) {
			ctx := context.Background()
			s, err := store.Open(filepath.Join(t.TempDir(), "s.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			m := &Manager{Store: s}
			cfg := domain.DefaultConfig()
			cfg.Shadow = false
			cfg.FeishuTargets = []string{"oc_test"}
			cfg.Rules["U01"] = domain.RuleConfig{Enabled: true, Owner: "monitor", Version: 1}
			now := time.Now().UTC()
			f := domain.Finding{Rule: "U01", RuleVersion: 1, Fingerprint: "test", Scope: "account:1", Severity: severity, At: now, Quality: domain.Fresh}
			// Two affected users pass the open-card threshold.
			for _, user := range []int64{1, 2} {
				f.UserID, f.EvidenceID = user, fmt.Sprint("1-", user)
				if err = m.Apply(ctx, f, cfg, now); err != nil {
					t.Fatal(err)
				}
			}
			for n := 1; n <= 192; n++ {
				if err = m.Tick(ctx, cfg, now.Add(time.Duration(n)*15*time.Minute)); err != nil {
					t.Fatal(err)
				}
			}
			ds, _ := s.Deliveries(ctx)
			if len(ds) != 1 {
				t.Fatalf("stale reminders: %d", len(ds))
			}
			f.At = now.Add(6 * time.Minute)
			f.EvidenceID = "2"
			if err = m.Apply(ctx, f, cfg, f.At); err != nil {
				t.Fatal(err)
			}
			ds, _ = s.Deliveries(ctx)
			want := 2
			if severity == "P2" {
				want = 1
			}
			if len(ds) != want {
				t.Fatalf("updates: %d want %d", len(ds), want)
			}
			f.At = now.Add(61 * time.Minute)
			f.EvidenceID = "3"
			if err = m.Apply(ctx, f, cfg, f.At); err != nil {
				t.Fatal(err)
			}
			ds, _ = s.Deliveries(ctx)
			if len(ds) != want+1 {
				t.Fatal("new fault must notify")
			}
		})
	}
}
func TestSingleLatencyRecordedWithoutDelivery(t *testing.T) {
	ctx := context.Background()
	s, e := store.Open(filepath.Join(t.TempDir(), "s.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	cfg := domain.DefaultConfig()
	cfg.Shadow = false
	cfg.FeishuTargets = []string{"oc_test"}
	cfg.Rules["L02"] = domain.RuleConfig{Enabled: true, Owner: "monitor", Version: 1}
	now := time.Now()
	m := &Manager{Store: s}
	f := domain.Finding{Rule: "L02", RuleVersion: 1, Fingerprint: "latency", Severity: "P1", At: now, EvidenceID: "slow", Quality: domain.Fresh}
	if e = m.Apply(ctx, f, cfg, now); e != nil {
		t.Fatal(e)
	}
	ds, _ := s.Deliveries(ctx)
	rows, _ := s.Incidents(ctx)
	if len(ds) != 0 || len(rows) != 1 {
		t.Fatal("must retain incident without notification")
	}
}
