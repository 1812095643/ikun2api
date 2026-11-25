// 事件只接受可信观测；确认、静默与恢复相互独立。
package incidents

import (
	"context"
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/detectors"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/store"
	"github.com/google/uuid"
	"strings"
	"sync"
	"time"
)

type Manager struct {
	configVersion *int
	Store         *store.Store
	mu            sync.Mutex
	seen          map[string]struct{}
	order         []string
	next          int
}

func key(f domain.Finding) string { return fmt.Sprintf("%s:v%d", f.Fingerprint, f.RuleVersion) }
func (m *Manager) Apply(ctx context.Context, f domain.Finding, cfg domain.Config, now time.Time) (err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err = m.checkConfig(ctx, cfg); err != nil {
		return err
	}
	if f.RuleVersion != cfg.Rule(f.Rule).Version {
		return nil
	}
	if f.Historical || f.Quality == domain.Stale || f.Quality == domain.Unsupported || !cfg.Rule(f.Rule).Enabled {
		return nil
	}
	cacheKey := key(f) + ":" + f.EvidenceID + ":" + f.Severity
	if _, ok := m.seen[cacheKey]; ok {
		return nil
	}
	defer func() {
		if err == nil {
			m.remember(cacheKey)
		}
	}()
	i, err := m.Store.Incident(ctx, key(f))
	if err != nil {
		return err
	}
	isNew := i == nil
	if i != nil && !f.Healthy && (i.Status == "resolved" || i.Status == "closed") {
		if !f.At.After(i.ResolvedAt) {
			return nil
		}
		i = nil
		isNew = true
	}
	if isNew {
		if f.Healthy {
			return nil
		}
		i = &domain.Incident{Platform: f.Platform, GroupID: f.GroupID, Model: f.Model, ID: uuid.NewString(), Fingerprint: key(f), Rule: f.Rule, RuleVersion: f.RuleVersion, Scope: f.Scope, FirstSeen: f.At, Status: "candidate", Severity: f.Severity, EpisodeStart: f.At}
	}
	exists, err := m.Store.EvidenceExists(ctx, i.ID, f.EvidenceID)
	if err != nil {
		return err
	}
	reclassified := exists && !f.Healthy && f.Severity < i.Severity
	if exists && !reclassified {
		return nil
	}
	if reclassified {
		f.EvidenceID += ":severity:" + f.Severity
	}
	if f.ParentFingerprint != "" {
		parent, readErr := m.Store.Incident(ctx, fmt.Sprintf("%s:v%d", f.ParentFingerprint, f.RuleVersion))
		if readErr != nil {
			return readErr
		}
		if parent != nil {
			i.ParentID = parent.ID
		}
	}
	oldStatus, oldSeverity := i.Status, i.Severity
	accountAppeared := f.Rule == "L02" && !f.Healthy && i.Sample != nil && f.RequestID != "" && i.Sample.RequestID == f.RequestID && i.Sample.AccountID == 0 && f.AccountID > 0
	if f.Healthy {
		if f.Quality != domain.Fresh || i.Status == "resolved" || f.At.Before(i.LastFault) || f.At.Before(i.LastHealthy) {
			return nil
		}
		if f.Rule == "L02" && f.RequestID != "" && ((i.Sample != nil && i.Sample.RequestID == f.RequestID) || ((i.Sample == nil || i.Sample.RequestID == "") && strings.HasSuffix(i.Scope, ":"+f.RequestID))) {
			if i.Sample == nil {
				i.Sample = &domain.IncidentSample{RequestID: f.RequestID}
			}
			i.Sample.RequestID = f.RequestID
			if f.UserID > 0 {
				i.Sample.UserID = f.UserID
			}
			if f.KeyID > 0 {
				i.Sample.KeyID = f.KeyID
			}
			if f.AccountID > 0 {
				i.Sample.AccountID = f.AccountID
			}
			if f.GroupID > 0 {
				i.Sample.GroupID = f.GroupID
			}
			if !f.RequestStartedAt.IsZero() {
				i.Sample.RequestStartedAt = f.RequestStartedAt
			}
			if !f.FirstOutputAt.IsZero() {
				i.Sample.FirstOutputAt = f.FirstOutputAt
			}
			if !f.RequestEndedAt.IsZero() {
				i.Sample.RequestEndedAt = f.RequestEndedAt
				i.Sample.RequestOutcome = f.RequestOutcome
			}
		}
		if i.RecoveringSince.IsZero() || (!i.LastHealthy.IsZero() && f.At.Sub(i.LastHealthy) > 2*time.Minute) {
			i.RecoveringSince = f.At
			i.HealthySamples = 0
		}
		i.LastHealthy = f.At
		i.HealthySamples++
		i.Status = "recovering"
		d := detectors.DefinitionFor(f.Rule)
		if i.HealthySamples >= d.RecoverySamples && f.At.Sub(i.RecoveringSince) >= time.Duration(d.RecoverySeconds)*time.Second {
			i.Status = "resolved"
			i.ResolvedAt = f.At
		}
	} else {
		if !reclassified {
			i.Count++
			addHit(i, f)
		}
		if !f.At.Before(i.LastHealthy) || i.Status != "resolved" {
			if f.At.Before(i.FirstSeen) {
				i.FirstSeen = f.At
			}
			if !f.At.Before(i.LastFault) {
				if !isNew && startsEpisode(*i, oldStatus, i.LastFault, f.At) {
					i.EpisodeStart = f.At
				}
				i.Sample = &domain.IncidentSample{RequestID: f.RequestID, UserID: f.UserID, KeyID: f.KeyID, AccountID: f.AccountID, GroupID: f.GroupID, At: f.At, RequestStartedAt: f.RequestStartedAt, FirstOutputAt: f.FirstOutputAt, RequestEndedAt: f.RequestEndedAt, RequestOutcome: f.RequestOutcome}
				i.LastFault = f.At
				i.Status = "firing"
				i.RecoveringSince = time.Time{}
				i.HealthySamples = 0
			}
			if oldStatus == "resolved" {
				i.ClosedBy = ""
				i.Acknowledged = false
				i.SilencedUntil = time.Time{}
				i.Severity = f.Severity
			}
			if f.Severity < i.Severity {
				i.Severity = f.Severity
			}
		}
	}
	if f.At.After(i.LastSeen) {
		i.LastSeen = f.At
	}
	if !f.Healthy || i.Status == "resolved" {
		i.Explanation = f.Explanation
	}
	i.Quality = f.Quality
	i.Revision++
	kind, send, forceNew := notifyKind(*i, oldStatus, oldSeverity, f.Healthy, accountAppeared, now)
	if kind == "resolved" && now.Sub(i.LastFault) < quietBeforeResolve {
		send = false
	}
	if now.Before(i.SilencedUntil) {
		send = false
	}

	if i.DigestOnly {
		send = false // Never-served group/models are reported in the daily digest.
	}
	var ds []domain.Delivery
	if send {
		ds = route(*i, kind, cfg, now, forceNew)
		if len(ds) > 0 {
			i.LastNotified = now
			i.NotifiedCount = i.Count
		}
	}
	return m.Store.SaveFinding(ctx, *i, ds, f.EvidenceID, &f)
}
func (m *Manager) Tick(ctx context.Context, cfg domain.Config, now time.Time, sourceComplete ...bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.checkConfig(ctx, cfg); err != nil {
		return err
	}
	rows, err := m.Store.ActiveIncidents(ctx)
	if err != nil {
		return err
	}
	for _, i := range rows {
		// 代理事件只依据已关联账号的明确恢复结果收尾；缺采集时继续保留。
		if i.Rule == "S03" && strings.HasPrefix(i.Scope, "proxy:") && (len(sourceComplete) == 0 || sourceComplete[0]) {
			children, readErr := m.Store.Children(ctx, i.ID)
			if readErr != nil {
				return readErr
			}
			healthy := len(children) > 0
			for _, child := range children {
				if child.Status != "resolved" || child.ClosedBy != "" {
					healthy = false
				}
			}
			if healthy {
				i.Status = "resolved"
				i.ResolvedAt = now
				i.LastSeen = now
				i.Revision++
				i.Explanation.Facts = append(i.Explanation.Facts, "已关联账号均通过明确恢复检查")
				ds := route(i, "resolved", cfg, now, false)
				if err = m.Store.SaveIncident(ctx, i, ds); err != nil {
					return err
				}
				continue
			}
		}
		if i.Rule == "L02" || i.DigestOnly || now.Before(i.SilencedUntil) || i.Status == "resolved" || !cfg.Rule(i.Rule).Enabled {
			continue
		}
		kind, forceNew := "update", false
		if !carded(i) {
			if i.Status != "firing" || rolledUp(i) || (gated(i) && !thresholdMet(i)) {
				continue
			}
			kind, forceNew = "firing", !i.LastNotified.IsZero()
		} else if reminderDue(i, cfg, now) {
			kind = "reminder"
		} else if !routineUpdate(i, false, now) {
			continue
		}
		i.Revision++
		ds := route(i, kind, cfg, now, forceNew)
		if len(ds) == 0 {
			continue
		}
		i.LastNotified = now
		i.NotifiedCount = i.Count
		if err = m.Store.SaveIncident(ctx, i, ds); err != nil {
			return err
		}
	}
	pending, err := m.Store.RecentlyResolved(ctx, now.Add(-24*time.Hour))
	if err != nil {
		return err
	}
	for _, i := range pending {
		if !carded(i) || !i.LastNotified.Before(i.ResolvedAt) || now.Sub(i.LastFault) < quietBeforeResolve || now.Before(i.SilencedUntil) || !cfg.Rule(i.Rule).Enabled {
			continue
		}
		if i.DigestOnly || (rolledUp(i) && i.LastNotified.IsZero()) {
			continue
		}
		i.Revision++
		ds := route(i, "resolved", cfg, now, false)
		if len(ds) == 0 {
			continue
		}
		i.LastNotified = now
		if err = m.Store.SaveIncident(ctx, i, ds); err != nil {
			return err
		}
	}
	return nil
}
func (m *Manager) Control(ctx context.Context, id, action, actor, requestID string, revision int, until time.Time, reason string, now time.Time) error {
	return m.Store.ControlIncident(ctx, id, action, actor, requestID, revision, until, reason, now)
}

func (m *Manager) remember(key string) {
	if m.seen == nil {
		m.seen = map[string]struct{}{}
	}
	if _, ok := m.seen[key]; ok {
		return
	}
	if len(m.order) < 50000 {
		m.order = append(m.order, key)
	} else {
		delete(m.seen, m.order[m.next])
		m.order[m.next] = key
		m.next = (m.next + 1) % len(m.order)
	}
	m.seen[key] = struct{}{}
}

func (m *Manager) Configure(ctx context.Context, c domain.Config, expected int) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if c.Rules == nil {
		c.Rules = map[string]domain.RuleConfig{}
	}
	for _, d := range detectors.Catalog {
		if _, ok := c.Rules[d.ID]; !ok {
			c.Rules[d.ID] = c.Rule(d.ID)
		}
	}
	err := m.Store.SaveConfig(ctx, c, expected)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.configVersion = nil

	if err == nil {
		m.seen = nil
		m.order = nil
		m.next = 0
	}
	return err
}

func (m *Manager) checkConfig(ctx context.Context, c domain.Config) error {
	if m.configVersion == nil || c.Version != *m.configVersion {
		current, err := m.Store.Config(ctx)
		if err != nil {
			return err
		}
		v := current.Version
		m.configVersion = &v
	}
	if c.Version != *m.configVersion {
		return store.ErrConflict
	}
	return nil
}
