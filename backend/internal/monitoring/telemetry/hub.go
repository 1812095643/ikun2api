// 请求内仅保留有界的数字和状态，不保存正文，不等待数据库或外部通知。
package telemetry

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/google/uuid"
	"strconv"
	"strings"
	"sync"
	"time"
)

type contextKey struct{}
type Hub struct {
	mu                          sync.Mutex
	site, instance, build, boot string
	active                      map[string]*Tracker
	events                      []domain.Observation
	seq, ack, dropped           uint64
	activeLimit, eventLimit     int
	Enabled                     func() bool
}
type Tracker struct {
	localReason                                string
	hub                                        *Hub
	id                                         string
	observation                                domain.Observation
	start, first, last, queueStart             time.Time
	queueTotal                                 time.Duration
	done, terminal, failed, partial, supported bool
	attempt                                    int
}

func New(site, instance, build string, activeLimit, eventLimit int) *Hub {
	if activeLimit < 1 {
		activeLimit = 4096
	}
	if eventLimit < 1 {
		eventLimit = 8192
	}
	return &Hub{site: SafeID(site), instance: SafeID(instance), build: build, boot: uuid.NewString(), active: map[string]*Tracker{}, activeLimit: activeLimit, eventLimit: eventLimit, events: make([]domain.Observation, eventLimit)}
}

func SafeID(s string) string {
	valid := true
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("_.:/+-", r)) {
			valid = false
			break
		}
	}
	if !valid || len(s) > 128 || strings.ContainsAny(s, "?@\r\n \"<>") || strings.Contains(s, "://") {
		return fmt.Sprintf("opaque:%x", sha256.Sum256([]byte(s)))
	}
	return s
}
func (h *Hub) Start(ctx context.Context, requestID string, now time.Time) (context.Context, *Tracker) {
	if t := FromContext(ctx); t != nil {
		return ctx, t
	}
	t := &Tracker{hub: h, id: uuid.NewString(), start: now, observation: domain.Observation{Schema: 1, Site: h.site, Instance: h.instance, Source: "gateway", BootID: h.boot, RequestID: SafeID(requestID), Kind: "request.started", OccurredAt: now, ObservedAt: now, Quality: domain.Fresh, Metrics: map[string]float64{"stall_evidence_v1": 1}}}
	h.mu.Lock()
	if len(h.active) < h.activeLimit {
		h.active[t.id] = t
	} else {
		h.dropped++
		t.partial = true
	}
	h.mu.Unlock()
	return context.WithValue(ctx, contextKey{}, t), t
}
func FromContext(ctx context.Context) *Tracker {
	if ctx == nil {
		return nil
	}
	t, _ := ctx.Value(contextKey{}).(*Tracker)
	return t
}
func clone(o domain.Observation) domain.Observation {
	m := make(map[string]float64, len(o.Metrics))
	for k, v := range o.Metrics {
		m[k] = v
	}
	o.Metrics = m
	return o
}
func (h *Hub) emitLocked(o domain.Observation) {
	h.seq++
	o.EventID = h.boot + ":" + strconv.FormatUint(h.seq, 10)
	o.BootID = h.boot
	o.Source = "gateway"
	o.Schema = 1
	o.Site = h.site
	o.Instance = h.instance
	if o.Quality == "" {
		o.Quality = domain.Fresh
	}
	if o.OccurredAt.IsZero() {
		o.OccurredAt = time.Now()
	}
	o.ObservedAt = o.OccurredAt
	if h.seq > uint64(h.eventLimit) && h.seq-uint64(h.eventLimit) > h.ack {
		h.dropped++
	}
	h.events[(h.seq-1)%uint64(h.eventLimit)] = clone(o)
}
func (h *Hub) Emit(o domain.Observation) {
	if h == nil || h.Enabled != nil && !h.Enabled() {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.emitLocked(o)
}
func (t *Tracker) Update(o domain.Observation) {
	if t == nil {
		return
	}
	h := t.hub
	h.mu.Lock()
	defer h.mu.Unlock()
	if o.AccountID != 0 {
		t.observation.AccountID = o.AccountID
	}
	if o.GroupID != 0 {
		t.observation.GroupID = o.GroupID
	}
	if o.UserID != 0 {
		t.observation.UserID = o.UserID
	}
	if o.KeyID != 0 {
		t.observation.KeyID = o.KeyID
	}
	if o.ChannelID != 0 {
		t.observation.ChannelID = o.ChannelID
	}
	if o.Model != "" {
		t.observation.Model = SafeID(o.Model)
	}
	if o.Platform != "" {
		t.observation.Platform = SafeID(o.Platform)
	}
	if o.Reason != "" {
		t.observation.Reason = o.Reason
	}
	for k, v := range o.Metrics {
		t.observation.Metrics[k] = v
	}
}
func (t *Tracker) Event(kind, reason string, status int, account int64, now time.Time) {
	if t == nil {
		return
	}
	h := t.hub
	h.mu.Lock()
	defer h.mu.Unlock()
	o := clone(t.observation)
	o.Kind = kind
	o.Reason = reason
	o.UpstreamStatus = status
	if account != 0 {
		o.AccountID = account
	}
	o.OccurredAt = now
	o.AttemptID = fmt.Sprintf("%s:%d", t.id, t.attempt)
	h.emitLocked(o)
}

func (t *Tracker) Task(kind, taskID, reason string, metrics map[string]float64, now time.Time) {
	if t == nil {
		return
	}
	h := t.hub
	h.mu.Lock()
	defer h.mu.Unlock()
	o := clone(t.observation)
	o.Kind = kind
	o.TaskID = SafeID(taskID)
	o.Reason = reason
	o.OccurredAt = now
	for k, v := range metrics {
		o.Metrics[k] = v
	}
	h.emitLocked(o)
}
func (t *Tracker) Attempt(account int64, model string, now time.Time) {
	if t == nil {
		return
	}
	h := t.hub
	h.mu.Lock()
	defer h.mu.Unlock()
	if t.observation.AccountID != 0 && t.observation.AccountID != account {
		t.observation.Metrics["switches"]++
	}
	t.attempt++
	t.observation.AccountID = account
	t.observation.Model = SafeID(model)
	t.observation.AttemptID = fmt.Sprintf("%s:%d", t.id, t.attempt)
	t.observation.Metrics["attempts"] = float64(t.attempt)
	t.observation.Metrics["pre_forward_ms"] = float64(now.Sub(t.start).Milliseconds())
	t.observation.Metrics["selected_at_ms"] = float64(now.UnixMilli())
}
func (t *Tracker) Queue(start bool, slot string, now time.Time) {
	if t == nil {
		return
	}
	h := t.hub
	h.mu.Lock()
	defer h.mu.Unlock()
	if start {
		t.queueStart = now
		t.observation.Metrics["queue_"+slot] = 1
	} else if !t.queueStart.IsZero() {
		t.queueTotal += now.Sub(t.queueStart)
		t.queueStart = time.Time{}
		t.observation.Metrics["queue_ms"] = float64(t.queueTotal.Milliseconds())
	}
}
func (t *Tracker) Signal(s Signal, now time.Time) {
	if t == nil {
		return
	}
	h := t.hub
	h.mu.Lock()
	defer h.mu.Unlock()
	t.supported = t.supported || s.Supported
	t.partial = t.partial || s.Partial
	t.terminal = t.terminal || s.Done
	if s.OutputBudgetExhausted {
		t.observation.Metrics["output_budget_exhausted"] = 1
	}
	if s.ReasoningStarted && t.observation.N("reasoning_started") != 1 {
		t.observation.Metrics["reasoning_started"] = 1
		t.observation.Metrics["reasoning_started_ms"] = float64(now.Sub(t.start).Milliseconds())
	}
	if s.Failed {
		t.failed = true
		if t.localReason != "" {
			t.observation.Reason = t.localReason
		} else {
			t.observation.Reason = s.Reason
		}
		o := clone(t.observation)
		o.Kind = "stream.failed"
		o.OccurredAt = now
		h.emitLocked(o)
	}
	if s.Output != "" {
		if t.first.IsZero() {
			t.first = now
			t.observation.Metrics["first_effective_ms"] = float64(now.Sub(t.start).Milliseconds())
			t.observation.Metrics["has_output"] = 1
		}
		t.last = now
		k := s.Output + "_output"
		if t.observation.Metrics[k] == 0 {
			t.observation.Metrics[k] = 1
		}
		t.observation.Metrics["effective_bytes"] += float64(s.Bytes)
	}
}
func (t *Tracker) Finish(status int, cancelled bool, now time.Time) {
	if t == nil {
		return
	}
	h := t.hub
	h.mu.Lock()
	defer h.mu.Unlock()
	if t.done {
		return
	}
	t.done = true
	delete(h.active, t.id)
	o := clone(t.observation)
	o.Kind = "request.finished"
	o.ClientStatus = status
	o.OccurredAt = now
	o.Metrics["elapsed_ms"] = float64(now.Sub(t.start).Milliseconds())
	o.Metrics["terminal"] = boolNumber(t.terminal)
	o.Metrics["protocol_supported"] = boolNumber(t.supported)
	o.Metrics["queue_ms"] = float64(t.queueTotal.Milliseconds())
	o.Outcome = "success"
	if cancelled {
		o.Kind = "request.cancelled"
		o.Outcome = "cancelled"
	} else if t.failed || status >= 400 {
		o.Outcome = "failed"
	} else if !t.partial && !t.supported && o.N("expected_text_protocol") == 1 {
		o.Outcome = "failed"
		o.Reason = "wrong_protocol"
	} else if t.partial || !t.supported {
		o.Outcome = "unknown"
		o.Quality = domain.Partial
	} else if !t.terminal {
		o.Outcome = "failed"
		o.Reason = "missing_terminal"
	} else if o.N("output_budget_exhausted") == 1 && o.N("content_output") != 1 && o.N("tool_output") != 1 {
		o.Outcome = "failed"
		o.Reason = "output_budget_exhausted"
	} else if t.first.IsZero() && o.N("reasoning_started") == 1 {
		o.Outcome = "unknown"
		o.Quality = domain.Partial
	} else if t.first.IsZero() {
		o.Outcome = "failed"
		o.Reason = "empty_output"
	}
	h.emitLocked(o)
}
func boolNumber(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
func (h *Hub) Snapshot(after uint64, now time.Time) domain.Snapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	if after > h.seq {
		after = 0
	}
	if after > h.ack {
		h.ack = after
	}
	oldest := uint64(1)
	if h.seq > uint64(h.eventLimit) {
		oldest = h.seq - uint64(h.eventLimit) + 1
	}
	s := domain.Snapshot{Schema: 1, Site: h.site, Instance: h.instance, Build: h.build, BootID: h.boot, Timing: "elapsed_monotonic_ms;pre_forward_includes_upload_and_queue", Sequence: h.seq, Oldest: oldest, Dropped: h.dropped, Events: []domain.Observation{}, Active: []domain.Observation{}, Capabilities: []domain.Capability{{Name: "request_lifecycle", Quality: domain.Fresh}, {Name: "sse_text_reasoning_tool", Quality: domain.Fresh}, {Name: "hidden_reasoning_progress", Quality: domain.Partial, Reason: "empty_thinking_deltas_do_not_prove_progress"}, {Name: "websocket_binary_progress", Quality: domain.Unsupported, Reason: "requires_protocol_specific_events"}}}
	start := after + 1
	if start < oldest {
		start = oldest
	}
	last := h.seq
	if last >= start+1024 {
		last = start + 1023
	}
	s.Sequence = last
	for seq := start; seq <= last; seq++ {
		s.Events = append(s.Events, clone(h.events[(seq-1)%uint64(h.eventLimit)]))
	}
	for _, t := range h.active {
		o := clone(t.observation)
		o.Kind = "request.active"
		o.EventID = fmt.Sprintf("active:%s:%d", t.id, now.Unix()/5)
		o.OccurredAt = now
		o.ObservedAt = now
		o.Metrics["elapsed_ms"] = float64(now.Sub(t.start).Milliseconds())
		if !t.last.IsZero() {
			o.Metrics["idle_ms"] = float64(now.Sub(t.last).Milliseconds())
		}
		o.Metrics["queue_ms"] = float64(t.queueTotal.Milliseconds())
		if !t.queueStart.IsZero() {
			o.Metrics["queue_ms"] += float64(now.Sub(t.queueStart).Milliseconds())
			o.Metrics["queue_active"] = 1
		}
		if t.partial {
			o.Quality = domain.Partial
		}
		s.Active = append(s.Active, o)
	}
	return s
}

func (t *Tracker) LocalError(reason string) {
	if t == nil || reason == "unclassified" || reason == "" {
		return
	}
	t.hub.mu.Lock()
	defer t.hub.mu.Unlock()
	t.localReason = reason
	t.observation.Reason = reason
}
