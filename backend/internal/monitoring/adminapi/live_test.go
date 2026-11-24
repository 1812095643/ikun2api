package adminapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/incidents"
	"github.com/MACOS-DO/sub4api/internal/monitoring/notify"
	"github.com/MACOS-DO/sub4api/internal/monitoring/store"
)

type fakeNames struct{ calls int }

func (f *fakeNames) Resolve(_ context.Context, refs map[string][]int64) map[string]notify.Identity {
	f.calls++
	out := map[string]notify.Identity{}
	names := map[string]notify.Identity{"user:6": {Name: "小明", Email: "xm@example.com"}, "user:9": {Name: "阿强"}, "key:34": {Name: "开发 Key", Owner: 9}, "account:31": {Name: "apipros"}, "group:23": {Name: "Claude Max"}}
	for kind, ids := range refs {
		for _, id := range ids {
			k := kind + ":" + itoa(id)
			if v, ok := names[k]; ok {
				out[k] = v
			}
		}
	}
	return out
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

func obs(kind, req string, at time.Time, f func(*domain.Observation)) domain.Observation {
	o := domain.Observation{Schema: 1, Site: "t", Source: "gateway", EventID: kind + req + at.String(), Kind: kind, RequestID: req, OccurredAt: at, ObservedAt: at, Quality: domain.Fresh, Metrics: map[string]float64{}}
	if f != nil {
		f(&o)
	}
	return o
}

func TestLiveViewBucketsInflightAndProblemAccounts(t *testing.T) {
	now := time.Date(2026, 11, 24, 8, 30, 20, 0, time.UTC)
	rows := []domain.Observation{
		obs("request.finished", "a", now.Add(-50*time.Minute), func(o *domain.Observation) {
			o.Outcome = "success"
			o.AccountID = 31
			o.Metrics["first_effective_ms"] = 2000
		}),
		obs("request.finished", "b", now.Add(-10*time.Second), func(o *domain.Observation) { o.Outcome = "failed"; o.AccountID = 31 }),
		obs("request.cancelled", "c", now.Add(-20*time.Second), func(o *domain.Observation) { o.Outcome = "cancelled" }),
		obs("request.finished", "d", now.Add(-30*time.Second), func(o *domain.Observation) { o.Outcome = "unknown"; o.AccountID = 7 }),
		obs("attempt.failed", "b", now.Add(-15*time.Second), func(o *domain.Observation) { o.AccountID = 31; o.UpstreamStatus = 429 }),
		obs("attempt.failed", "b", now.Add(-14*time.Second), func(o *domain.Observation) { o.AccountID = 31 }),
		obs("request.active", "live1", now.Add(-5*time.Second), func(o *domain.Observation) {
			o.UserID = 6
			o.AccountID = 31
			o.Model = "claude-opus-5-5"
			o.Metrics["elapsed_ms"] = 30000
		}),
		obs("request.active", "stale", now.Add(-2*time.Minute), nil),
		obs("request.active", "b", now.Add(-20*time.Second), nil), // finished later: not in flight
		obs("request.finished", "old", now.Add(-2*time.Hour), func(o *domain.Observation) { o.Outcome = "failed" }),
	}
	v := liveView(rows, now)
	buckets := v["buckets"].([]liveBucket)
	if len(buckets) != 60 || !buckets[59].At.Equal(now.Truncate(time.Minute)) {
		t.Fatal(len(buckets), buckets[59].At)
	}
	totals := v["totals"].(map[string]int)
	if totals["requests"] != 4 || totals["failed"] != 1 || totals["cancelled"] != 1 || totals["succeeded"] != 2 || totals["upstream_errors"] != 2 {
		t.Fatalf("%v", totals)
	}
	if b := buckets[59]; b.Requests != 2 || b.Failed != 1 || b.Cancelled != 1 || b.Upstream != 2 || buckets[58].Succeeded != 1 {
		t.Fatalf("last minute %+v", b)
	}
	inflight := v["inflight"].([]liveRequest)
	if v["inflight_count"] != 1 || inflight[0].Request != "live1" || inflight[0].ElapsedMS < 35000 || !inflight[0].Waiting {
		t.Fatalf("%+v", inflight)
	}
	p := v["problem_accounts"].([]liveAccount)
	if len(p) != 1 || p[0].AccountID != 31 || p[0].Failed != 1 || p[0].Upstream != 2 || p[0].Statuses["429"] != 1 || p[0].Statuses["传输错误"] != 1 || p[0].Requests != 2 {
		t.Fatalf("%+v", p)
	}
	if v["first_output_p95_ms"].(float64) != 2000 {
		t.Fatal(v["first_output_p95_ms"])
	}
}

func TestDetailAndSummaryCarryNamesImpactAndHits(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	i := domain.Incident{ID: "inc-1", Fingerprint: "fp", Rule: "U02", Revision: 1, Status: "firing", Severity: "P1", Scope: "account:31/group:23/model:claude-opus-5-5", LastSeen: now, Sample: &domain.IncidentSample{UserID: 6, KeyID: 34}}
	for n, user := range []int64{6, 9} {
		f := &domain.Finding{RequestID: "r" + itoa(int64(n)), UserID: user, AccountID: 31}
		if e = s.SaveFinding(ctx, i, nil, "ev-"+itoa(int64(n))+":false", f); e != nil {
			t.Fatal(e)
		}
	}
	if e = s.SaveFinding(ctx, i, nil, "ok:healthy"); e != nil {
		t.Fatal(e)
	}
	names := &fakeNames{}
	api := (&API{Store: s, Manager: &incidents.Manager{Store: s}, Names: names}).Handler()
	get := func(path string) map[string]any {
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("X-Ops-Actor", "admin:1")
		w := httptest.NewRecorder()
		api.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal(path, w.Code, w.Body.String())
		}
		var out map[string]any
		json.Unmarshal(w.Body.Bytes(), &out)
		return out["data"].(map[string]any)
	}
	d := get("/v1/incidents/inc-1")
	n := d["names"].(map[string]any)
	for _, k := range []string{"user:6", "user:9", "key:34", "account:31", "group:23"} {
		if n[k] == nil {
			t.Fatalf("missing %s in %v", k, n)
		}
	}
	impact := d["impact"].(map[string]any)
	if len(impact["users"].([]any)) != 2 || len(impact["accounts"].([]any)) != 1 {
		t.Fatal(impact)
	}
	hits := d["hits"].([]any)
	healthy := 0
	for _, h := range hits {
		if h.(map[string]any)["healthy"] == true {
			healthy++
		}
	}
	if len(hits) != 3 || healthy != 1 {
		t.Fatal(hits)
	}
	sum := get("/v1/summary")
	if sum["names"].(map[string]any)["account:31"] == nil {
		t.Fatal(sum["names"])
	}
	live := get("/v1/live")
	if live["inflight_count"].(float64) != 0 || len(live["buckets"].([]any)) != 60 {
		t.Fatal(live)
	}
	raw, _ := json.Marshal(d)
	if strings.Contains(string(raw), "api_key") {
		t.Fatal("unexpected secret field")
	}
}

func TestSummaryCachedUntilAWrite(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	open := domain.Incident{ID: "a", Fingerprint: "a", Rule: "U01", Revision: 1, Status: "firing", Severity: "P1", LastSeen: now}
	old := domain.Incident{ID: "b", Fingerprint: "b", Rule: "U01", Revision: 1, Status: "resolved", Severity: "P1", LastSeen: now.Add(-5 * 24 * time.Hour)}
	for _, i := range []domain.Incident{open, old} {
		if e = s.SaveIncident(ctx, i, nil); e != nil {
			t.Fatal(e)
		}
	}
	api := (&API{Store: s, Manager: &incidents.Manager{Store: s}}).Handler()
	count := func() int {
		r := httptest.NewRequest("GET", "/v1/summary", nil)
		r.Header.Set("X-Ops-Actor", "admin:1")
		w := httptest.NewRecorder()
		api.ServeHTTP(w, r)
		var out struct {
			Data struct {
				Incidents []domain.Incident `json:"incidents"`
			} `json:"data"`
		}
		json.Unmarshal(w.Body.Bytes(), &out)
		return len(out.Data.Incidents)
	}
	if n := count(); n != 1 {
		t.Fatalf("closed incidents older than 3 days must not be listed: %d", n)
	}
	second := domain.Incident{ID: "c", Fingerprint: "c", Rule: "U01", Revision: 1, Status: "firing", Severity: "P1", LastSeen: now}
	if e = s.SaveIncident(ctx, second, nil); e != nil {
		t.Fatal(e)
	}
	if n := count(); n != 1 {
		t.Fatalf("within the cache window the same page is served: %d", n)
	}
	r := httptest.NewRequest("POST", "/v1/incidents/a/control", strings.NewReader(`{}`))
	r.Header.Set("X-Ops-Actor", "admin:1")
	api.ServeHTTP(httptest.NewRecorder(), r)
	if n := count(); n != 2 {
		t.Fatalf("a write must clear the cache: %d", n)
	}
}
