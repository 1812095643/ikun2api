package runtime

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/detectors"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/incidents"
	"github.com/MACOS-DO/sub4api/internal/monitoring/notify"
	"github.com/MACOS-DO/sub4api/internal/monitoring/sources"
	"github.com/MACOS-DO/sub4api/internal/monitoring/store"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type replaySource struct {
	view       string
	rows       []domain.Observation
	activation time.Time
}

func (s *replaySource) Name() string { return "postgres." + s.view }
func (s *replaySource) Poll(_ context.Context, c domain.Cursor, now time.Time) (sources.Batch, error) {
	out := []domain.Observation{}
	for _, o := range s.rows {
		if o.Source != s.Name() || o.OccurredAt.After(now) || o.OccurredAt.Before(now.Add(-2*time.Minute)) {
			continue
		}
		if o.OccurredAt.Before(s.activation) {
			o.Quality = domain.Historical
		}
		o.ObservedAt = now
		out = append(out, o)
	}
	return sources.Batch{Observations: out, Cursor: domain.Cursor{Source: s.Name(), LastSuccess: now, Quality: domain.Partial}}, nil
}

type loopbackTransport struct {
	base      *url.URL
	transport http.RoundTripper
}

func (r loopbackTransport) RoundTrip(q *http.Request) (*http.Response, error) {
	if q.URL.Host != "open.feishu.cn" {
		return nil, fmt.Errorf("unexpected origin")
	}
	copy := q.Clone(q.Context())
	u := *q.URL
	copy.URL = &u
	copy.URL.Scheme = r.base.Scheme
	copy.URL.Host = r.base.Host
	return r.transport.RoundTrip(copy)
}

func TestReal24HourPipelineMergeAndDelivery(t *testing.T) {
	rows := readRealReplay(t)
	ctx := context.Background()
	start, _ := time.Parse(time.RFC3339, "2026-11-12T21:41:42Z")
	end := start.Add(24 * time.Hour)
	path := filepath.Join(t.TempDir(), "replay.db")
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	audit, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer audit.Close()
	count := func(q string, args ...any) int {
		var n int
		if err := audit.QueryRow(q, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	cfg := domain.DefaultConfig()
	cfg.Site = "replay"
	cfg.Actor = "admin:1"
	cfg.RequestID = "isolated-replay"
	cfg.Shadow = false
	cfg.FeishuTargets = []string{"oc_replay"}
	for _, r := range detectors.Catalog {
		cfg.Rules[r.ID] = domain.RuleConfig{Enabled: true, Owner: "monitor", Version: 1}
	}
	if err = s.SaveConfig(ctx, cfg, 0); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	creates, patches, retries := 0, 0, 0
	messages := map[string]bool{}
	uuids := map[string]string{}
	limitedUUID := ""
	lastCards := map[string]json.RawMessage{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "tenant_access_token") {
			fmt.Fprint(w, `{"code":0,"tenant_access_token":"isolated-token","expire":7200}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer isolated-token" {
			t.Error("missing token")
		}
		var b map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			t.Error(err)
		}
		var content string
		if err := json.Unmarshal(b["content"], &content); err != nil {
			t.Error(err)
		}
		var card map[string]any
		if json.Unmarshal([]byte(content), &card) != nil || card["schema"] != "2.0" {
			t.Error("invalid card")
		}
		if r.Method == "PATCH" {
			id := strings.TrimPrefix(r.URL.Path, "/open-apis/im/v1/messages/")
			if !messages[id] {
				t.Error("patch without original message")
			}
			patches++
			lastCards[id] = []byte(content)
			fmt.Fprint(w, `{"code":0}`)
			return
		}
		var uuid string
		json.Unmarshal(b["uuid"], &uuid)
		if limitedUUID == "" {
			limitedUUID = uuid
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(429)
			fmt.Fprint(w, `{"code":99991400}`)
			return
		}
		if uuid == limitedUUID {
			retries++
		}
		id := uuids[uuid]
		if id == "" {
			creates++
			id = fmt.Sprintf("om_replay_%d", creates)
			uuids[uuid] = id
			messages[id] = true
		}
		lastCards[id] = []byte(content)
		fmt.Fprintf(w, `{"code":0,"data":{"message_id":%q}}`, id)
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL)
	sender := notify.NewFeishuWithTransport("fixture-app", "fixture-secret", loopbackTransport{base, http.DefaultTransport})
	clock := &fakeClock{start}
	manager := &incidents.Manager{Store: s}
	eval := &Evaluator{Store: s, Manager: manager, Clock: clock}
	worker := &notify.Worker{Store: s, Senders: map[string]notify.Sender{"feishu": sender}}
	var replayPG *sql.DB
	if dsn := os.Getenv("TEST_MONITOR_REPLAY_DSN"); dsn != "" {
		replayPG, err = sources.OpenPostgres(dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer replayPG.Close()
	}
	collectors := []*Collector{}
	for _, v := range []string{"errors", "attempts", "usage", "system"} {
		var source sources.EvidenceSource = &replaySource{view: v, rows: rows, activation: start}
		if replayPG != nil {
			source = &sources.Postgres{DB: replayPG, View: v, Site: "replay", ActivatedAt: start}
		}
		collectors = append(collectors, &Collector{Store: s, Site: "replay", Clock: clock, Source: source})
	}
	drain := func() {
		for n := 0; n < 10000; n++ {
			due := count("SELECT count(*) FROM notification_outbox WHERE state='pending' AND next_attempt<=?", clock.Now().UnixMilli())
			if due == 0 {
				return
			}
			if err = worker.Tick(ctx, clock.Now()); err != nil {
				t.Fatal(err)
			}
		}
		t.Fatal("queue did not drain")
	}
	for now := start.Add(time.Minute); !now.After(end); now = now.Add(time.Minute) {
		clock.at = now
		for _, c := range collectors {
			for page := 0; page < 20; page++ {
				if err = c.Poll(ctx); err != nil {
					t.Fatal(err)
				}
				cur, e := s.Cursor(ctx, c.Source.Name())
				if e != nil {
					t.Fatal(e)
				}
				var position struct {
					Boundary time.Time `json:"boundary"`
				}
				if cur.Watermark != "" {
					if e = json.Unmarshal([]byte(cur.Watermark), &position); e != nil {
						t.Fatal(e)
					}
				}
				if position.Boundary.IsZero() {
					break
				}
				if page == 19 {
					t.Fatal("source page did not drain")
				}
			}
		}
		if err = eval.Poll(ctx); err != nil {
			t.Fatal(err)
		}
		drain()
	}
	pending := count("SELECT count(*) FROM notification_outbox WHERE state IN ('pending','leased','dead')")
	if pending != 0 {
		t.Fatalf("unaccepted queue=%d", pending)
	}
	observed := count("SELECT count(*) FROM observations WHERE source LIKE 'postgres.%'")
	if observed != len(rows) {
		t.Fatalf("replay ingestion lost rows: %d vs %d", observed, len(rows))
	}
	before := count("SELECT count(*) FROM notification_outbox")
	evidenceBefore := count("SELECT count(*) FROM incident_evidence")
	// Restart SQLite/manager and replay identical evidence; durable dedup must hold.
	s.Close()
	s, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	manager = &incidents.Manager{Store: s}
	eval = &Evaluator{Store: s, Manager: manager, Clock: clock}
	worker.Store = s
	for _, v := range []string{"errors", "attempts", "usage", "system"} {
		subset := []domain.Observation{}
		for _, o := range rows {
			if o.Source == "postgres."+v {
				subset = append(subset, o)
			}
		}
		if err = s.Ingest(ctx, subset, domain.Cursor{Source: "postgres." + v, LastSuccess: end, Quality: domain.Partial}); err != nil {
			t.Fatal(err)
		}
	}
	if err = eval.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	drain()
	if count("SELECT count(*) FROM notification_outbox") != before || count("SELECT count(*) FROM incident_evidence") != evidenceBefore {
		t.Fatal("restart/replay generated duplicate evidence or notifications")
	}
	merged := count("SELECT count(*) FROM incidents WHERE json_extract(body,'$.count')>1")
	if merged == 0 || patches == 0 || retries != 1 {
		t.Fatalf("merge/update/retry absent merged=%d patches=%d retries=%d", merged, patches, retries)
	}
	if count("SELECT count(*) FROM incidents WHERE json_extract(body,'$.rule_id')='Q04'") != 0 {
		t.Fatal("daily quota notified")
	}
	if count("SELECT sum(json_extract(body,'$.count')) FROM incidents WHERE json_extract(body,'$.rule_id')='B03'") != 2 {
		t.Fatal("real billing metadata errors were dropped by the runtime")
	}
	report := map[string]any{"postgres_adapter": replayPG != nil, "window_start": start, "window_end": end, "observations": observed, "incidents": count("SELECT count(*) FROM incidents"), "merged_incidents": merged, "outbox": before, "accepted": count("SELECT count(*) FROM notification_outbox WHERE state='accepted'"), "creates": creates, "updates": patches, "injected_429_retries": retries, "pending_or_failed": pending, "restart_replay_no_duplicates": true, "real_feishu_receipt": false}
	rr, err := audit.Query("SELECT json_extract(body,'$.rule_id'),count(*),sum(json_extract(body,'$.count')) FROM incidents GROUP BY 1")
	if err != nil {
		t.Fatal(err)
	}
	byRule := map[string]any{}
	for rr.Next() {
		var rule string
		var n, hits int
		if err = rr.Scan(&rule, &n, &hits); err != nil {
			t.Fatal(err)
		}
		byRule[rule] = map[string]int{"incidents": n, "evidence_hits": hits}
	}
	rr.Close()
	report["by_rule"] = byRule
	verifySyntheticRecovery(t, rows, sender)
	report["synthetic_recovery_accepted"] = true
	report["first_install_history_suppression"] = true
	if dest := os.Getenv("TEST_MONITOR_REPLAY_REPORT"); dest != "" {
		b, _ := json.MarshalIndent(report, "", "  ")
		if err = os.WriteFile(dest, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := json.Marshal(report)
	t.Log(string(b))
	// Real historical data at first install must stay archived, not become new alerts.
	hist, e := store.Open(filepath.Join(t.TempDir(), "historical.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer hist.Close()
	cfg.Version = 0
	cfg.RequestID = "historical"
	if e = hist.SaveConfig(ctx, cfg, 0); e != nil {
		t.Fatal(e)
	}
	hrows := append([]domain.Observation(nil), rows...)
	for i := range hrows {
		hrows[i].Quality = domain.Historical
	}
	if e = hist.Ingest(ctx, hrows, domain.Cursor{Source: "replay", LastSuccess: end, Quality: domain.Partial}); e != nil {
		t.Fatal(e)
	}
	he := Evaluator{Store: hist, Manager: &incidents.Manager{Store: hist}, Clock: clock}
	for n := 0; n < 12; n++ {
		if e = he.Poll(ctx); e != nil {
			t.Fatal(e)
		}
	}
	hs, e := hist.Incidents(ctx)
	if e != nil || len(hs) != 0 {
		t.Fatalf("history generated alerts %d %v", len(hs), e)
	}
}

// Recovery cannot be inferred from missing historical errors. This separate
// synthetic extension supplies explicit successful terminal observations.
func verifySyntheticRecovery(t *testing.T, rows []domain.Observation, sender notify.Sender) {
	t.Helper()
	ctx := context.Background()
	var seed domain.Observation
	for _, o := range rows {
		if o.Kind == "request.error" && o.UpstreamStatus == 429 {
			seed = o
			break
		}
	}
	if seed.EventID == "" {
		t.Fatal("no real upstream seed")
	}
	s, err := store.Open(filepath.Join(t.TempDir(), "synthetic-recovery.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cfg := domain.DefaultConfig()
	cfg.Site = "replay"
	cfg.Actor = "admin:1"
	cfg.RequestID = "synthetic-recovery"
	cfg.Shadow = false
	cfg.FeishuTargets = []string{"oc_replay"}
	cfg.Rules["U01"] = domain.RuleConfig{Enabled: true, Owner: "monitor", Version: 1}
	if err = s.SaveConfig(ctx, cfg, 0); err != nil {
		t.Fatal(err)
	}
	clock := &fakeClock{seed.OccurredAt}
	eval := Evaluator{Store: s, Manager: &incidents.Manager{Store: s}, Clock: clock}
	worker := notify.Worker{Store: s, Senders: map[string]notify.Sender{"feishu": sender}}
	ingest := func(o domain.Observation) {
		if err = s.Ingest(ctx, []domain.Observation{o}, domain.Cursor{Source: seed.Source, LastSuccess: clock.Now(), Quality: domain.Partial}); err != nil {
			t.Fatal(err)
		}
		if err = eval.Poll(ctx); err != nil {
			t.Fatal(err)
		}
		for n := 0; n < 5; n++ {
			if err = worker.Tick(ctx, clock.Now()); err != nil {
				t.Fatal(err)
			}
		}
	}
	ingest(seed)
	for n := 1; n <= 6; n++ {
		clock.at = seed.OccurredAt.Add(time.Duration(n) * time.Minute)
		o := seed
		o.Source = "synthetic.gateway"
		o.EventID = fmt.Sprint(n)
		o.RequestID = "synthetic-success-" + fmt.Sprint(n)
		o.Kind = "request.finished"
		o.Reason = ""
		o.UpstreamStatus = 0
		o.Outcome = "success"
		o.Quality = domain.Fresh
		o.OccurredAt = clock.Now()
		o.ObservedAt = clock.Now()
		o.Metrics = map[string]float64{"terminal": 1, "has_output": 1, "first_effective_ms": 100}
		ingest(o)
	}
	ds, err := s.Deliveries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	resolved := false
	for _, d := range ds {
		if d.Payload.Rule == "U01" && d.Kind == "resolved" && d.State == "accepted" {
			resolved = true
		}
	}
	if !resolved {
		t.Fatal("explicit synthetic recovery did not reach HTTP receiver")
	}
}
