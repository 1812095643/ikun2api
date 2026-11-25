// 事件、去重证据和通知任务在同一事务保存，查询使用 PostgreSQL 的索引与 JSONB。
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/google/uuid"
	"time"
)

var ErrConflict = errors.New("configuration revision conflict")

type Store struct{ db *sql.DB }

func New(db *sql.DB) *Store      { return &Store{db: db} }
func (s *Store) reader() *sql.DB { return s.db }

func (s *Store) Ingest(ctx context.Context, observations []domain.Observation, c domain.Cursor) error {
	if len(observations) > 16384 {
		return errors.New("观测批次超过上限")
	}
	for _, o := range observations {
		if err := o.Validate(); err != nil {
			return err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if len(observations) > 0 {
		type row struct {
			Source   string             `json:"source"`
			EventID  string             `json:"event_id"`
			Occurred int64              `json:"occurred"`
			Body     domain.Observation `json:"body"`
		}
		batch := make([]row, len(observations))
		for index, o := range observations {
			batch[index] = row{o.Source, o.EventID, o.OccurredAt.UnixMilli(), o}
		}
		raw, e := json.Marshal(batch)
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO ops_monitor_observations(source,event_id,occurred,body) SELECT x.source,x.event_id,x.occurred,x.body FROM jsonb_to_recordset($1::jsonb) AS x(source text,event_id text,occurred bigint,body jsonb) ON CONFLICT DO NOTHING`, string(raw)); e != nil {
			return e
		}
	}
	for _, o := range observations {
		if o.TaskID != "" && o.Quality != domain.Stale && o.Quality != domain.Unsupported {
			switch o.Kind {
			case "media.accepted", "media.pending", "media.completed", "media.failed", "media.cancelled", "billing.applied", "billing.failed", "billing.metadata_failed":
				comparison := ">="
				if o.Kind == "media.accepted" || o.Kind == "media.completed" {
					comparison = "<"
				}
				scope := TaskKey(o)
				b, e := json.Marshal(o)
				if e != nil {
					return e
				}
				_, e = tx.ExecContext(ctx, "INSERT INTO ops_monitor_task_facts VALUES($1,$2,$3,$4) ON CONFLICT(scope,kind) DO UPDATE SET occurred=excluded.occurred,body=excluded.body WHERE excluded.occurred"+comparison+"ops_monitor_task_facts.occurred", scope, o.Kind, o.OccurredAt.UnixMilli(), string(b))
				if e != nil {
					return e
				}
			}
		}
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO ops_monitor_source_cursors VALUES($1,$2) ON CONFLICT(source) DO UPDATE SET body=excluded.body", c.Source, string(b)); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Cursor(ctx context.Context, source string) (domain.Cursor, error) {
	var c domain.Cursor
	var b []byte
	err := s.db.QueryRowContext(ctx, "SELECT body FROM ops_monitor_source_cursors WHERE source=$1", source).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Cursor{Source: source, Quality: domain.Stale}, nil
	}
	if err != nil {
		return c, err
	}
	err = json.Unmarshal(b, &c)
	return c, err
}
func (s *Store) Cursors(ctx context.Context) ([]domain.Cursor, error) {
	return readJSON[domain.Cursor](ctx, s.db, "SELECT body FROM ops_monitor_source_cursors ORDER BY source")
}

func (s *Store) Window(ctx context.Context, since time.Time, limit int) ([]domain.Observation, bool, error) {
	limit = bounded(limit)
	rows, err := readJSON[domain.Observation](ctx, s.db, "SELECT body FROM ops_monitor_observations WHERE occurred>=$1 ORDER BY occurred DESC,seq DESC LIMIT $2", since.UnixMilli(), limit+1)
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	for a, b := 0, len(rows)-1; a < b; a, b = a+1, b-1 {
		rows[a], rows[b] = rows[b], rows[a]
	}
	return rows, truncated, err
}
func (s *Store) Observations(ctx context.Context, since time.Time, limit int) ([]domain.Observation, error) {
	rows, _, err := s.Window(ctx, since, limit)
	return rows, err
}
func (s *Store) TaskFacts(ctx context.Context) ([]domain.Observation, error) {
	rows, _, err := s.TaskPage(ctx, "", 500, nil)
	return rows, err
}
func bounded(n int) int {
	if n <= 0 || n > 20000 {
		return 20000
	}
	return n
}
func readJSON[T any](ctx context.Context, db *sql.DB, q string, args ...any) ([]T, error) {
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []T{}
	for rows.Next() {
		var b []byte
		var v T
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(b, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) Config(ctx context.Context) (domain.Config, error) {
	var b []byte
	err := s.db.QueryRowContext(ctx, "SELECT body FROM ops_monitor_config WHERE id=1").Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DefaultConfig(), nil
	}
	var c domain.Config
	if err == nil {
		err = json.Unmarshal(b, &c)
	}
	if err == nil {
		c.Targets, err = s.Targets(ctx, true)
	}
	if c.RetentionDays == 0 {
		c.RetentionDays = 90
	}
	return c, err
}
func (s *Store) SaveConfig(ctx context.Context, c domain.Config, expected int) error {
	if c.Actor == "" || c.RequestID == "" {
		return errors.New("actor and idempotency key required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", ConfigLock); err != nil {
		return err
	}
	original, _ := json.Marshal(c)
	var prev []byte
	err = tx.QueryRowContext(ctx, "SELECT body FROM ops_monitor_mutations WHERE id=$1", c.RequestID).Scan(&prev)
	if err == nil {
		if !jsonEqual(prev, original) {
			return ErrConflict
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var version int
	var oldBody []byte
	old := domain.DefaultConfig()
	err = tx.QueryRowContext(ctx, "SELECT version,body FROM ops_monitor_config WHERE id=1 FOR UPDATE").Scan(&version, &oldBody)
	if err == nil {
		if err = json.Unmarshal(oldBody, &old); err != nil {
			return err
		}
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if version != expected {
		return ErrConflict
	}
	if version > 0 && c.Site != old.Site {
		return errors.New("site identity cannot change on an existing state database")
	}
	changed := semanticConfig(c) != semanticConfig(old)
	ruleCopy := make(map[string]domain.RuleConfig, len(c.Rules))
	for id, r := range c.Rules {
		ruleCopy[id] = r
	}
	c.Rules = ruleCopy
	for id, r := range c.Rules {
		r.Version = old.Rule(id).Version
		if changed {
			r.Version++
		}
		c.Rules[id] = r
	}
	c.Version = expected + 1
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO ops_monitor_config VALUES(1,$1,$2) ON CONFLICT(id) DO UPDATE SET version=excluded.version,body=excluded.body", c.Version, string(b)); err != nil {
		return err
	}
	rows, e := tx.QueryContext(ctx, "SELECT body FROM ops_monitor_incidents WHERE (body #>> '{status}') NOT IN ('resolved','superseded','closed') AND COALESCE(body->>'legacy_event_id','0')='0'")
	if e != nil {
		return e
	}
	updates := []domain.Incident{}
	for rows.Next() {
		var raw []byte
		var i domain.Incident
		if e = rows.Scan(&raw); e != nil {
			rows.Close()
			return e
		}
		if e = json.Unmarshal(raw, &i); e != nil {
			rows.Close()
			return e
		}
		touched := false
		if changed {
			i.Status = "superseded"
			touched = true
		}
		if old.Shadow && !c.Shadow || old.Owner(i.Rule, i.Scope, i.GroupID, i.Model) != c.Owner(i.Rule, i.Scope, i.GroupID, i.Model) {
			i.LastNotified = time.Time{}
			i.NotifiedCount = 0
			touched = true
		}
		if touched {
			i.Revision++
			i.LastActor = c.Actor
			i.ControlReason = "configuration_changed"
			updates = append(updates, i)
		}
	}
	if e = rows.Err(); e != nil {
		rows.Close()
		return e
	}
	rows.Close()
	for _, i := range updates {
		raw, _ := json.Marshal(i)
		if _, e = tx.ExecContext(ctx, "UPDATE ops_monitor_incidents SET body=$1 WHERE id=$2", string(raw), i.ID); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO ops_monitor_incident_transitions(incident_id,created,body) VALUES($1,$2,$3)", i.ID, time.Now().UnixMilli(), string(raw)); e != nil {
			return e
		}
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO ops_monitor_mutations VALUES($1,$2,$3,$4)", c.RequestID, c.Actor, string(original), time.Now().UnixMilli()); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Incidents(ctx context.Context) ([]domain.Incident, error) {
	return readJSON[domain.Incident](ctx, s.reader(), "SELECT body FROM ops_monitor_incidents ORDER BY updated DESC LIMIT 1000")
}
func (s *Store) Incident(ctx context.Context, fingerprint string) (*domain.Incident, error) {
	var b []byte
	err := s.db.QueryRowContext(ctx, "SELECT body FROM ops_monitor_incidents WHERE fingerprint=$1 OR id=$2", fingerprint, fingerprint).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var i domain.Incident
	err = json.Unmarshal(b, &i)
	return &i, err
}
func (s *Store) SaveIncident(ctx context.Context, i domain.Incident, ds []domain.Delivery) error {
	return s.SaveFinding(ctx, i, ds, "")
}

func (s *Store) SaveFinding(ctx context.Context, i domain.Incident, ds []domain.Delivery, evidenceID string, findings ...*domain.Finding) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `UPDATE ops_monitor_incidents SET fingerprint=fingerprint || ':history:' || id WHERE fingerprint=$1 AND id<>$2 AND body->>'status' IN ('resolved','closed')`, i.Fingerprint, i.ID); err != nil {
		return err
	}
	if evidenceID != "" {
		r, e := tx.ExecContext(ctx, "INSERT INTO ops_monitor_incident_evidence VALUES($1,$2,$3) ON CONFLICT DO NOTHING", i.ID, evidenceID, i.LastSeen.UnixMilli())
		if e != nil {
			return e
		}
		n, _ := r.RowsAffected()
		if n == 0 {
			return nil
		}
	}
	for _, f := range findings {
		if f == nil || f.Healthy {
			continue
		}
		samples := append([]domain.IncidentSample{{RequestID: f.RequestID, AccountID: f.AccountID, UserID: f.UserID}}, f.RelatedSamples...)
		for _, sample := range samples {
			impacts := map[string]string{"request": sample.RequestID}
			if sample.AccountID > 0 {
				impacts["account"] = fmt.Sprint(sample.AccountID)
			}
			if sample.UserID > 0 {
				impacts["user"] = fmt.Sprint(sample.UserID)
			}
			for kind, value := range impacts {
				if value != "" {
					if _, err = tx.ExecContext(ctx, "INSERT INTO ops_monitor_incident_impact VALUES($1,$2,$3) ON CONFLICT DO NOTHING", i.ID, kind, value); err != nil {
						return err
					}
				}
			}
		}
	}
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM ops_monitor_incident_impact WHERE incident_id=$1 AND kind='request'", i.ID).Scan(&i.RequestCount); err != nil {
		return err
	}
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM ops_monitor_incident_impact WHERE incident_id=$1 AND kind='account'", i.ID).Scan(&i.AccountCount); err != nil {
		return err
	}
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM ops_monitor_incident_impact WHERE incident_id=$1 AND kind='user'", i.ID).Scan(&i.UserCount); err != nil {
		return err
	}
	changedAt := i.LastSeen
	if i.ResolvedAt.After(changedAt) {
		changedAt = i.ResolvedAt
	}
	b, err := json.Marshal(i)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO ops_monitor_incidents VALUES($1,$2,$3,$4) ON CONFLICT(id) DO UPDATE SET fingerprint=excluded.fingerprint,updated=excluded.updated,body=excluded.body", i.ID, i.Fingerprint, changedAt.UnixMilli(), string(b)); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO ops_monitor_incident_transitions(incident_id,created,body) VALUES($1,$2,$3)", i.ID, changedAt.UnixMilli(), string(b)); err != nil {
		return err
	}
	for _, d := range ds {
		if d.Kind == "update" && d.State != "shadow" {
			_, err = tx.ExecContext(ctx, `UPDATE ops_monitor_notification_outbox SET state='superseded',body=jsonb_set(body,'{state}','"superseded"'::jsonb) WHERE incident_id=$1 AND channel=$2 AND target=$3 AND kind='update' AND state='pending' AND revision<$4`, d.IncidentID, d.Channel, d.Target, d.Revision)
			if err != nil {
				return err
			}
		}
		if d.State == "" {
			d.State = "pending"
		}
		d.Payload = i
		b, err = json.Marshal(d)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO ops_monitor_notification_outbox(id,incident_id,revision,channel,target,kind,state,next_attempt,body) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT DO NOTHING", d.ID, d.IncidentID, d.Revision, d.Channel, d.Target, d.Kind, d.State, d.NextAttempt.UnixMilli(), string(b)); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) EvidenceExists(ctx context.Context, id, key string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM ops_monitor_incident_evidence WHERE incident_id=$1 AND evidence_id=$2", id, key).Scan(&n)
	return n > 0, err
}
func (s *Store) Lease(ctx context.Context, now time.Time, duration time.Duration) (*domain.Delivery, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var b []byte
	var d domain.Delivery
	var attempts int
	var priorState string
	var sendStarted int64
	err = tx.QueryRowContext(ctx, `SELECT o.body,o.attempts,o.state,o.send_started_at FROM ops_monitor_notification_outbox o WHERE o.state IN ('pending','leased') AND o.next_attempt<=$1 AND o.lease_until<=$2 AND NOT EXISTS(SELECT 1 FROM ops_monitor_notification_outbox older WHERE older.incident_id=o.incident_id AND older.channel=o.channel AND older.target=o.target AND older.revision<o.revision AND older.state IN ('pending','leased')) ORDER BY o.next_attempt,o.revision LIMIT 1 FOR UPDATE OF o SKIP LOCKED`, now.UnixMilli(), now.UnixMilli()).Scan(&b, &attempts, &priorState, &sendStarted)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(b, &d); err != nil {
		return nil, err
	}
	d.Attempts = attempts + 1
	d.RecoveredLease = priorState == "leased" && sendStarted > 0
	d.State = "leased"
	d.LeaseUntil = now.Add(duration)
	d.LeaseToken = uuid.NewString()
	if _, err = tx.ExecContext(ctx, "UPDATE ops_monitor_notification_outbox SET state='leased',lease_until=$1,lease_token=$2,attempts=$3 WHERE id=$4", d.LeaseUntil.UnixMilli(), d.LeaseToken, d.Attempts, d.ID); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &d, nil
}
func (s *Store) CompleteDelivery(ctx context.Context, d domain.Delivery, fallback ...domain.Delivery) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	r, err := tx.ExecContext(ctx, "UPDATE ops_monitor_notification_outbox SET state=$1,next_attempt=$2,lease_until=0,send_started_at=0,body=$3 WHERE id=$4 AND lease_token=$5 AND state='leased'", d.State, d.NextAttempt.UnixMilli(), string(b), d.ID, d.LeaseToken)
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return errors.New("expired delivery lease")
	}
	if d.State == "accepted" && d.MessageID != "" {
		_, err = tx.ExecContext(ctx, "INSERT INTO ops_monitor_message_map VALUES($1,$2,$3,$4,$5) ON CONFLICT(incident_id,channel,target) DO UPDATE SET message_id=excluded.message_id,revision=excluded.revision WHERE excluded.revision>=ops_monitor_message_map.revision", d.IncidentID, d.Channel, d.Target, d.MessageID, d.Revision)
		if err != nil {
			return err
		}
	}
	for _, next := range fallback {
		raw, e := json.Marshal(next)
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, "INSERT INTO ops_monitor_notification_outbox(id,incident_id,revision,channel,target,kind,state,next_attempt,body) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT DO NOTHING", next.ID, next.IncidentID, next.Revision, next.Channel, next.Target, next.Kind, next.State, next.NextAttempt.UnixMilli(), string(raw))
		if e != nil {
			return e
		}
	}
	return tx.Commit()
}
func (s *Store) MessageID(ctx context.Context, i, channel, target string) string {
	var id string
	_ = s.db.QueryRowContext(ctx, "SELECT message_id FROM ops_monitor_message_map WHERE incident_id=$1 AND channel=$2 AND target=$3", i, channel, target).Scan(&id)
	return id
}
func (s *Store) Deliveries(ctx context.Context) ([]domain.Delivery, error) {
	return readJSON[domain.Delivery](ctx, s.db, "SELECT body || jsonb_build_object('state',state,'attempts',attempts) FROM ops_monitor_notification_outbox ORDER BY next_attempt DESC LIMIT 1000")
}

func (s *Store) DeliveryLog(ctx context.Context) ([]domain.Delivery, error) {
	return readJSON[domain.Delivery](ctx, s.reader(), "SELECT (body || jsonb_build_object('state',state,'attempts',attempts)) - 'payload' FROM ops_monitor_notification_outbox ORDER BY next_attempt DESC LIMIT 300")
}
func (s *Store) State(ctx context.Context, key string, dest any) error {
	var b []byte
	err := s.db.QueryRowContext(ctx, "SELECT body FROM ops_monitor_rule_state WHERE key=$1", key).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dest)
}
func (s *Store) SaveState(ctx context.Context, key string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, "INSERT INTO ops_monitor_rule_state VALUES($1,$2) ON CONFLICT(key) DO UPDATE SET body=excluded.body", key, string(b))
	return err
}

const HistoryRetention = 90 * 24 * time.Hour

func (s *Store) Retain(ctx context.Context, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	cfg, err := s.Config(ctx)
	if err != nil {
		return err
	}
	days := cfg.RetentionDays
	if days < 7 || days > 365 {
		days = 90
	}
	history := now.Add(-time.Duration(days) * 24 * time.Hour)
	for _, q := range []struct {
		sql string
		cut time.Time
	}{
		{"DELETE FROM ops_monitor_observations WHERE occurred<$1 AND seq<=COALESCE((SELECT (body->>'sequence')::bigint FROM ops_monitor_rule_state WHERE key='evaluation'),0)", now.Add(-3 * 24 * time.Hour)},
		{"DELETE FROM ops_monitor_incident_transitions WHERE created<$1 AND NOT EXISTS (SELECT 1 FROM ops_monitor_incidents i WHERE i.id=ops_monitor_incident_transitions.incident_id AND i.body->>'status' NOT IN ('resolved','closed','superseded'))", history},
		{"DELETE FROM ops_monitor_incident_evidence WHERE created<$1 AND NOT EXISTS (SELECT 1 FROM ops_monitor_notification_outbox o WHERE o.incident_id=ops_monitor_incident_evidence.incident_id AND o.state IN ('pending','leased','uncertain','dead')) AND EXISTS(SELECT 1 FROM ops_monitor_incidents i WHERE i.id=ops_monitor_incident_evidence.incident_id AND i.body->>'status' IN ('resolved','closed','superseded'))", history},
		{"DELETE FROM ops_monitor_task_facts f WHERE occurred<$1 AND EXISTS(SELECT 1 FROM ops_monitor_task_facts done WHERE done.scope=f.scope AND done.kind IN ('media.completed','media.failed','media.cancelled')) AND NOT EXISTS (SELECT 1 FROM ops_monitor_incidents i WHERE i.body->>'status' NOT IN ('resolved','closed','superseded') AND i.body->>'scope'='task:' || (f.body->>'task_id') || '/user:' || COALESCE(f.body->>'user_id','0') || '/key:' || COALESCE(f.body->>'key_id','0'))", history},
		{"DELETE FROM ops_monitor_mutations WHERE created<$1", now.Add(-90 * 24 * time.Hour)},
		{"DELETE FROM ops_monitor_notification_outbox WHERE state IN ('accepted','shadow','superseded','suppressed') AND next_attempt<$1", history},
	} {
		if _, err = tx.ExecContext(ctx, q.sql, q.cut.UnixMilli()); err != nil {
			return fmt.Errorf("retention: %w", err)
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM ops_monitor_incidents WHERE updated<$1 AND (body #>> '{status}') IN ('resolved','closed','superseded') AND NOT EXISTS (SELECT 1 FROM ops_monitor_notification_outbox o WHERE o.incident_id=ops_monitor_incidents.id AND o.state IN ('pending','leased','dead','uncertain'))`, history.UnixMilli()); err != nil {
		return err
	}
	for _, table := range []string{"ops_monitor_incident_evidence", "ops_monitor_incident_impact", "ops_monitor_message_map", "ops_monitor_incident_transitions"} {
		if _, err = tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE NOT EXISTS (SELECT 1 FROM ops_monitor_incidents i WHERE i.id="+table+".incident_id)"); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func semanticConfig(c domain.Config) string {
	b, _ := json.Marshal(struct {
		Profiles              map[string]domain.Profile
		Balance, Subscription *float64
		Payment, Billing      int
	}{c.Profiles, c.BalanceLow, c.SubscriptionLow, c.PaymentGraceSeconds, c.BillingGraceSeconds})
	return string(b)
}
