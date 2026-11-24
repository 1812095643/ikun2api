package incidents

import (
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"strings"
	"time"
)

const (
	// A gated incident opens a card only after this much evidence in gateWindow.
	gateWindow   = 10 * time.Minute
	gateRequests = 5
	gateUsers    = 2
	maxHits      = 64
	// "已恢复" is announced only after this long without a new fault.
	quietBeforeResolve = 30 * time.Minute
	// A fault within this long of the last fault/recovery edits the existing card
	// silently; after it, the fault starts a new episode and a new message.
	episodeGap = time.Hour
)

// Single upstream/retry/protocol failures are archived until they affect enough traffic.
var gatedRules = map[string]bool{"U01": true, "U02": true, "U03": true, "F01": true, "R01": true, "F02": true}

func gated(i domain.Incident) bool { return gatedRules[i.Rule] }

// rolledUp members are reported by their account or group rollup card.
func rolledUp(i domain.Incident) bool {
	return i.ParentID != "" && (gated(i) || i.Rule == "S01")
}

// Keep individual latency evidence queryable without paging for each request.
func updateInterval(i domain.Incident) time.Duration {
	if i.Severity == "P2" || strings.HasPrefix(i.Scope, "signature:") {
		return time.Hour
	}
	return 5 * time.Minute
}
func freshUnnotifiedFault(i domain.Incident, now time.Time) bool {
	return i.Count > i.NotifiedCount && i.LastFault.After(i.LastNotified) && !i.LastFault.After(now) && now.Sub(i.LastFault) <= 10*time.Minute
}

// carded reports whether the current episode already has a message in chat.
// Legacy incidents have no EpisodeStart and count as carded once notified.
func carded(i domain.Incident) bool {
	return !i.LastNotified.IsZero() && !i.LastNotified.Before(i.EpisodeStart)
}

// startsEpisode: a fault more than episodeGap after the previous fault, or after
// the recorded recovery, is a new outage and deserves a fresh message.
// Rules in silentRecurrence never start a new episode: any recurrence edits the
// existing card. Source gaps, pool shrink and quarantine flaps are not worth a new push.
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

// notifyKind decides the card action for a revision. forceNew posts a new
// message (which pushes) instead of editing the episode's card (which does not).
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
			// Recurrence inside episodeGap: flip the existing card back silently.
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
