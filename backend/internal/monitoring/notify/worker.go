// Package notify transports immutable outbox revisions. Transport errors expose
// only classified codes, never platform response bodies, tokens or recipient data.
package notify

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/store"
	"slices"
	"time"
)

type Sender interface {
	Send(context.Context, domain.Delivery, string, string) (string, error)
}
type Failure struct {
	Class          string
	Permanent      bool
	RetryAfter     time.Duration
	MessageMissing bool
}

func (f *Failure) Error() string { return f.Class }

type Worker struct {
	Report  func(string, time.Time, error)
	Store   *store.Store
	Senders map[string]Sender
}

func (w *Worker) Tick(ctx context.Context, now time.Time) (result error) {
	defer func() {
		if w.Report != nil {
			w.Report("notification", now, result)
		}
	}()
	d, err := w.Store.Lease(ctx, now, 30*time.Second)
	if err != nil || d == nil {
		return err
	}
	if d.Payload.Rule == "L02" || d.Kind == "reminder" {
		d.State = "suppressed"
		return w.Store.CompleteDelivery(ctx, *d)
	}
	cfg, err := w.Store.Config(ctx)
	if err != nil {
		return err
	}
	allowed := slices.Contains(cfg.FeishuTargets, d.Target)
	if d.Channel == "email" {
		allowed = slices.Contains(cfg.EmailTargets, d.Target)
	}
	if cfg.Shadow || cfg.Owner(d.Payload.Rule, d.Payload.Scope, d.Payload.GroupID, d.Payload.Model) != "monitor" || !cfg.Rule(d.Payload.Rule).Enabled || !allowed {
		d.State = "shadow"
		return w.Store.CompleteDelivery(ctx, *d)
	}
	current, readErr := w.Store.Incident(ctx, d.IncidentID)
	if readErr != nil {
		return readErr
	}
	if current != nil && current.Status == "superseded" {
		d.State = "superseded"
		return w.Store.CompleteDelivery(ctx, *d)
	}
	if current != nil && now.Before(current.SilencedUntil) && d.Kind != "escalated" {
		d.State = "suppressed"
		return w.Store.CompleteDelivery(ctx, *d)
	}
	msg := w.Store.MessageID(ctx, d.IncidentID, d.Channel, d.Target)
	if d.ForceNew {
		msg = ""
	}
	// A quiet close only edits an existing card; it never posts a new message.
	if d.Kind == "closed" && msg == "" {
		d.State = "suppressed"
		return w.Store.CompleteDelivery(ctx, *d)
	}
	sender := w.Senders[d.Channel]
	var id string
	if sender == nil {
		err = &Failure{Class: "channel_not_configured", Permanent: true}
	} else {
		attempt, cancel := context.WithTimeout(ctx, 15*time.Second)
		id, err = sender.Send(attempt, *d, msg, cfg.DetailBaseURL)
		cancel()
	}
	fallback := []domain.Delivery{}
	if err == nil {
		d.State = "accepted"
		d.MessageID = id
		d.AcceptedAt = now
		d.ErrorClass = ""
	} else {
		failure := &Failure{Class: "transport_unknown"}
		var known *Failure
		if errors.As(err, &known) {
			failure = known
		}
		d.ErrorClass = failure.Class
		d.State = "pending"
		delay := retryDelay(d.ID, d.Attempts)
		if failure.RetryAfter > delay {
			delay = failure.RetryAfter
		}
		d.NextAttempt = now.Add(delay)
		if d.Kind == "closed" && (failure.MessageMissing || failure.Permanent) {
			d.State = "suppressed"
		} else if failure.MessageMissing && !d.ForceNew {
			d.ForceNew = true
		} else if failure.Permanent || d.Attempts >= 8 {
			d.State = "dead"
		}
	}
	if err = w.Store.CompleteDelivery(ctx, *d, fallback...); err != nil {
		return err
	}
	// Errors while sending M02 never recursively create another M02.
	if d.Payload.Rule != "M02" {
		return w.health(ctx, *d, cfg.Site, now)
	}
	return nil
}
func retryDelay(id string, attempt int) time.Duration {
	if attempt > 7 {
		attempt = 7
	}
	if attempt < 1 {
		attempt = 1
	}
	base := time.Duration(1<<attempt) * time.Second
	h := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", id, attempt)))
	return base + time.Duration(h[0])*base/1024
}
func (w *Worker) health(ctx context.Context, d domain.Delivery, site string, now time.Time) error {
	m := map[string]float64{"failed_attempts": float64(d.Attempts)}
	if d.State == "accepted" {
		m = map[string]float64{"healthy": 1}
	}
	if d.State == "dead" {
		m["permanent_failure"] = 1
	}
	targetHash := sha256.Sum256([]byte(d.Target))
	o := domain.Observation{Schema: 1, Site: site, Source: "notification", EventID: fmt.Sprintf("%s:%d:%s", d.ID, d.Attempts, d.State), Kind: "delivery.health", TaskID: fmt.Sprintf("%s:%x", d.Channel, targetHash[:8]), Reason: d.ErrorClass, Quality: domain.Fresh, OccurredAt: now, ObservedAt: now, Metrics: m}
	return w.Store.Ingest(ctx, []domain.Observation{o}, domain.Cursor{Source: "notification", LastSuccess: now, Quality: domain.Fresh})
}
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			_ = w.Tick(ctx, now)
		}
	}
}
