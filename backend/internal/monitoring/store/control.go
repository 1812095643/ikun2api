package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"strings"
	"time"
)

var ErrNotFound = errors.New("incident not found")

func (s *Store) ActiveIncidents(ctx context.Context) ([]domain.Incident, error) {
	return readJSON[domain.Incident](ctx, s.db, "SELECT body FROM ops_monitor_incidents WHERE (body #>> '{status}') NOT IN ('resolved','superseded','closed') AND COALESCE(body->>'legacy_event_id','0')='0' ORDER BY updated")
}

func (s *Store) DigestOnlyIncidents(ctx context.Context) ([]domain.Incident, error) {
	return readJSON[domain.Incident](ctx, s.db, "SELECT body FROM ops_monitor_incidents WHERE COALESCE((body #>> '{digest_only}')::boolean,false)=true ORDER BY updated")
}

func (s *Store) OldestObservation(ctx context.Context) (time.Time, error) {
	var at sql.NullInt64
	if err := s.db.QueryRowContext(ctx, "SELECT min(occurred) FROM ops_monitor_observations").Scan(&at); err != nil || !at.Valid {
		return time.Time{}, err
	}
	return time.UnixMilli(at.Int64).UTC(), nil
}

func (s *Store) ServedSince(ctx context.Context, since time.Time) (map[string]time.Time, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT COALESCE((body #>> '{group_id}')::bigint,0), (body #>> '{model}'), max(occurred) FROM ops_monitor_observations
		WHERE occurred>=$1 AND (body #>> '{kind}') IN ('request.finished','usage.recorded')
		AND ((body #>> '{kind}')='usage.recorded' OR (body #>> '{outcome}')='success')
		AND (body #>> '{source_quality}') NOT IN ('stale','unsupported')
		AND COALESCE((body #>> '{group_id}')::bigint,0)>0 AND COALESCE((body #>> '{model}'),'')<>''
		GROUP BY 1,2`, since.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var group int64
		var model string
		var at int64
		if err = rows.Scan(&group, &model, &at); err != nil {
			return nil, err
		}
		out[fmt.Sprintf("%d/%s", group, model)] = time.UnixMilli(at).UTC()
	}
	return out, rows.Err()
}

func (s *Store) RecentlyResolved(ctx context.Context, since time.Time) ([]domain.Incident, error) {
	return readJSON[domain.Incident](ctx, s.db, "SELECT body FROM ops_monitor_incidents WHERE (body #>> '{status}')='resolved' AND COALESCE(body->>'legacy_event_id','0')='0' AND updated>=$1 ORDER BY updated", since.UnixMilli())
}
func (s *Store) ControlIncident(ctx context.Context, id, action, actor, requestID string, revision int, until time.Time, reason string, now time.Time) error {
	if actor == "" || requestID == "" || len(requestID) > 128 || len(reason) > 500 || strings.ContainsAny(reason, "\r\x00") {
		return errors.New("invalid control fields")
	}
	if action != "ack" && action != "unack" && action != "silence" && action != "unsilence" && action != "close" {
		return errors.New("invalid control action")
	}
	if action == "silence" && (!until.After(now) || until.After(now.Add(7*24*time.Hour))) {
		return errors.New("silence must expire within seven days")
	}
	if (action == "silence" || action == "close") && strings.TrimSpace(reason) == "" {
		return errors.New("reason required")
	}
	body, _ := json.Marshal(struct {
		ID, Action, Actor string
		Revision          int
		Until             time.Time
		Reason            string
	}{id, action, actor, revision, until, reason})
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", ConfigLock); err != nil {
		return err
	}
	var prior []byte
	err = tx.QueryRowContext(ctx, "SELECT body FROM ops_monitor_mutations WHERE id=$1", requestID).Scan(&prior)
	if err == nil {
		if jsonEqual(prior, body) {
			return nil
		}
		return ErrConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var raw []byte
	err = tx.QueryRowContext(ctx, "SELECT body FROM ops_monitor_incidents WHERE id=$1 FOR UPDATE", id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	var i domain.Incident
	if err = json.Unmarshal(raw, &i); err != nil {
		return err
	}
	if i.Revision != revision {
		return ErrConflict
	}
	previousStatus := i.Status
	switch action {
	case "ack":
		i.Acknowledged = true
	case "unack":
		i.Acknowledged = false
	case "silence":
		i.SilencedUntil = until
	case "unsilence":
		i.SilencedUntil = time.Time{}
	case "close":
		i.Status = "closed"
		i.ClosedBy = "manual"
		i.ResolvedAt = now
	}
	i.Revision++
	i.LastControlAt = now
	i.LastActor = actor
	i.ControlReason = reason
	raw, _ = json.Marshal(i)
	if _, err = tx.ExecContext(ctx, "UPDATE ops_monitor_incidents SET body=$1,updated=$2 WHERE id=$3", string(raw), now.UnixMilli(), id); err != nil {
		return err
	}
	transition, _ := json.Marshal(domain.IncidentTransition{Incident: i, RecordedAt: now, Action: action, Actor: actor, PreviousStatus: previousStatus})
	if _, err = tx.ExecContext(ctx, "INSERT INTO ops_monitor_incident_transitions(incident_id,created,body) VALUES($1,$2,$3)", id, now.UnixMilli(), string(transition)); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO ops_monitor_mutations VALUES($1,$2,$3,$4)", requestID, actor, string(body), now.UnixMilli()); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Children(ctx context.Context, id string) ([]domain.Incident, error) {
	return readJSON[domain.Incident](ctx, s.db, "SELECT body FROM ops_monitor_incidents WHERE (body #>> '{parent_id}')=$1", id)
}

func (s *Store) History(ctx context.Context, id string) ([]domain.Incident, error) {
	return readJSON[domain.Incident](ctx, s.reader(), "SELECT body FROM ops_monitor_incident_transitions WHERE incident_id=$1 ORDER BY id DESC LIMIT 100", id)
}

func (s *Store) ImpactValues(ctx context.Context, id, kind string, limit int) ([]string, error) {
	rows, err := s.reader().QueryContext(ctx, "SELECT value FROM ops_monitor_incident_impact WHERE incident_id=$1 AND kind=$2 LIMIT $3", id, kind, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		if err = rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

type EvidenceHit struct {
	At      time.Time `json:"at"`
	Healthy bool      `json:"healthy"`
}

func (s *Store) EvidenceHits(ctx context.Context, id string, since time.Time) ([]EvidenceHit, error) {
	rows, err := s.reader().QueryContext(ctx, "SELECT evidence_id, created FROM ops_monitor_incident_evidence WHERE incident_id=$1 AND created>=$2 ORDER BY created DESC LIMIT 5000", id, since.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EvidenceHit{}
	for rows.Next() {
		var ev string
		var at int64
		if err = rows.Scan(&ev, &at); err != nil {
			return nil, err
		}
		out = append(out, EvidenceHit{At: time.UnixMilli(at).UTC(), Healthy: strings.HasSuffix(ev, ":true") || strings.HasSuffix(ev, ":healthy")})
	}
	for a, b := 0, len(out)-1; a < b; a, b = a+1, b-1 {
		out[a], out[b] = out[b], out[a]
	}
	return out, rows.Err()
}

func (s *Store) LiveObservations(ctx context.Context, since time.Time) ([]domain.Observation, error) {
	return readJSON[domain.Observation](ctx, s.reader(), `SELECT body FROM ops_monitor_observations
		WHERE occurred>=$1 AND source='gateway' AND (
		(body #>> '{kind}') IN ('request.finished','request.cancelled','attempt.failed')
		OR (occurred>=$2 AND (body #>> '{kind}') IN ('request.started','request.active')))
		ORDER BY occurred LIMIT 60000`, since.UnixMilli(), time.Now().Add(-2*time.Minute).UnixMilli())
}

func (s *Store) PageIncidents(ctx context.Context, now time.Time) ([]domain.Incident, error) {
	return readJSON[domain.Incident](ctx, s.reader(), `SELECT body FROM (
		SELECT body, updated FROM ops_monitor_incidents WHERE (body #>> '{status}') NOT IN ('resolved','superseded','closed')
		UNION ALL SELECT body, updated FROM (SELECT body, updated FROM ops_monitor_incidents WHERE updated>=$1 AND (body #>> '{status}') IN ('resolved','superseded') AND (body #>> '{closed_by}') IS DISTINCT FROM 'quiet' ORDER BY updated DESC LIMIT 200)
	) ORDER BY updated DESC`, now.Add(-3*24*time.Hour).UnixMilli())
}
