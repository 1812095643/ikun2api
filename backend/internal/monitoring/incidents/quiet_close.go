package incidents

import (
	"context"
	"strings"
	"time"

	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
)

const quietCardEdit = 24 * time.Hour

func QuietCloses(rule string) bool { return domain.QuietCloseAfter(rule) > 0 }

func SourcesFresh(cursors []domain.Cursor, now time.Time) bool {
	sources := 0
	for _, c := range cursors {
		if c.Source == "notification" {
			continue
		}
		sources++
		staleAfter := 2 * time.Minute
		if strings.Contains(c.Source, "reconcile") {
			staleAfter = 2 * time.Hour
		}
		if c.LastSuccess.IsZero() || now.Sub(c.LastSuccess) > staleAfter || c.Quality != domain.Fresh {
			return false
		}
	}
	return sources > 0
}

func quietClosable(i domain.Incident, now time.Time) bool {
	after := domain.QuietCloseAfter(i.Rule)
	if i.Status == "resolved" || i.Status == "superseded" || i.Status == "closed" || after == 0 {
		return false
	}
	last := i.LastFault
	if last.IsZero() {
		last = i.LastSeen
	}
	return !last.IsZero() && now.Sub(last) >= after
}

func (m *Manager) CloseQuiet(ctx context.Context, cfg domain.Config, now time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rows, err := m.Store.ActiveIncidents(ctx)
	if err != nil {
		return 0, err
	}
	closed := 0
	for _, i := range rows {
		if !quietClosable(i, now) {
			continue
		}
		wasCarded := carded(i)
		i.Status = "closed"
		i.ResolvedAt = now
		i.ClosedBy = "quiet"
		i.RecoveringSince = time.Time{}
		i.Revision++
		var ds []domain.Delivery
		if wasCarded {
			// Marks the recovery as announced so Tick does not post "已恢复" for it.
			edit := now.Sub(i.LastNotified) <= quietCardEdit && !now.Before(i.LastNotified)
			i.LastNotified = now
			if edit && !i.DigestOnly && !now.Before(i.SilencedUntil) && cfg.Rule(i.Rule).Enabled {
				ds = route(i, "closed", cfg, now, false)
			}
		}
		if err = m.Store.SaveIncident(ctx, i, ds); err != nil {
			return closed, err
		}
		closed++
	}
	return closed, nil
}
