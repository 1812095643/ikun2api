package domain

import (
	"errors"
	"regexp"
	"time"
)

const SchemaVersion = 1

type Quality string

const (
	Fresh       Quality = "fresh"
	Stale       Quality = "stale"
	Unsupported Quality = "unsupported"
	Partial     Quality = "partial"
	Historical  Quality = "historical"
)

type Observation struct {
	Schema         int                `json:"schema_version"`
	Site           string             `json:"site_id"`
	Instance       string             `json:"instance_id,omitempty"`
	Source         string             `json:"source_id"`
	EventID        string             `json:"source_event_id"`
	BootID         string             `json:"boot_id,omitempty"`
	Kind           string             `json:"kind"`
	OccurredAt     time.Time          `json:"occurred_at"`
	ObservedAt     time.Time          `json:"observed_at"`
	RequestID      string             `json:"request_id,omitempty"`
	AttemptID      string             `json:"attempt_id,omitempty"`
	TaskID         string             `json:"task_id,omitempty"`
	Platform       string             `json:"platform,omitempty"`
	Model          string             `json:"model,omitempty"`
	GroupID        int64              `json:"group_id,omitempty"`
	AccountID      int64              `json:"account_id,omitempty"`
	ChannelID      int64              `json:"channel_id,omitempty"`
	UserID         int64              `json:"user_id,omitempty"`
	KeyID          int64              `json:"key_id,omitempty"`
	Reason         string             `json:"reason_code,omitempty"`
	Outcome        string             `json:"outcome,omitempty"`
	ClientStatus   int                `json:"client_http_status,omitempty"`
	UpstreamStatus int                `json:"upstream_status,omitempty"`
	Quality        Quality            `json:"source_quality"`
	Metrics        map[string]float64 `json:"metrics,omitempty"`
}

var safeID = regexp.MustCompile(`^[a-zA-Z0-9_.:/@+\-]{0,256}$`)
var safeCode = regexp.MustCompile(`^[a-zA-Z0-9_.\-]{0,96}$`)

func (o Observation) Validate() error {
	if o.Schema != SchemaVersion {
		return errors.New("unsupported observation schema")
	}
	if o.Source == "" || o.EventID == "" || o.Site == "" || o.Kind == "" || o.OccurredAt.IsZero() {
		return errors.New("incomplete observation")
	}
	for _, s := range []string{o.Site, o.Instance, o.Source, o.EventID, o.BootID, o.RequestID, o.AttemptID, o.TaskID, o.Platform, o.Model} {
		if !safeID.MatchString(s) {
			return errors.New("unsafe observation identifier")
		}
	}
	for _, s := range []string{o.Kind, o.Reason, o.Outcome} {
		if !safeCode.MatchString(s) {
			return errors.New("unsafe observation enum")
		}
	}
	if len(o.Metrics) > 64 {
		return errors.New("observation metrics limit")
	}
	for k := range o.Metrics {
		if !safeCode.MatchString(k) {
			return errors.New("unsafe metric name")
		}
	}
	switch o.Quality {
	case Fresh, Stale, Partial, Unsupported, Historical:
	default:
		return errors.New("invalid source quality")
	}
	return nil
}
func (o Observation) Has(k string) bool  { _, ok := o.Metrics[k]; return ok }
func (o Observation) N(k string) float64 { return o.Metrics[k] }

type Cursor struct {
	Source      string    `json:"source_id"`
	Watermark   string    `json:"watermark"`
	BootID      string    `json:"boot_id"`
	LastSuccess time.Time `json:"last_success"`
	Quality     Quality   `json:"quality"`
	Dropped     uint64    `json:"dropped"`
}
type Capability struct {
	Name    string  `json:"name"`
	Quality Quality `json:"quality"`
	Reason  string  `json:"reason,omitempty"`
}
type Snapshot struct {
	Schema       int           `json:"observation_schema_version"`
	Build        string        `json:"gateway_build"`
	Site         string        `json:"site_id"`
	Instance     string        `json:"instance_id"`
	BootID       string        `json:"boot_id"`
	Timing       string        `json:"timing_semantics"`
	Capabilities []Capability  `json:"source_capabilities"`
	Events       []Observation `json:"events"`
	Active       []Observation `json:"active"`
	Sequence     uint64        `json:"sequence"`
	Oldest       uint64        `json:"oldest_sequence"`
	Dropped      uint64        `json:"dropped"`
}
type Explanation struct {
	Conclusion string   `json:"conclusion"`
	Confidence string   `json:"confidence"`
	Facts      []string `json:"facts"`
	Inferences []string `json:"inferences"`
	Missing    []string `json:"missing_evidence"`
	Advice     []string `json:"advice"`
}
type Finding struct {
	Platform          string           `json:"platform,omitempty"`
	RelatedSamples    []IncidentSample `json:"related_samples,omitempty"`
	GroupID           int64            `json:"group_id,omitempty"`
	Model             string           `json:"model,omitempty"`
	AccountID         int64            `json:"account_id,omitempty"`
	ParentFingerprint string           `json:"parent_fingerprint,omitempty"`
	RequestID         string           `json:"request_id,omitempty"`
	UserID            int64            `json:"user_id,omitempty"`
	KeyID             int64            `json:"key_id,omitempty"`
	RequestStartedAt  time.Time        `json:"request_started_at,omitempty"`
	FirstOutputAt     time.Time        `json:"first_output_at,omitempty"`
	RequestEndedAt    time.Time        `json:"request_ended_at,omitempty"`
	RequestOutcome    string           `json:"request_outcome,omitempty"`

	Rule        string      `json:"rule_id"`
	RuleVersion int         `json:"rule_version"`
	Fingerprint string      `json:"fingerprint"`
	Severity    string      `json:"severity"`
	At          time.Time   `json:"at"`
	EvidenceID  string      `json:"evidence_id"`
	Scope       string      `json:"scope"`
	Explanation Explanation `json:"explanation"`
	Healthy     bool        `json:"healthy"`
	Quality     Quality     `json:"quality"`
	Historical  bool        `json:"historical"`
}

type IncidentSample struct {
	RequestID        string    `json:"request_id,omitempty"`
	RequestStartedAt time.Time `json:"request_started_at,omitempty"`
	FirstOutputAt    time.Time `json:"first_output_at,omitempty"`
	RequestEndedAt   time.Time `json:"request_ended_at,omitempty"`
	RequestOutcome   string    `json:"request_outcome,omitempty"`
	UserID           int64     `json:"user_id,omitempty"`
	KeyID            int64     `json:"key_id,omitempty"`
	AccountID        int64     `json:"account_id,omitempty"`
	GroupID          int64     `json:"group_id,omitempty"`
	At               time.Time `json:"at"`
}
type Incident struct {
	LegacyVariables map[string]string `json:"legacy_variables,omitempty"`
	LegacyEventID   int64             `json:"legacy_event_id,omitempty"`
	UpdatedCursor   int64             `json:"updated_cursor,omitempty"`
	Platform        string            `json:"platform,omitempty"`
	DisplayScope    string            `json:"-"` // Transient notification labels, never persisted.
	Sample          *IncidentSample   `json:"sample,omitempty"`
	GroupID         int64             `json:"group_id,omitempty"`
	Model           string            `json:"model,omitempty"`
	ParentID        string            `json:"parent_id,omitempty"`
	AccountCount    int               `json:"account_count"`
	LastFault       time.Time         `json:"last_fault"`
	LastHealthy     time.Time         `json:"last_healthy"`
	LastNotified    time.Time         `json:"last_notified"`
	NotifiedCount   int               `json:"notified_count"`
	RequestCount    int               `json:"request_count"`
	UserCount       int               `json:"user_count"`
	LastActor       string            `json:"last_actor,omitempty"`
	ControlReason   string            `json:"control_reason,omitempty"`
	RecentHits      []RecentHit       `json:"recent_hits,omitempty"`
	EpisodeStart    time.Time         `json:"episode_start"`
	Rollup          bool              `json:"rollup,omitempty"`
	Members         []Member          `json:"members,omitempty"`
	ResolvedAt      time.Time         `json:"resolved_at"`
	ClosedBy        string            `json:"closed_by,omitempty"`
	DigestOnly      bool              `json:"digest_only,omitempty"`
	DigestedCount   int               `json:"digested_count,omitempty"`

	ID              string      `json:"id"`
	Fingerprint     string      `json:"fingerprint"`
	Rule            string      `json:"rule_id"`
	RuleVersion     int         `json:"rule_version"`
	Scope           string      `json:"scope"`
	Revision        int         `json:"revision"`
	Status          string      `json:"status"`
	Severity        string      `json:"severity"`
	FirstSeen       time.Time   `json:"first_seen"`
	LastSeen        time.Time   `json:"last_seen"`
	RecoveringSince time.Time   `json:"recovering_since"`
	HealthySamples  int         `json:"healthy_samples"`
	Count           int         `json:"count"`
	Acknowledged    bool        `json:"acknowledged"`
	SilencedUntil   time.Time   `json:"silenced_until"`
	Explanation     Explanation `json:"explanation"`
	Quality         Quality     `json:"quality"`
}
type Delivery struct {
	RecoveredLease    bool      `json:"-"`
	FallbackTargetIDs []string  `json:"fallback_target_ids,omitempty"`
	FallbackFor       string    `json:"fallback_for,omitempty"`
	MentionAll        bool      `json:"mention_all,omitempty"`
	ForceNew          bool      `json:"force_new_message,omitempty"`
	ID                string    `json:"id"`
	IncidentID        string    `json:"incident_id"`
	Revision          int       `json:"revision"`
	Channel           string    `json:"channel"`
	Target            string    `json:"target"`
	Kind              string    `json:"kind"`
	State             string    `json:"state"`
	Attempts          int       `json:"attempts"`
	NextAttempt       time.Time `json:"next_attempt"`
	LeaseUntil        time.Time `json:"lease_until"`
	LeaseToken        string    `json:"-"`
	MessageID         string    `json:"message_id,omitempty"`
	ErrorClass        string    `json:"error_class,omitempty"`
	AcceptedAt        time.Time `json:"accepted_at"`
	Payload           Incident  `json:"payload"`
}
type Profile struct {
	QueueWarnSeconds int `json:"queue_warn_seconds"`
	QueueFailSeconds int `json:"queue_fail_seconds"`
	FirstWarnSeconds int `json:"first_warn_seconds"`
	FirstFailSeconds int `json:"first_fail_seconds"`
	StallWarnSeconds int `json:"stall_warn_seconds"`
	StallFailSeconds int `json:"stall_fail_seconds"`
	TaskSLASeconds   int `json:"task_sla_seconds"`
}

func DefaultProfile() Profile { return Profile{10, 30, 30, 60, 30, 60, 0} }

type RuleConfig struct {
	Enabled bool   `json:"enabled"`
	Owner   string `json:"notification_owner"`
	Version int    `json:"version"`
}
type OwnerOverride struct {
	Rule    string `json:"rule_id"`
	Scope   string `json:"scope,omitempty"`
	GroupID *int64 `json:"group_id,omitempty"`
	Model   string `json:"model,omitempty"`
	Owner   string `json:"notification_owner"`
}
type LegacyHandoff struct {
	LegacyRuleID int64    `json:"legacy_rule_id"`
	GroupID      *int64   `json:"group_id,omitempty"`
	Platform     string   `json:"platform,omitempty"`
	MonitorRules []string `json:"monitor_rules"`
}
type Config struct {
	SiteName       string               `json:"site_name"`
	Policies       []NotificationPolicy `json:"policies"`
	Targets        []Target             `json:"-"`
	RetentionDays  int                  `json:"retention_days"`
	OwnerOverrides []OwnerOverride      `json:"owner_overrides"`
	LegacyHandoffs []LegacyHandoff      `json:"legacy_handoffs"`

	Version             int                   `json:"version"`
	Actor               string                `json:"actor_id"`
	RequestID           string                `json:"idempotency_key"`
	Shadow              bool                  `json:"shadow"`
	Site                string                `json:"site_id"`
	Rules               map[string]RuleConfig `json:"rules"`
	Profiles            map[string]Profile    `json:"profiles"`
	BalanceLow          *float64              `json:"balance_low_usd"`
	SubscriptionLow     *float64              `json:"subscription_low_ratio"`
	FeishuTargets       []string              `json:"feishu_targets"`
	EmailTargets        []string              `json:"email_targets"`
	DetailBaseURL       string                `json:"detail_base_url"`
	PaymentGraceSeconds int                   `json:"payment_grace_seconds"`
	BillingGraceSeconds int                   `json:"billing_grace_seconds"`
}

func DefaultConfig() Config {
	return Config{Site: "ikun2api", Shadow: true, RetentionDays: 90, Rules: map[string]RuleConfig{}, Profiles: map[string]Profile{"default": DefaultProfile()}, PaymentGraceSeconds: 300, BillingGraceSeconds: 120}
}
func (c Config) Profile(model string) Profile {
	if p, ok := c.Profiles[model]; ok {
		return p
	}
	if p, ok := c.Profiles["default"]; ok {
		return p
	}
	return DefaultProfile()
}

type RecentHit struct {
	Request string    `json:"request"`
	User    int64     `json:"user,omitempty"`
	At      time.Time `json:"at"`
}

type Member struct {
	Rule  string    `json:"rule"`
	Model string    `json:"model,omitempty"`
	Count int       `json:"count"`
	Last  time.Time `json:"last"`
}

func (c Config) Rule(id string) RuleConfig {
	if r, ok := c.Rules[id]; ok {
		return r
	}
	return RuleConfig{Enabled: true, Owner: "legacy", Version: 1}
}

func (c Config) Owner(rule, scope string, group int64, model string) string {
	owner := c.Rule(rule).Owner
	best := -1
	for _, o := range c.OwnerOverrides {
		if o.Rule != rule || o.Scope != "" && o.Scope != scope || o.GroupID != nil && *o.GroupID != group || o.Model != "" && o.Model != model {
			continue
		}
		score := 0
		if o.Scope != "" {
			score += 4
		}
		if o.GroupID != nil {
			score += 2
		}
		if o.Model != "" {
			score++
		}
		if score > best {
			best = score
			owner = o.Owner
		}
	}
	return owner
}
