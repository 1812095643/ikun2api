package incidents

import (
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"strings"
	"time"
)

const (
	gateWindow   = 10 * time.Minute
	gateRequests = 5
	gateUsers    = 2
	maxHits      = 64
	// "已恢复" is announced only after this long without a new fault.
	quietBeforeResolve = 30 * time.Minute
	episodeGap         = time.Hour
)

var gatedRules = map[string]bool{"U01": true, "U02": true, "U03": true, "F01": true, "R01": true, "F02": true}

func gated(i domain.Incident) bool { return gatedRules[i.Rule] }

func rolledUp(i domain.Incident) bool {
	return i.ParentID != "" && (gated(i) || i.Rule == "S01")
}

func updateInterval(i domain.Incident) time.Duration {
	if i.Severity == "P2" || strings.HasPrefix(i.Scope, "signature:") {
		return time.Hour
	}
	return 5 * time.Minute
}
func freshUnnotifiedFault(i domain.Incident, now time.Time) bool {
	return i.Count > i.NotifiedCount && i.LastFault.After(i.LastNotified) && !i.LastFault.After(now) && now.Sub(i.LastFault) <= 10*time.Minute
}

func carded(i domain.Incident) bool {
	return !i.LastNotified.IsZero() && !i.LastNotified.Before(i.EpisodeStart)
}

var silentRecurrence = map[string]bool{"M01": true, "S02": true, "S03": true}

func startsEpisode(i domain.Incident, oldStatus string, prevFault, at time.Time) bool {
	if silentRecurrence[i.Rule] {
		return false
	}
	ref := prevFault
	if oldStatus == "resolved" && !i.ResolvedAt.IsZero() {
		ref = i.ResolvedAt
	}
	return !ref.IsZero() && at.Sub(ref) > episodeGap
}

func addHit(i *domain.Incident, f domain.Finding) {
	req := f.RequestID
	if req == "" {
		req = f.EvidenceID
	}
	i.RecentHits = append(i.RecentHits, domain.RecentHit{Request: req, User: f.UserID, At: f.At})
	latest := time.Time{}
	for _, h := range i.RecentHits {
		if h.At.After(latest) {
			latest = h.At
		}
	}
	kept := i.RecentHits[:0]
	for _, h := range i.RecentHits {
		if latest.Sub(h.At) <= gateWindow {
			kept = append(kept, h)
		}
	}
	if len(kept) > maxHits {
		kept = kept[len(kept)-maxHits:]
	}
	i.RecentHits = kept
}

func thresholdMet(i domain.Incident) bool {
	requests, users := map[string]bool{}, map[int64]bool{}
	for _, h := range i.RecentHits {
		requests[h.Request] = true
		if h.User > 0 {
			users[h.User] = true
		}
	}
	return len(requests) >= gateRequests || len(users) >= gateUsers
}

func routineUpdate(i domain.Incident, accountAppeared bool, now time.Time) bool {
	return accountAppeared || (i.Severity != "P3" && freshUnnotifiedFault(i, now) && now.Sub(i.LastNotified) >= updateInterval(i))
}

func notifyKind(i domain.Incident, oldStatus, oldSeverity string, healthy, accountAppeared bool, now time.Time) (kind string, send, forceNew bool) {
	if !healthy && i.Status == "firing" {
		if !carded(i) {
			if gated(i) && !thresholdMet(i) {
				return "", false, false
			}
			return "firing", true, !i.LastNotified.IsZero()
		}
		switch {
		case oldStatus == "resolved":
			return "update", true, false
		case i.Severity < oldSeverity:
			return "escalated", true, false
		}
		return "update", routineUpdate(i, accountAppeared, now), false
	}
	if !carded(i) {
		return "", false, false
	}
	if i.Status == "resolved" && oldStatus != "resolved" {
		return "resolved", true, false
	}
	return "update", routineUpdate(i, accountAppeared, now), false
}
