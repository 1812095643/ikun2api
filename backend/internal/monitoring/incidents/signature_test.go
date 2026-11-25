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

func TestSignatureSingleCardHourlyUpdateAcrossRestart(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cfg := domain.DefaultConfig()
	cfg.Shadow = false
	cfg.FeishuTargets = []string{"ou_test"}
	cfg.Rules["F02"] = domain.RuleConfig{Enabled: true, Owner: "monitor", Version: 1}
	m := &Manager{Store: s}
	now := time.Now().UTC()
	f := domain.Finding{Rule: "F02", RuleVersion: 1, Fingerprint: "sig", Scope: "signature:session:abc", Severity: "P1", Quality: domain.Fresh}
	for n := 0; n <= 65; n++ {
		f.At = now.Add(time.Duration(n) * time.Minute)
		f.EvidenceID = fmt.Sprint(n)
		f.AccountID = int64(1 + n%2)
		if n == 30 {
			m = &Manager{Store: s}
		}
		if err = m.Apply(ctx, f, cfg, f.At); err != nil {
			t.Fatal(err)
		}
		if err = m.Apply(ctx, f, cfg, f.At); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.Incidents(ctx)
	if err != nil || len(rows) != 1 || rows[0].Count != 66 {
		t.Fatal(err, rows)
	}
	ds, err := s.Deliveries(ctx)
	if err != nil || len(ds) != 2 {
		t.Fatal(err, len(ds))
	}
	for _, d := range ds {
		if d.Channel != "feishu" {
			t.Fatal(d)
		}
	}
}
