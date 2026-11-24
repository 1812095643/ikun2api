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
	return readJSON[domain.Incident](ctx, s.db, "SELECT body FROM incidents WHERE json_extract(body,'$.status') NOT IN ('resolved','superseded') ORDER BY updated")
}

// DigestOnlyIncidents lists incidents summarised in the daily digest instead of carded.
func (s *Store) DigestOnlyIncidents(ctx context.Context) ([]domain.Incident, error) {
	return readJSON[domain.Incident](ctx, s.db, "SELECT body FROM incidents WHERE json_extract(body,'$.digest_only')=1 ORDER BY updated")
}

// OldestObservation returns the earliest stored observation time, or zero if none.
func (s *Store) OldestObservation(ctx context.Context) (time.Time, error) {
	var at sql.NullInt64
	if err := s.db.QueryRowContext(ctx, "SELECT min(occurred) FROM observations").Scan(&at); err != nil || !at.Valid {
		return time.Time{}, err
	}
	return time.UnixMilli(at.Int64).UTC(), nil
}

// ServedSince returns the latest success per "group/model" among stored
// observations since the cutoff (gateway successes and billed usage records).
func (s *Store) ServedSince(ctx context.Context, since time.Time) (map[string]time.Time, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT json_extract(body,'$.group_id'), json_extract(body,'$.model'), max(occurred) FROM observations
		WHERE occurred>=? AND json_extract(body,'$.kind') IN ('request.finished','usage.recorded')
		AND (json_extract(body,'$.kind')='usage.recorded' OR json_extract(body,'$.outcome')='success')
		AND json_extract(body,'$.source_quality') NOT IN ('stale','unsupported')
		AND json_extract(body,'$.group_id')>0 AND COALESCE(json_extract(body,'$.model'),'')<>''
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

// RecentlyResolved lists resolved incidents last updated at or after since.
func (s *Store) RecentlyResolved(ctx context.Context, since time.Time) ([]domain.Incident, error) {
	return readJSON[domain.Incident](ctx, s.db, "SELECT body FROM incidents WHERE json_extract(body,'$.status')='resolved' AND updated>=? ORDER BY updated", since.UnixMilli())
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
	defer tx.Rollback()
	var prior []byte
	err = tx.QueryRowContext(ctx, "SELECT body FROM mutations WHERE id=?", requestID).Scan(&prior)
	if err == nil {
		if string(prior) == string(body) {
			return nil
		}
		return ErrConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var raw []byte
	err = tx.QueryRowContext(ctx, "SELECT body FROM incidents WHERE id=?", id).Scan(&raw)
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
		i.Status = "resolved"
	}
	i.Revision++
	i.LastActor = actor
	i.ControlReason = reason
	raw, _ = json.Marshal(i)
	if _, err = tx.ExecContext(ctx, "UPDATE incidents SET body=?,updated=? WHERE id=?", raw, now.UnixMilli(), id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO incident_transitions(incident_id,created,body) VALUES(?,?,?)", id, now.UnixMilli(), raw); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO mutations VALUES(?,?,?,?)", requestID, actor, body, now.UnixMilli()); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Children(ctx context.Context, id string) ([]domain.Incident, error) {
	return readJSON[domain.Incident](ctx, s.db, "SELECT body FROM incidents WHERE json_extract(body,'$.parent_id')=?", id)
}

func (s *Store) History(ctx context.Context, id string) ([]domain.Incident, error) {
	return readJSON[domain.Incident](ctx, s.reader(), "SELECT body FROM incident_transitions WHERE incident_id=? ORDER BY id DESC LIMIT 100", id)
}

// ImpactValues lists distinct affected values ("user", "account", "request") for an incident.
func (s *Store) ImpactValues(ctx context.Context, id, kind string, limit int) ([]string, error) {
	rows, err := s.reader().QueryContext(ctx, "SELECT value FROM incident_impact WHERE incident_id=? AND kind=? LIMIT ?", id, kind, limit)
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

// EvidenceHits returns evidence timestamps since the cutoff (bounded), newest last.
func (s *Store) EvidenceHits(ctx context.Context, id string, since time.Time) ([]EvidenceHit, error) {
	rows, err := s.reader().QueryContext(ctx, "SELECT evidence_id, created FROM incident_evidence WHERE incident_id=? AND created>=? ORDER BY created DESC LIMIT 5000", id, since.UnixMilli())
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

// LiveObservations returns gateway request lifecycle observations since the cutoff.
func (s *Store) LiveObservations(ctx context.Context, since time.Time) ([]domain.Observation, error) {
	// Ends and upstream failures cover the whole window; snapshots of in-flight
	// requests only matter for the last two minutes and are most of the volume.
	// The time index keeps this to one hour of rows; the source index would scan days of gateway events.
	return readJSON[domain.Observation](ctx, s.reader(), `SELECT body FROM observations INDEXED BY observations_time
		WHERE occurred>=? AND source='gateway' AND (
		json_extract(body,'$.kind') IN ('request.finished','request.cancelled','attempt.failed')
		OR (occurred>=? AND json_extract(body,'$.kind') IN ('request.started','request.active')))
		ORDER BY occurred LIMIT 60000`, since.UnixMilli(), time.Now().Add(-2*time.Minute).UnixMilli())
}

// PageIncidents returns open incidents plus the 200 most recent closed ones from the last 3 days.
func (s *Store) PageIncidents(ctx context.Context, now time.Time) ([]domain.Incident, error) {
	return readJSON[domain.Incident](ctx, s.reader(), `SELECT body FROM (
		SELECT body, updated FROM incidents WHERE json_extract(body,'$.status') NOT IN ('resolved','superseded')
		UNION ALL SELECT body, updated FROM (SELECT body, updated FROM incidents INDEXED BY incidents_updated WHERE updated>=? AND json_extract(body,'$.status') IN ('resolved','superseded') AND json_extract(body,'$.closed_by') IS NOT 'quiet' ORDER BY updated DESC LIMIT 200)
	) ORDER BY updated DESC`, now.Add(-3*24*time.Hour).UnixMilli())
}
