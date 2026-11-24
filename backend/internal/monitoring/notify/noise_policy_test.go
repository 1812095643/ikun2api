package notify

import (
	"context"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/store"
	"path/filepath"
	"testing"
	"time"
)

func TestPreviouslyQueuedNoiseIsSuppressed(t *testing.T) {
	for _, tc := range []struct{ rule, kind string }{{"L02", "firing"}, {"L02", "resolved"}, {"U01", "reminder"}} {
		t.Run(tc.rule+tc.kind, func(t *testing.T) {
			ctx := context.Background()
			s, e := store.Open(filepath.Join(t.TempDir(), "db"))
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			now := time.Now()
			i := domain.Incident{ID: "i", Fingerprint: "f", Rule: tc.rule, Revision: 1, LastSeen: now}
			if e = s.SaveIncident(ctx, i, []domain.Delivery{{ID: "d", IncidentID: "i", Revision: 1, Channel: "feishu", Target: "oc_test", Kind: tc.kind, NextAttempt: now}}); e != nil {
				t.Fatal(e)
			}
			sender := &stubSender{}
			w := &Worker{Store: s, Senders: map[string]Sender{"feishu": sender}}
			if e = w.Tick(ctx, now); e != nil {
				t.Fatal(e)
			}
			ds, _ := s.Deliveries(ctx)
			if sender.calls != 0 || len(ds) != 1 || ds[0].State != "suppressed" {
				t.Fatal(ds, sender.calls)
			}
		})
	}
}
