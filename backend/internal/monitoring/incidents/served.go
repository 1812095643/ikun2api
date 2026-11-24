package incidents

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/store"
)

const (
	// S01 is a sudden outage only if the group/model served traffic this recently;
	// otherwise the group never served that model and it goes to the daily digest.
	servedLookback = 7 * 24 * time.Hour
	servedStateKey = "served"
	digestStateKey = "s01_digest"
	digestHour     = 9 // daily digest after 09:00 Beijing time
	digestFacts    = 6 // the card shows at most six facts
	// With less history than this, "never served" cannot be trusted: alert instead.
	minServedCoverage = 24 * time.Hour
)

var beijing = time.FixedZone("CST", 8*3600)

// servedIndex records the last successful request per group/model.
type servedIndex struct {
	Bootstrapped bool                 `json:"bootstrapped"`
	CoveredSince time.Time            `json:"covered_since"`
	Last         map[string]time.Time `json:"last"`
}

func servedKey(group int64, model string) string { return fmt.Sprintf("%d/%s", group, model) }

// servedEvidence: a successful gateway completion or a billed usage record.
func servedEvidence(o domain.Observation) bool {
	if o.GroupID <= 0 || o.Model == "" || o.Quality == domain.Stale || o.Quality == domain.Unsupported {
		return false
	}
	return (o.Kind == "request.finished" && o.Outcome == "success") || o.Kind == "usage.recorded"
}

// UpdateServed folds new observations into the served index. The first call
// bootstraps from stored observations so a restart does not forget history.
func UpdateServed(ctx context.Context, s *store.Store, rows []domain.Observation, now time.Time) error {
	var idx servedIndex
	if err := s.State(ctx, servedStateKey, &idx); err != nil {
		return err
	}
	if idx.Last == nil {
		idx.Last = map[string]time.Time{}
	}
	changed := false
	if !idx.Bootstrapped {
		boot, err := s.ServedSince(ctx, now.Add(-servedLookback))
		if err != nil {
			return err
		}
		for k, t := range boot {
			if t.After(idx.Last[k]) {
				idx.Last[k] = t
			}
		}
		oldest, err := s.OldestObservation(ctx)
		if err != nil {
			return err
		}
		if oldest.IsZero() || oldest.After(now) {
			oldest = now
		}
		if cut := now.Add(-servedLookback); oldest.Before(cut) {
			oldest = cut
		}
		idx.CoveredSince = oldest
		idx.Bootstrapped, changed = true, true
	}
	for _, o := range rows {
		if !servedEvidence(o) {
			continue
		}
		k := servedKey(o.GroupID, o.Model)
		if o.OccurredAt.After(idx.Last[k]) {
			idx.Last[k], changed = o.OccurredAt, true
		}
	}
	for k, t := range idx.Last {
		if now.Sub(t) > servedLookback {
			delete(idx.Last, k)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return s.SaveState(ctx, servedStateKey, idx)
}

// neverServed reports whether a group/model had no success in servedLookback.
// Before the index is bootstrapped, or with under minServedCoverage of history,
// it answers false so real outages still page.
func (m *Manager) neverServed(ctx context.Context, group int64, model string, at time.Time) (bool, error) {
	var idx servedIndex
	if err := m.Store.State(ctx, servedStateKey, &idx); err != nil {
		return false, err
	}
	if !idx.Bootstrapped || at.Sub(idx.CoveredSince) < minServedCoverage {
		return false, nil
	}
	last, ok := idx.Last[servedKey(group, model)]
	return !ok || at.Sub(last) > servedLookback, nil
}

// dailyDigest posts one card per day listing never-served group/models that were
// requested since the previous digest, then marks those hits as reported.
func (m *Manager) dailyDigest(ctx context.Context, cfg domain.Config, now time.Time) error {
	local := now.In(beijing)
	if local.Hour() < digestHour || !cfg.Rule("S01").Enabled {
		return nil
	}
	day := local.Format("2006-01-02")
	var state struct {
		Last string `json:"last"`
	}
	if err := m.Store.State(ctx, digestStateKey, &state); err != nil {
		return err
	}
	if state.Last == day {
		return nil
	}
	fingerprint := "digest:S01:" + day
	existing, err := m.Store.Incident(ctx, fingerprint)
	if err != nil {
		return err
	}
	if existing == nil {
		rows, err := m.Store.DigestOnlyIncidents(ctx)
		if err != nil {
			return err
		}
		type entry struct {
			i    domain.Incident
			hits int
		}
		var entries []entry
		total := 0
		for _, i := range rows {
			if hits := i.Count - i.DigestedCount; hits > 0 {
				entries = append(entries, entry{i, hits})
				total += hits
			}
		}
		if len(entries) > 0 {
			sort.SliceStable(entries, func(a, b int) bool { return entries[a].hits > entries[b].hits })
			facts := []string{fmt.Sprintf("共 %d 个分组/模型，被请求 %d 次；这些组合近 7 天没有成功服务记录", len(entries), total)}
			for _, e := range entries {
				if len(facts) == digestFacts {
					facts[digestFacts-1] = fmt.Sprintf("另有 %d 个组合，详见后台事件列表", len(entries)-(digestFacts-2))
					break
				}
				facts = append(facts, fmt.Sprintf("分组 #%d · 模型 %s · 被请求 %d 次", e.i.GroupID, e.i.Model, e.hits))
			}
			d := domain.Incident{ID: incidentID(fingerprint), Fingerprint: fingerprint, Rule: "S01", RuleVersion: cfg.Rule("S01").Version, Scope: "digest", Status: "resolved", Severity: "P3", FirstSeen: now, LastSeen: now, Count: total, Revision: 1, Quality: domain.Fresh,
				Explanation: domain.Explanation{Conclusion: "每日摘要：从未成功服务过的分组/模型被请求", Confidence: "high", Facts: facts, Inferences: []string{}, Missing: []string{"通常是客户端请求了分组不支持的模型，或分组缺少模型映射；不是渠道突发故障"}, Advice: []string{"为这些分组补充模型映射或可用账号，或告知相关用户换用支持的模型"}}}
			ds := route(d, "firing", cfg, now, false)
			if len(ds) > 0 {
				d.LastNotified = now
			}
			if err = m.Store.SaveIncident(ctx, d, ds); err != nil {
				return err
			}
			for _, e := range entries {
				e.i.DigestedCount = e.i.Count
				if err = m.Store.SaveIncident(ctx, e.i, nil); err != nil {
					return err
				}
			}
		}
	}
	state.Last = day
	return m.Store.SaveState(ctx, digestStateKey, state)
}
