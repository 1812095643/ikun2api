// Package incidents owns incident lifecycle. Acknowledgement, silence and health
// are independent; only explicit, fresh evidence can close an incident.
package incidents

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/detectors"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/ownership"
	"github.com/MACOS-DO/sub4api/internal/monitoring/store"
	"strings"
	"sync"
	"time"
)

type Manager struct {
	OwnershipPath string
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
	if isNew {
		if f.Healthy {
			return nil
		}
		i = &domain.Incident{GroupID: f.GroupID, Model: f.Model, ID: incidentID(key(f)), Fingerprint: key(f), Rule: f.Rule, RuleVersion: f.RuleVersion, Scope: f.Scope, FirstSeen: f.At, Status: "candidate", Severity: f.Severity, EpisodeStart: f.At}
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
	if f.Rule == "S01" && !f.Healthy {
		never, servedErr := m.neverServed(ctx, f.GroupID, f.Model, f.At)
		if servedErr != nil {
			return servedErr
		}
		i.DigestOnly = never
	}
	if f.ParentFingerprint != "" {
		i.ParentID = incidentID(fmt.Sprintf("%s:v%d", f.ParentFingerprint, f.RuleVersion))
	} else if !f.Healthy {
		var spec *rollupSpec
		// Signature incidents already span accounts; splitting them per account would duplicate cards.
		if gated(*i) && f.AccountID > 0 && !strings.HasPrefix(f.Scope, "signature:") {
			r := accountRollup(f)
			spec = &r
		} else if f.Rule == "S01" && f.GroupID > 0 && !i.DigestOnly {
			r := groupRollup(f)
			spec = &r
		}
		if spec != nil {
			parent, rollupErr := m.applyRollup(ctx, f, cfg, now, *spec)
			if rollupErr != nil {
				return rollupErr
			}
			i.ParentID = parent
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
		// Delayed evidence is counted but must not undo a later proven recovery.
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
	// Recovery is announced only after a quiet period; Tick posts it later.
	if kind == "resolved" && now.Sub(i.LastFault) < quietBeforeResolve {
		send = false
	}
	if now.Before(i.SilencedUntil) && kind != "escalated" {
		send = false
	}
	if rolledUp(*i) && !(kind == "resolved" && carded(*i)) {
		send = false // The rollup card reports members; only a legacy own card is closed.
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
func route(i domain.Incident, kind string, cfg domain.Config, now time.Time, forceNew bool) []domain.Delivery {
	if i.Rule == "L02" {
		return nil
	}
	if cfg.Owner(i.Rule, i.Scope, i.GroupID, i.Model) != "monitor" || i.ParentID != "" {
		return nil
	}
	ds := []domain.Delivery{}
	add := func(channel, target string) {
		state := "pending"
		if cfg.Shadow {
			state = "shadow"
		}
		id := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%s:%s:%s", i.ID, i.Revision, channel, target, kind))))
		ds = append(ds, domain.Delivery{ForceNew: forceNew, ID: id, IncidentID: i.ID, Revision: i.Revision, Channel: channel, Target: target, Kind: kind, State: state, NextAttempt: now})
	}
	for _, target := range cfg.FeishuTargets {
		add("feishu", target)
	}
	if emailWorthy(i, kind, forceNew) {
		for _, target := range cfg.EmailTargets {
			add("email", target)
		}
	}
	return ds
}

// Email is a backup for serious problems only: P0/P1 new faults (including a
// new episode), escalations and recoveries. Silent card edits never email.
func emailWorthy(i domain.Incident, kind string, forceNew bool) bool {
	if i.Severity == "" || i.Severity > "P1" || i.Scope == "digest" {
		return false
	}
	switch kind {
	case "firing", "escalated", "resolved":
		return true
	}
	return forceNew
}
func (m *Manager) Tick(ctx context.Context, cfg domain.Config, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.checkConfig(ctx, cfg); err != nil {
		return err
	}
	if m.OwnershipPath != "" {
		if err := ownership.Write(m.OwnershipPath, ownership.Project(cfg)); err != nil {
			return err
		}
	}
	rows, err := m.Store.ActiveIncidents(ctx)
	if err != nil {
		return err
	}
	for _, i := range rows {
		if i.Rule == "S03" && strings.HasPrefix(i.Scope, "proxy:") {
			children, readErr := m.Store.Children(ctx, i.ID)
			if readErr != nil {
				return readErr
			}
			recovered := len(children) > 0
			for _, child := range children {
				if child.Status != "resolved" {
					recovered = false
				}
			}
			if recovered {
				i.Status = "resolved"
				i.Revision++
				i.LastSeen = now
				i.Explanation.Facts = append(i.Explanation.Facts, "已关联的全部账号隔离事件均有明确恢复证据")
				ds := route(i, "resolved", cfg, now, false)
				if now.Before(i.SilencedUntil) {
					ds = nil
				}
				if err = m.Store.SaveIncident(ctx, i, ds); err != nil {
					return err
				}
				continue
			}
		}
		if i.Rule == "L02" || i.DigestOnly || now.Before(i.SilencedUntil) || i.Status == "resolved" || !cfg.Rule(i.Rule).Enabled {
			continue
		}
		// A rollup has no healthy samples of its own: it recovers after a quiet period.
		if i.Rollup && now.Sub(i.LastFault) >= quietBeforeResolve {
			i.Status = "resolved"
			i.ResolvedAt = now
			i.Revision++
			var ds []domain.Delivery
			if carded(i) {
				ds = route(i, "resolved", cfg, now, false)
				if len(ds) > 0 {
					i.LastNotified = now
				}
			}
			if err = m.Store.SaveIncident(ctx, i, ds); err != nil {
				return err
			}
			continue
		}
		kind, forceNew := "update", false
		if !carded(i) {
			if i.Status != "firing" || rolledUp(i) || (gated(i) && !thresholdMet(i)) {
				continue
			}
			kind, forceNew = "firing", !i.LastNotified.IsZero()
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
	// Recoveries held back by quietBeforeResolve are announced once the quiet period passes.
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
	return m.dailyDigest(ctx, cfg, now)
}
func (m *Manager) Control(ctx context.Context, id, action, actor, requestID string, revision int, until time.Time, reason string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Store.ControlIncident(ctx, id, action, actor, requestID, revision, until, reason, now)
}

// Bounded acceleration only: SQLite remains authoritative across restarts.
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
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := c.Validate(); err != nil {
		return err
	}
	for _, d := range detectors.Catalog {
		if _, ok := c.Rules[d.ID]; !ok {
			c.Rules[d.ID] = c.Rule(d.ID)
		}
	}
	old, readErr := m.Store.Config(ctx)
	if readErr != nil {
		return readErr
	}
	if m.OwnershipPath != "" {
		if err := ownership.Write(m.OwnershipPath, ownership.Union(ownership.Project(old), ownership.Project(c))); err != nil {
			return err
		}
	}
	err := m.Store.SaveConfig(ctx, c, expected)
	m.configVersion = nil
	if m.OwnershipPath != "" {
		current, readErr := m.Store.Config(ctx)
		if readErr != nil {
			return readErr
		}
		if publishErr := ownership.Write(m.OwnershipPath, ownership.Project(current)); publishErr != nil {
			return publishErr
		}
	}

	if err == nil {
		m.seen = nil
		m.order = nil
		m.next = 0
	}
	return err
}

// All runtime configuration writes pass through this manager. The cached version
// is invalidated on every write attempt; stale evaluator batches must retry
// without advancing their durable checkpoint.
func (m *Manager) checkConfig(ctx context.Context, c domain.Config) error {
	if m.configVersion == nil {
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
