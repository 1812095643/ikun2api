package incidents

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"time"

	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
)

func incidentID(key string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(key)))[:32] }

func route(i domain.Incident, kind string, cfg domain.Config, now time.Time, forceNew bool) []domain.Delivery {
	if i.Rule == "L02" || cfg.Owner(i.Rule, i.Scope, i.GroupID, i.Model) != "monitor" || i.ParentID != "" || now.Before(i.SilencedUntil) {
		return nil
	}
	targets := map[string]domain.Target{}
	for _, t := range cfg.Targets {
		targets[t.ID] = t
	}
	planned := map[string]domain.Delivery{}
	for _, p := range cfg.Policies {
		if !p.Matches(i) || !p.Allows(kind, i.Acknowledged) || kind == "reminder" && now.Sub(i.LastNotified) < time.Duration(p.RepeatMinutes)*time.Minute {
			continue
		}
		for _, id := range p.TargetIDs {
			t, ok := targets[id]
			if !ok {
				continue
			}
			// 计数变化只编辑已有消息，不支持编辑的平台保留站内累计。
			if (kind == "update" || kind == "closed") && !domain.SupportsEdit(t.ChannelKind) && t.ChannelKind != "webhook" {
				continue
			}
			if kind == "update" && !i.LastNotified.IsZero() && now.Sub(i.LastNotified) < time.Minute {
				continue
			}
			d, exists := planned[id]
			if !exists {
				state := "pending"
				if cfg.Shadow {
					state = "shadow"
				}
				d = domain.Delivery{ID: fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%s:%s:%s", i.ID, i.Revision, t.ChannelID, id, kind)))), IncidentID: i.ID, Revision: i.Revision, Channel: t.ChannelID, Target: id, Kind: kind, State: state, NextAttempt: now, ForceNew: forceNew}
			}
			d.MentionAll = d.MentionAll || p.MentionAll
			for _, fallback := range p.FallbackTargetIDs {
				if fallback != id && !slices.Contains(p.TargetIDs, fallback) && !slices.Contains(d.FallbackTargetIDs, fallback) {
					if _, ok := targets[fallback]; ok {
						d.FallbackTargetIDs = append(d.FallbackTargetIDs, fallback)
					}
				}
			}
			planned[id] = d
		}
	}
	out := make([]domain.Delivery, 0, len(planned))
	for _, d := range planned {
		out = append(out, d)
	}
	slices.SortFunc(out, func(a, b domain.Delivery) int {
		if a.Target < b.Target {
			return -1
		}
		if a.Target > b.Target {
			return 1
		}
		return 0
	})
	return out
}

func reminderDue(i domain.Incident, cfg domain.Config, now time.Time) bool {
	if i.Acknowledged || i.Status != "firing" || now.Before(i.SilencedUntil) || i.LastNotified.IsZero() {
		return false
	}
	for _, p := range cfg.Policies {
		if p.Matches(i) && p.RepeatMinutes > 0 && now.Sub(i.LastNotified) >= time.Duration(p.RepeatMinutes)*time.Minute {
			return true
		}
	}
	return false
}
