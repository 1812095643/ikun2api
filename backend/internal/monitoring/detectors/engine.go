package detectors

import (
	"crypto/sha256"
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"sort"
	"strconv"
	"strings"
	"time"
)

type evaluation struct {
	signatures     map[string]domain.Observation
	now            time.Time
	cfg            domain.Config
	rows           []domain.Observation
	findings       []domain.Finding
	ends           map[string]domain.Observation
	prior          []domain.Incident
	recovery       map[string]bool
	latestPayments map[string]domain.Observation
}

func Evaluate(rows []domain.Observation, now time.Time, cfg domain.Config, prior ...domain.Incident) []domain.Finding {
	e := &evaluation{signatures: map[string]domain.Observation{}, latestPayments: map[string]domain.Observation{}, recovery: map[string]bool{}, prior: prior, now: now, cfg: cfg, rows: append([]domain.Observation(nil), rows...), ends: map[string]domain.Observation{}, findings: []domain.Finding{}}
	for _, i := range prior {
		e.recovery[strings.TrimSuffix(i.Fingerprint, fmt.Sprintf(":v%d", i.RuleVersion))] = true
	}
	sort.SliceStable(e.rows, func(i, j int) bool { return e.rows[i].OccurredAt.Before(e.rows[j].OccurredAt) })
	for _, o := range e.rows {
		if o.Reason == "thinking_signature_invalid" && o.Quality != domain.Stale && o.Quality != domain.Unsupported {
			k := signatureRequestKey(o)
			if prior, ok := e.signatures[k]; !ok || prior.Source != "gateway" {
				e.signatures[k] = o
			}
		}
		if o.Kind == "payment.snapshot" && fresh(o) {
			e.latestPayments[o.Site+":"+taskScope(o)] = o
		}
		if o.Kind == "request.finished" || o.Kind == "request.cancelled" {
			e.ends[requestKey(o)] = o
		}
	}
	for _, o := range e.rows {
		if o.Quality == domain.Stale || o.Quality == domain.Unsupported {
			continue
		}
		e.quota(o)
		e.request(o)
		e.system(o)
	}
	e.pools()
	e.latency()
	e.stalls()
	e.tasks()
	e.cancellations()
	e.healthySamples()
	e.escalateImpact()
	sort.SliceStable(e.findings, func(i, j int) bool { return e.findings[i].At.Before(e.findings[j].At) })
	return e.findings
}
func requestKey(o domain.Observation) string { return o.Site + ":" + o.RequestID }
func cohort(o domain.Observation) string {
	if o.ChannelID > 0 {
		return fmt.Sprintf("channel:%d/model:%s", o.ChannelID, o.Model)
	}
	return fmt.Sprintf("account:%d/group:%d/model:%s", o.AccountID, o.GroupID, o.Model)
}
func poolScope(o domain.Observation) string {
	return fmt.Sprintf("group:%d/model:%s", o.GroupID, o.Model)
}
func taskScope(o domain.Observation) string {
	return fmt.Sprintf("task:%s/user:%d/key:%d", o.TaskID, o.UserID, o.KeyID)
}
func (e *evaluation) add(rule string, o domain.Observation, scope, cause, severity string, healthy bool, facts, missing []string) *domain.Finding {
	if !e.cfg.Rule(rule).Enabled || rule == "Q04" {
		return nil
	}
	d := DefinitionFor(rule)
	v := e.cfg.Rule(rule).Version
	if v < 1 {
		v = 1
	}
	fp := fmt.Sprintf("%x", sha256.Sum256([]byte(o.Site+"|"+rule+"|"+scope+"|"+cause)))
	if healthy && !e.recovery[fp] {
		return nil
	}
	if !healthy {
		e.recovery[fp] = true
	}
	f := domain.Finding{Platform: o.Platform, GroupID: o.GroupID, Model: o.Model, AccountID: o.AccountID, RequestID: o.RequestID, UserID: o.UserID, KeyID: o.KeyID, Rule: rule, RuleVersion: v, Fingerprint: fp, Scope: scope, Severity: severity, At: o.OccurredAt, EvidenceID: fmt.Sprintf("%s:%s:%s:%s:%t", o.Source, o.EventID, rule, cause, healthy), Healthy: healthy, Quality: o.Quality, Historical: o.Quality == domain.Historical, Explanation: domain.Explanation{Conclusion: d.Title, Confidence: "high", Facts: facts, Missing: missing, Advice: []string{d.Advice}, Inferences: []string{}}}
	if o.Source == "gateway" && o.RequestID != "" && (o.Kind == "request.active" || o.Kind == "request.finished" || o.Kind == "request.cancelled") && o.Has("elapsed_ms") && o.N("elapsed_ms") >= 0 && o.N("elapsed_ms") <= float64((7*24*time.Hour)/time.Millisecond) {
		f.RequestStartedAt = o.OccurredAt.Add(-time.Duration(o.N("elapsed_ms")) * time.Millisecond)
		if o.Has("first_effective_ms") && o.N("first_effective_ms") >= 0 && o.N("first_effective_ms") <= o.N("elapsed_ms") {
			f.FirstOutputAt = f.RequestStartedAt.Add(time.Duration(o.N("first_effective_ms")) * time.Millisecond)
		}
		if o.Kind == "request.finished" || o.Kind == "request.cancelled" {
			f.RequestEndedAt = o.OccurredAt
			f.RequestOutcome = o.Outcome
		}
	}
	if len(missing) > 0 {
		f.Explanation.Confidence = "medium"
	}
	e.findings = append(e.findings, f)
	return &e.findings[len(e.findings)-1]
}
func (e *evaluation) timed(o domain.Observation, suffix string) domain.Observation {
	o.EventID += ":" + suffix + ":" + strconv.FormatInt(e.now.Unix()/30, 10)
	o.OccurredAt = e.now
	return o
}
func fresh(o domain.Observation) bool { return o.Quality == domain.Fresh }
func (e *evaluation) healthySamples() {
	failures := map[string]map[string]domain.Finding{}
	seeds := append([]domain.Finding(nil), e.findings...)
	for _, i := range e.prior {
		fp := strings.TrimSuffix(i.Fingerprint, fmt.Sprintf(":v%d", i.RuleVersion))
		seeds = append(seeds, domain.Finding{Rule: i.Rule, RuleVersion: i.RuleVersion, Fingerprint: fp, Scope: i.Scope, Severity: i.Severity, At: i.LastFault, Explanation: i.Explanation})
	}
	for _, f := range seeds {
		if f.Healthy {
			continue
		}
		if failures[f.Scope] == nil {
			failures[f.Scope] = map[string]domain.Finding{}
		}
		if _, exists := failures[f.Scope][f.Fingerprint]; !exists {
			failures[f.Scope][f.Fingerprint] = f
		}
	}
	for _, o := range e.rows {
		if o.Kind != "request.finished" || o.Outcome != "success" || !fresh(o) || o.N("terminal") != 1 {
			continue
		}
		for _, f := range failures[cohort(o)] {
			if f.Healthy || f.Scope != cohort(o) || !o.OccurredAt.After(f.At) {
				continue
			}
			switch f.Rule {
			case "U01", "U02", "U03", "F01", "F02", "Q05":
			default:
				continue
			}
			h := f
			h.At = o.OccurredAt
			h.EvidenceID = o.Source + ":" + o.EventID + ":" + f.Rule + ":healthy"
			h.Healthy = true
			h.Quality = domain.Fresh
			h.Historical = false
			h.Explanation.Facts = []string{"同一账号/渠道与模型出现明确成功终态；仍需连续健康窗口"}
			e.findings = append(e.findings, h)
		}
	}
}
