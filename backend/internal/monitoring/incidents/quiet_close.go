package incidents

import (
	"context"
	"strings"
	"time"

	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
)

// quietCardEdit bounds which Feishu cards are edited on a quiet close. Older
// cards are long scrolled away and may no longer be editable.
const quietCardEdit = 24 * time.Hour

// QuietCloses reports whether a rule's incidents close after a quiet stretch.
func QuietCloses(rule string) bool { return domain.QuietCloseAfter(rule) > 0 }

// SourcesFresh reports whether every evidence source has reported recently.
// While a source is behind, silence proves nothing, so nothing closes quietly.
func SourcesFresh(cursors []domain.Cursor, now time.Time) bool {
	for _, c := range cursors {
		if c.Source == "notification" || c.LastSuccess.IsZero() {
			continue
		}
		staleAfter := 2 * time.Minute
		if strings.Contains(c.Source, "reconcile") {
			staleAfter = 2 * time.Hour
		}
		if now.Sub(c.LastSuccess) > staleAfter || c.Quality == domain.Stale {
			return false
		}
	}
	return true
}

func quietClosable(i domain.Incident, now time.Time) bool {
	after := domain.QuietCloseAfter(i.Rule)
	if i.Status == "resolved" || i.Status == "superseded" || after == 0 {
		return false
	}
	last := i.LastFault
	if last.IsZero() {
		last = i.LastSeen
	}
	return !last.IsZero() && now.Sub(last) >= after
}

// CloseQuiet closes incidents with no new fault for their rule's quiet window. It never
// posts a new message or email: a recent card is edited in place, older ones are
// left alone. A later fault reopens the incident as a new episode.
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
		i.Status = "resolved"
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
