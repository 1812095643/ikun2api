package notify

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/MACOS-DO/sub4api/internal/monitoring/detectors"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/store"
)

type Worker struct {
	Store         *store.Store
	Sender        *Sender
	Cipher        store.Cipher
	Accepted      func(context.Context, domain.Incident)
	LegacyAllowed func(context.Context, domain.Incident, domain.Target) (bool, error)
	DetailsURL    func(context.Context) string
	SiteName      func(context.Context) string
}

func (w *Worker) Tick(ctx context.Context, now time.Time) error {
	d, err := w.Store.Lease(ctx, now, 2*time.Minute)
	if err != nil || d == nil {
		return err
	}
	release, ok, err := w.Store.LockChannel(ctx, d.Channel)
	if err != nil || !ok {
		e := w.Store.ReturnLease(ctx, *d, now)
		if err != nil {
			return err
		}
		return e
	}
	defer release()
	cfg, err := w.Store.Config(ctx)
	if err != nil {
		_ = w.Store.ReturnLease(ctx, *d, now)
		return err
	}
	if cfg.SiteName == "" && w.SiteName != nil {
		cfg.SiteName = w.SiteName(ctx)
	}
	if cfg.DetailBaseURL == "" && w.DetailsURL != nil {
		cfg.DetailBaseURL = w.DetailsURL(ctx)
	}
	i, err := w.Store.Incident(ctx, d.IncidentID)
	if err != nil {
		_ = w.Store.ReturnLease(ctx, *d, now)
		return err
	}
	if i == nil {
		return w.finish(ctx, d, "suppressed", "stale", now, cfg)
	}
	target, err := w.Store.Target(ctx, d.Target)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return w.finish(ctx, d, "suppressed", "disabled", now, cfg)
		}
		_ = w.Store.ReturnLease(ctx, *d, now)
		return err
	}
	c, secrets, err := w.Store.ChannelSecret(ctx, d.Channel, w.Cipher)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return w.finish(ctx, d, "suppressed", "disabled", now, cfg)
		}
		if errors.Is(err, store.ErrCredentials) {
			return w.finish(ctx, d, "dead", "credentials", now, cfg)
		}
		_ = w.Store.ReturnLease(ctx, *d, now)
		return err
	}
	defer clear(secrets)
	if !c.Enabled || !target.Enabled {
		return w.finish(ctx, d, "suppressed", "disabled", now, cfg)
	}
	if i.LegacyEventID == 0 {
		if cfg.Shadow {
			return w.finish(ctx, d, "shadow", "", now, cfg)
		}
		if !cfg.Rule(i.Rule).Enabled || cfg.Owner(i.Rule, i.Scope, i.GroupID, i.Model) != "monitor" {
			return w.finish(ctx, d, "suppressed", "disabled", now, cfg)
		}
		allowed := false
		for _, p := range cfg.Policies {
			if p.Matches(*i) && p.Allows(d.Kind, i.Acknowledged) {
				ids := p.TargetIDs
				if d.FallbackFor != "" {
					ids = p.FallbackTargetIDs
				}
				if slices.Contains(ids, d.Target) {
					allowed = true
				}
			}
		}
		if !allowed {
			return w.finish(ctx, d, "suppressed", "disabled", now, cfg)
		}
	} else if w.LegacyAllowed != nil {
		allowed, checkErr := w.LegacyAllowed(ctx, *i, target)
		if checkErr != nil {
			_ = w.Store.ReturnLease(ctx, *d, now)
			return checkErr
		}
		if !allowed {
			return w.finish(ctx, d, "suppressed", "disabled", now, cfg)
		}
	}
	if now.Before(i.SilencedUntil) || d.Kind == "reminder" && i.Acknowledged {
		return w.finish(ctx, d, "suppressed", "stale", now, cfg)
	}
	if (i.Status == "resolved" || i.Status == "closed" || i.Status == "superseded") && d.Kind != "resolved" && d.Kind != "closed" {
		return w.finish(ctx, d, "superseded", "stale", now, cfg)
	}
	if d.RecoveredLease {
		return w.finish(ctx, d, "uncertain", "uncertain", now, cfg)
	}
	previous := ""
	if domain.SupportsEdit(c.Kind) && !d.ForceNew {
		previous = w.Store.MessageID(ctx, d.IncidentID, d.Channel, d.Target)
	}
	if (d.Kind == "update" || d.Kind == "closed") && previous == "" && c.Kind != "webhook" {
		return w.finish(ctx, d, "suppressed", "stale", now, cfg)
	}
	d.Payload = *i
	if err = w.Store.MarkDeliverySending(ctx, *d, time.Now().UTC()); err != nil {
		return err
	}
	result, sendErr := w.Sender.Send(ctx, c, secrets, target, BuildMessage(cfg, *i, d.Kind, d.MentionAll), previous)
	clear(secrets)
	if sendErr == nil {
		d.MessageID = result.MessageID
		d.AcceptedAt = time.Now().UTC()
		if err = w.finish(ctx, d, "accepted", "", now, cfg); err == nil && w.Accepted != nil {
			w.Accepted(ctx, *i)
		}
		return err
	}
	var f *Failure
	if !errors.As(sendErr, &f) {
		f = &Failure{Class: "network"}
	}
	state := "pending"
	if f.Uncertain {
		state = "uncertain"
	} else if f.Permanent || d.Attempts >= 8 {
		state = "dead"
	}
	d.NextAttempt = now.Add(retryDelay(d.ID, d.Attempts))
	if f.RetryAfter > 0 {
		d.NextAttempt = now.Add(f.RetryAfter)
	}
	return w.finish(ctx, d, state, f.Class, now, cfg)
}

func (w *Worker) finish(ctx context.Context, d *domain.Delivery, state, class string, now time.Time, cfg domain.Config) error {
	d.State = state
	d.ErrorClass = class
	fallback := []domain.Delivery{}
	if state == "dead" || state == "uncertain" {
		for _, id := range d.FallbackTargetIDs {
			t, err := w.Store.Target(ctx, id)
			if err != nil || !t.Enabled {
				continue
			}
			key := fmt.Sprintf("%s:%d:%s:%s:%s", d.IncidentID, d.Revision, t.ChannelID, t.ID, d.Kind)
			fallback = append(fallback, domain.Delivery{ID: fmt.Sprintf("%x", sha256.Sum256([]byte(key))), IncidentID: d.IncidentID, Revision: d.Revision, Channel: t.ChannelID, Target: t.ID, Kind: d.Kind, State: "pending", NextAttempt: now, Payload: d.Payload, FallbackFor: d.ID})
		}
	}
	if err := w.Store.CompleteDelivery(ctx, *d, fallback...); err != nil {
		return err
	}
	if d.Payload.Rule != "M02" && (state == "dead" || state == "uncertain" || d.Attempts >= 3 || state == "accepted") {
		metrics := map[string]float64{"failed_attempts": float64(d.Attempts)}
		if state == "dead" || state == "uncertain" {
			metrics["permanent_failure"] = 1
		}
		if state == "accepted" {
			metrics = map[string]float64{"healthy": 1}
		}
		o := domain.Observation{Schema: 1, Site: cfg.Site, Source: "notification", EventID: d.ID + ":" + fmt.Sprint(d.Attempts), Kind: "delivery.health", TaskID: "channel:" + d.Channel, Quality: domain.Fresh, OccurredAt: now, ObservedAt: now, Metrics: metrics}
		return w.Store.Ingest(ctx, []domain.Observation{o}, domain.Cursor{Source: "notification", LastSuccess: now, Quality: domain.Fresh})
	}
	return nil
}

func retryDelay(id string, attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 8 {
		attempt = 8
	}
	sum := sha256.Sum256([]byte(id))
	return time.Duration(5*(1<<uint(attempt-1))+int(sum[0])%5) * time.Second
}

func BuildMessage(cfg domain.Config, i domain.Incident, kind string, mentionAll bool) Message {
	title := detectors.DefinitionFor(i.Rule).Title
	if title == "" {
		title = i.Explanation.Conclusion
	}
	if len([]rune(title)) > 160 {
		title = string([]rune(title)[:160])
	}
	status := "处理中"
	switch i.Status {
	case "recovering":
		status = "恢复观察"
	case "resolved":
		status = "已恢复"
	case "closed":
		status = "已关闭"
	}
	if i.Acknowledged && i.Status == "firing" {
		status = "已确认"
	}
	text := fmt.Sprintf("状态：%s\n平台：%s · 分组：%d\n模型：%s\n影响：%d 个请求 · %d 个账号 · %d 位用户\n最近发生：%s", status, i.Platform, i.GroupID, i.Model, i.RequestCount, i.AccountCount, i.UserCount, i.LastSeen.In(time.FixedZone("CST", 8*3600)).Format("01-02 15:04:05 (UTC+8)"))
	detail := ""
	if domain.ValidHTTPS(cfg.DetailBaseURL) {
		u, _ := url.Parse(cfg.DetailBaseURL)
		u.Path = strings.TrimRight(u.Path, "/") + "/admin/ops"
		u.RawQuery = url.Values{"incident": []string{i.ID}}.Encode()
		u.Fragment = ""
		detail = u.String()
	}
	safeEvent := domain.Incident{LegacyEventID: i.LegacyEventID, ID: i.ID, Rule: i.Rule, Revision: i.Revision, Status: i.Status, Severity: i.Severity, Platform: i.Platform, GroupID: i.GroupID, Model: i.Model, RequestCount: i.RequestCount, AccountCount: i.AccountCount, UserCount: i.UserCount, FirstSeen: i.FirstSeen, LastSeen: i.LastSeen, Explanation: domain.Explanation{Conclusion: title}}
	siteName := cfg.SiteName
	if siteName == "" {
		siteName = cfg.Site
	}
	return Message{EmailVariables: i.LegacyVariables, Title: fmt.Sprintf("【%s｜%s】%s", siteName, i.Severity, title), Text: text, DetailURL: detail, Event: safeEvent, Kind: kind, MentionAll: mentionAll}
}
