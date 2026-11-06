package service

import (
	"context"
	"math"
	"sort"
	"time"
)

// One second of weekly urgency outweighs the entire normalized manual-priority
// contribution (less than one point), without rewriting the stored priority.
const claudeWeeklyResetPriorityWeight = 1_000_000.0

// claudeWeeklyResetOrder only orders candidates that have already passed the
// existing scheduling gates. It never changes account priority or session affinity.
// A single clock value keeps comparisons transitive when a reset is imminent.
type claudeWeeklyResetOrder struct {
	enabled bool
	now     time.Time
}

func newClaudeWeeklyResetOrder(group *Group, now time.Time) claudeWeeklyResetOrder {
	return claudeWeeklyResetOrder{
		enabled: group != nil && group.Platform == PlatformAnthropic && group.PreferSoonestWeeklyReset,
		now:     now,
	}
}

func (s *GatewayService) claudeWeeklyResetOrder(ctx context.Context, groupID *int64) claudeWeeklyResetOrder {
	var group *Group
	if groupID != nil {
		group = s.groupFromContext(ctx, *groupID)
		if group == nil && s.groupRepo != nil {
			group, _ = s.groupRepo.GetByIDLite(ctx, *groupID)
		}
	}
	return newClaudeWeeklyResetOrder(group, time.Now())
}

func (o claudeWeeklyResetOrder) resetAt(account *Account) (time.Time, bool) {
	if !o.enabled || account == nil || account.Platform != PlatformAnthropic {
		return time.Time{}, false
	}
	// Both active usage queries and passive response sampling persist the same
	// overall 7d window. Do not substitute the 5h session or model-specific quota.
	raw := account.Extra["passive_usage_7d_reset"]
	end := parseExtraTime(raw)
	if end.IsZero() {
		seconds := parseExtraFloat64(raw)
		if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 {
			return time.Time{}, false
		}
		if seconds > 1e11 {
			seconds /= 1000
		}
		if seconds > float64(o.now.Add(7*24*time.Hour+time.Minute).Unix()) {
			return time.Time{}, false
		}
		end = time.Unix(int64(seconds), 0)
	}
	if !end.After(o.now) || end.After(o.now.Add(7*24*time.Hour+time.Minute)) {
		return time.Time{}, false
	}
	if used, ok := resolveAccountExtraNumber(account.Extra, "passive_usage_7d_utilization"); ok && used >= 1 {
		return time.Time{}, false
	}
	return end, true
}

// score adds a dominant weekly-reset contribution to a bounded, monotonic
// manual-priority score. Only a usable future overall 7d window earns a bonus.
func (o claudeWeeklyResetOrder) score(account *Account) float64 {
	priorityScore := 0.5 - math.Atan(float64(account.Priority))/math.Pi
	if end, ok := o.resetAt(account); ok {
		remaining := end.Sub(o.now).Seconds()
		urgency := (7*24*time.Hour + time.Minute).Seconds() + 1 - remaining
		return priorityScore + urgency*claudeWeeklyResetPriorityWeight
	}
	return priorityScore
}

// Existing tie breakers remain authoritative when the switch is off or scores tie.
func (o claudeWeeklyResetOrder) compare(a, b *Account) int {
	if !o.enabled {
		return 0
	}
	left, right := o.score(a), o.score(b)
	switch {
	case left > right:
		return -1
	case left < right:
		return 1
	default:
		return 0
	}
}

func (o claudeWeeklyResetOrder) sortAccounts(accounts []*Account) {
	if o.enabled {
		sort.SliceStable(accounts, func(i, j int) bool { return o.compare(accounts[i], accounts[j]) < 0 })
	}
}

func (o claudeWeeklyResetOrder) sortLoaded(accounts []accountWithLoad) {
	if o.enabled {
		sort.SliceStable(accounts, func(i, j int) bool { return o.compare(accounts[i].account, accounts[j].account) < 0 })
	}
}

// Keep the existing layered selection: only its priority contribution is enhanced.
// Load, LRU, account gates and slot acquisition continue in their original stages.
func (o claudeWeeklyResetOrder) filterByPriority(accounts []accountWithLoad) []accountWithLoad {
	if !o.enabled || len(accounts) < 2 {
		return filterByMinPriority(accounts)
	}
	best := math.Inf(-1)
	result := make([]accountWithLoad, 0, len(accounts))
	for _, item := range accounts {
		score := o.score(item.account)
		if score > best {
			best = score
			result = result[:0]
		}
		if score == best {
			result = append(result, item)
		}
	}
	return filterByMinPriority(result)
}
