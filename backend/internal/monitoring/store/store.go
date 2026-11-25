package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

var ErrConflict = errors.New("configuration revision conflict")

type Store struct {
	db *sql.DB
	// ro serves admin-page reads on separate WAL reader connections so the
	// page is not queued behind the evaluator's single writer connection.
	ro   *sql.DB
	lock *os.File
}

func (s *Store) reader() *sql.DB {
	if s.ro != nil {
		return s.ro
	}
	return s.db
}

func Open(path string) (*Store, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("state path must be absolute")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	for _, p := range []string{path, path + ".lock"} {
		if st, e := os.Lstat(p); e == nil && st.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("state symlink refused")
		}
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return nil, errors.New("monitor already running")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		lock.Close()
		return nil, err
	}
	s := &Store{db: db, lock: lock}
	db.SetMaxOpenConns(1)
	var schema int
	if e := db.QueryRow("SELECT COALESCE(MAX(version),0) FROM schema_migrations").Scan(&schema); e == nil && schema > 1 {
		s.Close()
		return nil, errors.New("monitor state schema is newer than this binary")
	}
	_, err = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=3000;
	 PRAGMA max_page_count=262144;
 CREATE TABLE IF NOT EXISTS schema_migrations(version INTEGER PRIMARY KEY);
 INSERT OR IGNORE INTO schema_migrations VALUES(1);
 CREATE TABLE IF NOT EXISTS observations(seq INTEGER PRIMARY KEY AUTOINCREMENT, source TEXT NOT NULL,event_id TEXT NOT NULL,occurred INTEGER NOT NULL,body BLOB NOT NULL,UNIQUE(source,event_id));
 CREATE INDEX IF NOT EXISTS observations_time ON observations(occurred,seq);
 CREATE TABLE IF NOT EXISTS task_facts(scope TEXT NOT NULL,kind TEXT NOT NULL,occurred INTEGER NOT NULL,body BLOB NOT NULL,PRIMARY KEY(scope,kind));
 CREATE TABLE IF NOT EXISTS source_cursors(source TEXT PRIMARY KEY,body BLOB NOT NULL);
 CREATE TABLE IF NOT EXISTS config(id INTEGER PRIMARY KEY CHECK(id=1),version INTEGER NOT NULL,body BLOB NOT NULL);
 CREATE TABLE IF NOT EXISTS mutations(id TEXT PRIMARY KEY,actor TEXT NOT NULL,body BLOB NOT NULL,created INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS incidents(id TEXT PRIMARY KEY,fingerprint TEXT NOT NULL UNIQUE,updated INTEGER NOT NULL,body BLOB NOT NULL);
 CREATE INDEX IF NOT EXISTS incidents_updated ON incidents(updated DESC);
 CREATE TABLE IF NOT EXISTS incident_evidence(incident_id TEXT NOT NULL,evidence_id TEXT NOT NULL,created INTEGER NOT NULL,PRIMARY KEY(incident_id,evidence_id));
 CREATE TABLE IF NOT EXISTS incident_impact(incident_id TEXT NOT NULL,kind TEXT NOT NULL,value TEXT NOT NULL,PRIMARY KEY(incident_id,kind,value));
 CREATE TABLE IF NOT EXISTS incident_transitions(id INTEGER PRIMARY KEY AUTOINCREMENT,incident_id TEXT NOT NULL,created INTEGER NOT NULL,body BLOB NOT NULL);
 CREATE TABLE IF NOT EXISTS notification_outbox(id TEXT PRIMARY KEY,incident_id TEXT NOT NULL,revision INTEGER NOT NULL,channel TEXT NOT NULL,target TEXT NOT NULL,kind TEXT NOT NULL,state TEXT NOT NULL DEFAULT 'pending',next_attempt INTEGER NOT NULL,lease_until INTEGER NOT NULL DEFAULT 0,lease_token TEXT NOT NULL DEFAULT '',attempts INTEGER NOT NULL DEFAULT 0,body BLOB NOT NULL,UNIQUE(incident_id,revision,channel,target,kind));
 CREATE INDEX IF NOT EXISTS outbox_ready ON notification_outbox(state,next_attempt,lease_until);
 CREATE INDEX IF NOT EXISTS outbox_latest ON notification_outbox(next_attempt DESC);
 CREATE TABLE IF NOT EXISTS message_map(incident_id TEXT NOT NULL,channel TEXT NOT NULL,target TEXT NOT NULL,message_id TEXT NOT NULL,revision INTEGER NOT NULL,PRIMARY KEY(incident_id,channel,target));
 CREATE TABLE IF NOT EXISTS rule_state(key TEXT PRIMARY KEY,body BLOB NOT NULL);
 `)
	if err != nil {
		s.Close()
		return nil, err
	}
	if err = os.Chmod(path, 0600); err != nil {
		s.Close()
		return nil, err
	}
	if ro, e := sql.Open("sqlite", "file:"+path+"?mode=ro&_pragma=busy_timeout(3000)"); e == nil {
		ro.SetMaxOpenConns(3)
		s.ro = ro
	}
	return s, nil
}
func (s *Store) Close() error {
	if s.ro != nil {
		s.ro.Close()
	}
	err := s.db.Close()
	syscall.Flock(int(s.lock.Fd()), syscall.LOCK_UN)
	s.lock.Close()
	return err
}
func (s *Store) Ingest(ctx context.Context, observations []domain.Observation, c domain.Cursor) error {
	for _, o := range observations {
		if err := o.Validate(); err != nil {
			return err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, o := range observations {
		b, e := json.Marshal(o)
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, "INSERT OR IGNORE INTO observations(source,event_id,occurred,body) VALUES(?,?,?,?)", o.Source, o.EventID, o.OccurredAt.UnixMilli(), b); e != nil {
			return e
		}
		if o.TaskID != "" && o.Quality != domain.Stale && o.Quality != domain.Unsupported {
			switch o.Kind {
			case "media.accepted", "media.pending", "media.completed", "media.failed", "media.cancelled", "billing.applied", "billing.failed", "billing.metadata_failed":
				// Preserve the first acceptance/completion so polling cannot reset SLA/grace.
				comparison := ">="
				if o.Kind == "media.accepted" || o.Kind == "media.completed" {
					comparison = "<"
				}
				scope := TaskKey(o)
				_, e = tx.ExecContext(ctx, "INSERT INTO task_facts VALUES(?,?,?,?) ON CONFLICT(scope,kind) DO UPDATE SET occurred=excluded.occurred,body=excluded.body WHERE excluded.occurred"+comparison+"task_facts.occurred", scope, o.Kind, o.OccurredAt.UnixMilli(), b)
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
	if _, err = tx.ExecContext(ctx, "INSERT INTO source_cursors VALUES(?,?) ON CONFLICT(source) DO UPDATE SET body=excluded.body", c.Source, b); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Cursor(ctx context.Context, source string) (domain.Cursor, error) {
	var c domain.Cursor
	var b []byte
	err := s.db.QueryRowContext(ctx, "SELECT body FROM source_cursors WHERE source=?", source).Scan(&b)
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
	return readJSON[domain.Cursor](ctx, s.db, "SELECT body FROM source_cursors ORDER BY source")
}

// Window returns the newest bounded window in chronological order and exposes truncation.
func (s *Store) Window(ctx context.Context, since time.Time, limit int) ([]domain.Observation, bool, error) {
	limit = bounded(limit)
	rows, err := readJSON[domain.Observation](ctx, s.db, "SELECT body FROM observations WHERE occurred>=? ORDER BY occurred DESC,seq DESC LIMIT ?", since.UnixMilli(), limit+1)
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
	defer rows.Close()
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
	err := s.db.QueryRowContext(ctx, "SELECT body FROM config WHERE id=1").Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DefaultConfig(), nil
	}
	var c domain.Config
	if err == nil {
		err = json.Unmarshal(b, &c)
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
	defer tx.Rollback()
	original, _ := json.Marshal(c)
	var prev []byte
	err = tx.QueryRowContext(ctx, "SELECT body FROM mutations WHERE id=?", c.RequestID).Scan(&prev)
	if err == nil {
		if string(prev) != string(original) {
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
	err = tx.QueryRowContext(ctx, "SELECT version,body FROM config WHERE id=1").Scan(&version, &oldBody)
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
	if _, err = tx.ExecContext(ctx, "INSERT INTO config VALUES(1,?,?) ON CONFLICT(id) DO UPDATE SET version=excluded.version,body=excluded.body", c.Version, b); err != nil {
		return err
	}
	rows, e := tx.QueryContext(ctx, "SELECT body FROM incidents WHERE json_extract(body,'$.status') NOT IN ('resolved','superseded')")
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
		if _, e = tx.ExecContext(ctx, "UPDATE incidents SET body=? WHERE id=?", raw, i.ID); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO incident_transitions(incident_id,created,body) VALUES(?,?,?)", i.ID, time.Now().UnixMilli(), raw); e != nil {
			return e
		}
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO mutations VALUES(?,?,?,?)", c.RequestID, c.Actor, original, time.Now().UnixMilli()); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Incidents(ctx context.Context) ([]domain.Incident, error) {
	return readJSON[domain.Incident](ctx, s.reader(), "SELECT body FROM incidents ORDER BY updated DESC LIMIT 1000")
}
func (s *Store) Incident(ctx context.Context, fingerprint string) (*domain.Incident, error) {
	var b []byte
	err := s.db.QueryRowContext(ctx, "SELECT body FROM incidents WHERE fingerprint=? OR id=?", fingerprint, fingerprint).Scan(&b)
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

// SaveFinding atomically records incident state, evidence deduplication and all target deliveries.
func (s *Store) SaveFinding(ctx context.Context, i domain.Incident, ds []domain.Delivery, evidenceID string, findings ...*domain.Finding) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if evidenceID != "" {
		r, e := tx.ExecContext(ctx, "INSERT OR IGNORE INTO incident_evidence VALUES(?,?,?)", i.ID, evidenceID, i.LastSeen.UnixMilli())
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
					if _, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO incident_impact VALUES(?,?,?)", i.ID, kind, value); err != nil {
						return err
					}
				}
			}
		}
	}
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM incident_impact WHERE incident_id=? AND kind='request'", i.ID).Scan(&i.RequestCount); err != nil {
		return err
	}
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM incident_impact WHERE incident_id=? AND kind='account'", i.ID).Scan(&i.AccountCount); err != nil {
		return err
	}
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM incident_impact WHERE incident_id=? AND kind='user'", i.ID).Scan(&i.UserCount); err != nil {
		return err
	}
	b, err := json.Marshal(i)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO incidents VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET fingerprint=excluded.fingerprint,updated=excluded.updated,body=excluded.body", i.ID, i.Fingerprint, i.LastSeen.UnixMilli(), b); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO incident_transitions(incident_id,created,body) VALUES(?,?,?)", i.ID, i.LastSeen.UnixMilli(), b); err != nil {
		return err
	}
	for _, d := range ds {
		if d.Kind == "update" && d.State != "shadow" {
			_, err = tx.ExecContext(ctx, `UPDATE notification_outbox SET state='superseded',body=json_set(body,'$.state','superseded') WHERE incident_id=? AND channel=? AND target=? AND kind='update' AND state='pending' AND revision<?`, d.IncidentID, d.Channel, d.Target, d.Revision)
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
		if _, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO notification_outbox(id,incident_id,revision,channel,target,kind,state,next_attempt,body) VALUES(?,?,?,?,?,?,?,?,?)", d.ID, d.IncidentID, d.Revision, d.Channel, d.Target, d.Kind, d.State, d.NextAttempt.UnixMilli(), b); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) EvidenceExists(ctx context.Context, id, key string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM incident_evidence WHERE incident_id=? AND evidence_id=?", id, key).Scan(&n)
	return n > 0, err
}
func (s *Store) Lease(ctx context.Context, now time.Time, duration time.Duration) (*domain.Delivery, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var b []byte
	var d domain.Delivery
	var attempts int
	// Keep revisions ordered per recipient, including unexpired leases; dead letters do not block updates.
	err = tx.QueryRowContext(ctx, `SELECT o.body,o.attempts FROM notification_outbox o WHERE o.state IN ('pending','leased') AND o.next_attempt<=? AND o.lease_until<=? AND NOT EXISTS(SELECT 1 FROM notification_outbox older WHERE older.incident_id=o.incident_id AND older.channel=o.channel AND older.target=o.target AND older.revision<o.revision AND older.state IN ('pending','leased')) ORDER BY o.next_attempt,o.revision LIMIT 1`, now.UnixMilli(), now.UnixMilli()).Scan(&b, &attempts)
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
	d.State = "leased"
	d.LeaseUntil = now.Add(duration)
	d.LeaseToken = uuid.NewString()
	if _, err = tx.ExecContext(ctx, "UPDATE notification_outbox SET state='leased',lease_until=?,lease_token=?,attempts=? WHERE id=?", d.LeaseUntil.UnixMilli(), d.LeaseToken, d.Attempts, d.ID); err != nil {
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
	defer tx.Rollback()
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	r, err := tx.ExecContext(ctx, "UPDATE notification_outbox SET state=?,next_attempt=?,lease_until=0,body=? WHERE id=? AND lease_token=? AND state='leased'", d.State, d.NextAttempt.UnixMilli(), b, d.ID, d.LeaseToken)
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return errors.New("expired delivery lease")
	}
	if d.State == "accepted" && d.MessageID != "" {
		_, err = tx.ExecContext(ctx, "INSERT INTO message_map VALUES(?,?,?,?,?) ON CONFLICT(incident_id,channel,target) DO UPDATE SET message_id=excluded.message_id,revision=excluded.revision WHERE excluded.revision>=message_map.revision", d.IncidentID, d.Channel, d.Target, d.MessageID, d.Revision)
		if err != nil {
			return err
		}
	}
	for _, next := range fallback {
		raw, e := json.Marshal(next)
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, "INSERT OR IGNORE INTO notification_outbox(id,incident_id,revision,channel,target,kind,state,next_attempt,body) VALUES(?,?,?,?,?,?,?,?,?)", next.ID, next.IncidentID, next.Revision, next.Channel, next.Target, next.Kind, next.State, next.NextAttempt.UnixMilli(), raw)
		if e != nil {
			return e
		}
	}
	return tx.Commit()
}
func (s *Store) MessageID(ctx context.Context, i, channel, target string) string {
	var id string
	_ = s.db.QueryRowContext(ctx, "SELECT message_id FROM message_map WHERE incident_id=? AND channel=? AND target=?", i, channel, target).Scan(&id)
	return id
}
func (s *Store) Deliveries(ctx context.Context) ([]domain.Delivery, error) {
	return readJSON[domain.Delivery](ctx, s.db, "SELECT json_set(body,'$.state',state,'$.attempts',attempts) FROM notification_outbox ORDER BY next_attempt DESC LIMIT 1000")
}

// DeliveryLog is the admin list: no per-delivery incident copy, read off the writer connection.
func (s *Store) DeliveryLog(ctx context.Context) ([]domain.Delivery, error) {
	return readJSON[domain.Delivery](ctx, s.reader(), "SELECT json_remove(json_set(body,'$.state',state,'$.attempts',attempts),'$.payload') FROM notification_outbox ORDER BY next_attempt DESC LIMIT 300")
}
func (s *Store) State(ctx context.Context, key string, dest any) error {
	var b []byte
	err := s.db.QueryRowContext(ctx, "SELECT body FROM rule_state WHERE key=?", key).Scan(&b)
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
	_, err = s.db.ExecContext(ctx, "INSERT INTO rule_state VALUES(?,?) ON CONFLICT(key) DO UPDATE SET body=excluded.body", key, b)
	return err
}
func (s *Store) Backup(ctx context.Context, path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("backup path must be absolute")
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		return errors.New("backup destination already exists or inaccessible")
	}
	output, createErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if createErr != nil {
		return createErr
	}
	if createErr = output.Close(); createErr != nil {
		return createErr
	}
	// VACUUM INTO includes committed WAL pages and produces a consistent standalone snapshot.
	_, err := s.db.ExecContext(ctx, "VACUUM INTO '"+strings.ReplaceAll(path, "'", "''")+"'")
	if err != nil {
		return err
	}
	return os.Chmod(path, 0600)
}

// HistoryRetention bounds observations, incident history, evidence, finished
// deliveries and closed incidents. Open incidents are never removed.
const HistoryRetention = 3 * 24 * time.Hour

func (s *Store) Retain(ctx context.Context, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Raw evidence and history are kept for HistoryRetention only so the state file
	// stays small; task facts and config audit are tiny and keep their longer window.
	history := now.Add(-HistoryRetention)
	for _, q := range []struct {
		sql string
		cut time.Time
	}{
		{"DELETE FROM observations WHERE occurred<?", history},
		{"DELETE FROM incident_transitions WHERE created<?", history},
		{"DELETE FROM incident_evidence WHERE created<? AND NOT EXISTS (SELECT 1 FROM notification_outbox o WHERE o.incident_id=incident_evidence.incident_id AND o.state IN ('pending','leased'))", history},
		{"DELETE FROM task_facts WHERE occurred<?", now.Add(-90 * 24 * time.Hour)},
		{"DELETE FROM mutations WHERE created<?", now.Add(-90 * 24 * time.Hour)},
		{"DELETE FROM notification_outbox WHERE state IN ('accepted','shadow','dead','superseded','suppressed') AND next_attempt<?", history},
	} {
		if _, err = tx.ExecContext(ctx, q.sql, q.cut.UnixMilli()); err != nil {
			return fmt.Errorf("retention: %w", err)
		}
	}
	// Preserve any incident whose delivery is still actionable. Completed,
	// closed history beyond retention can release its evidence/impact indexes.
	if _, err = tx.ExecContext(ctx, `DELETE FROM incidents WHERE updated<? AND json_extract(body,'$.status') IN ('resolved','superseded') AND NOT EXISTS (SELECT 1 FROM notification_outbox o WHERE o.incident_id=incidents.id AND o.state IN ('pending','leased'))`, history.UnixMilli()); err != nil {
		return err
	}
	for _, table := range []string{"incident_evidence", "incident_impact", "message_map", "incident_transitions"} {
		if _, err = tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE NOT EXISTS (SELECT 1 FROM incidents i WHERE i.id="+table+".incident_id)"); err != nil {
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
