package incidents

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/detectors"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"sort"
	"time"
)

const maxMembers = 20

func incidentID(fingerprint string) string {
	return fmt.Sprintf("inc-%x", sha256.Sum256([]byte(fingerprint)))
}

type rollupSpec struct {
	fingerprint, scope string
	groupID            int64
	byGroup            bool
}

// One card per upstream account for all gated rules and models.
func accountRollup(f domain.Finding) rollupSpec {
	return rollupSpec{fingerprint: fmt.Sprintf("rollup:account:%d", f.AccountID), scope: fmt.Sprintf("account:%d", f.AccountID)}
}

// One S01 card per group for all models that suddenly lost candidates.
func groupRollup(f domain.Finding) rollupSpec {
	return rollupSpec{fingerprint: fmt.Sprintf("rollup:group:%d:S01", f.GroupID), scope: fmt.Sprintf("group:%d", f.GroupID), groupID: f.GroupID, byGroup: true}
}

// applyRollup records a member fault on its rollup incident and returns the
// rollup ID, which becomes the member's parent (members never card themselves).
// It runs before the member write so a crash between the two cannot lose rollup evidence.
func (m *Manager) applyRollup(ctx context.Context, f domain.Finding, cfg domain.Config, now time.Time, spec rollupSpec) (string, error) {
	id := incidentID(spec.fingerprint)
	r, err := m.Store.Incident(ctx, spec.fingerprint)
	if err != nil {
		return "", err
	}
	isNew := r == nil
	if isNew {
		r = &domain.Incident{ID: id, Fingerprint: spec.fingerprint, Rule: f.Rule, RuleVersion: f.RuleVersion, Scope: spec.scope, GroupID: spec.groupID, FirstSeen: f.At, Status: "candidate", Severity: f.Severity, Rollup: true, EpisodeStart: f.At}
	}
	// Different rules may cite the same request; each still counts as its own member.
	evidence := f.Rule + ":" + f.EvidenceID
	exists, err := m.Store.EvidenceExists(ctx, id, evidence)
	if err != nil {
		return "", err
	}
	if exists {
		if f.Severity >= r.Severity {
			return id, nil
		}
		evidence += ":severity:" + f.Severity
	}
	oldStatus, oldSeverity := r.Status, r.Severity
	if !isNew && startsEpisode(*r, oldStatus, r.LastFault, f.At) {
		r.EpisodeStart = f.At
		r.Members = nil
	}
	r.Rule, r.RuleVersion = f.Rule, f.RuleVersion
	if !exists {
		r.Count++
		addHit(r, f)
		addMember(r, f)
	}
	if f.At.Before(r.FirstSeen) {
		r.FirstSeen = f.At
	}
	if !f.At.Before(r.LastFault) {
		r.Sample = &domain.IncidentSample{RequestID: f.RequestID, UserID: f.UserID, KeyID: f.KeyID, AccountID: f.AccountID, GroupID: f.GroupID, At: f.At, RequestStartedAt: f.RequestStartedAt, FirstOutputAt: f.FirstOutputAt, RequestEndedAt: f.RequestEndedAt, RequestOutcome: f.RequestOutcome}
		r.LastFault = f.At
		r.Status = "firing"
		if oldStatus == "resolved" {
			r.Acknowledged = false
			r.SilencedUntil = time.Time{}
			r.Severity = f.Severity
		}
	}
	if f.Severity < r.Severity {
		r.Severity = f.Severity
	}
	if f.At.After(r.LastSeen) {
		r.LastSeen = f.At
	}
	r.Quality = f.Quality
	r.Explanation = rollupExplanation(*r, f, spec)
	r.Revision++
	kind, send, forceNew := notifyKind(*r, oldStatus, oldSeverity, false, false, now)
	if now.Before(r.SilencedUntil) && kind != "escalated" {
		send = false
	}
	var ds []domain.Delivery
	if send {
		ds = route(*r, kind, cfg, now, forceNew)
		if len(ds) > 0 {
			r.LastNotified = now
			r.NotifiedCount = r.Count
		}
	}
	if err = m.Store.SaveFinding(ctx, *r, ds, evidence, &f); err != nil {
		return "", err
	}
	return id, nil
}

func addMember(r *domain.Incident, f domain.Finding) {
	for n := range r.Members {
		if r.Members[n].Rule == f.Rule && r.Members[n].Model == f.Model {
			r.Members[n].Count++
			if f.At.After(r.Members[n].Last) {
				r.Members[n].Last = f.At
			}
			return
		}
	}
	r.Members = append(r.Members, domain.Member{Rule: f.Rule, Model: f.Model, Count: 1, Last: f.At})
	if len(r.Members) > maxMembers {
		sort.SliceStable(r.Members, func(a, b int) bool { return r.Members[a].Last.After(r.Members[b].Last) })
		r.Members = r.Members[:maxMembers]
	}
}

func rollupExplanation(r domain.Incident, latest domain.Finding, spec rollupSpec) domain.Explanation {
	members := append([]domain.Member(nil), r.Members...)
	sort.SliceStable(members, func(a, b int) bool { return members[a].Count > members[b].Count })
	rules, models := map[string]bool{}, map[string]bool{}
	for _, mb := range members {
		rules[mb.Rule] = true
		models[mb.Model] = true
	}
	e := domain.Explanation{Confidence: latest.Explanation.Confidence, Inferences: []string{}}
	if spec.byGroup {
		e.Conclusion = fmt.Sprintf("分组 #%d 突然无可用候选账号（%d 个模型）", spec.groupID, len(models))
	} else {
		e.Conclusion = fmt.Sprintf("上游账号 #%d 异常汇总（%d 类问题，%d 个模型）", latest.AccountID, len(rules), len(models))
	}
	for _, mb := range members {
		e.Facts = append(e.Facts, fmt.Sprintf("%s %s · 模型 %s · 本轮命中 %d 次", mb.Rule, detectors.DefinitionFor(mb.Rule).Title, mb.Model, mb.Count))
	}
	for _, fact := range latest.Explanation.Facts {
		e.Facts = append(e.Facts, "最近一条（"+latest.Rule+"）："+fact)
	}
	e.Missing = latest.Explanation.Missing
	advice := map[string]bool{}
	for _, mb := range members {
		a := detectors.DefinitionFor(mb.Rule).Advice
		if a != "" && !advice[a] {
			advice[a] = true
			e.Advice = append(e.Advice, a)
		}
	}
	return e
}
