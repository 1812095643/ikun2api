package adminapi

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/notify"
)

// NameResolver turns user/key/account/group IDs into display labels.
type NameResolver interface {
	Resolve(ctx context.Context, refs map[string][]int64) map[string]notify.Identity
}

type refSet map[string]map[int64]bool

func (r refSet) add(kind string, id int64) {
	if id <= 0 || !map[string]bool{"user": true, "key": true, "account": true, "group": true}[kind] {
		return
	}
	if r[kind] == nil {
		r[kind] = map[int64]bool{}
	}
	r[kind][id] = true
}

// addScope collects IDs from scopes like "account:31/group:23/model:x".
func (r refSet) addScope(scope string) {
	for _, part := range strings.Split(scope, "/") {
		kind, value, ok := strings.Cut(part, ":")
		if !ok {
			continue
		}
		if id, err := strconv.ParseInt(value, 10, 64); err == nil {
			r.add(kind, id)
		}
	}
}

func (r refSet) addIncident(i domain.Incident) {
	r.addScope(i.Scope)
	r.add("group", i.GroupID)
	if s := i.Sample; s != nil {
		r.add("user", s.UserID)
		r.add("key", s.KeyID)
		r.add("account", s.AccountID)
		r.add("group", s.GroupID)
	}
}

func (a *API) names(ctx context.Context, r refSet) map[string]notify.Identity {
	if a.Names == nil || len(r) == 0 {
		return map[string]notify.Identity{}
	}
	refs := map[string][]int64{}
	for kind, ids := range r {
		for id := range ids {
			refs[kind] = append(refs[kind], id)
		}
	}
	out := a.Names.Resolve(ctx, refs)
	// A key's owner is the user who is actually affected; resolve that too.
	owners := refSet{}
	for k, v := range out {
		if strings.HasPrefix(k, "key:") && v.Owner > 0 {
			if _, ok := out["user:"+strconv.FormatInt(v.Owner, 10)]; !ok {
				owners.add("user", v.Owner)
			}
		}
	}
	if len(owners) > 0 {
		more := map[string][]int64{}
		for id := range owners["user"] {
			more["user"] = append(more["user"], id)
		}
		for k, v := range a.Names.Resolve(ctx, more) {
			out[k] = v
		}
	}
	return out
}

type liveBucket struct {
	At        time.Time `json:"at"`
	Requests  int       `json:"requests"`
	Succeeded int       `json:"succeeded"`
	Failed    int       `json:"failed"`
	Cancelled int       `json:"cancelled"`
	Upstream  int       `json:"upstream_errors"`
}

type liveRequest struct {
	Request   string  `json:"request"`
	Model     string  `json:"model"`
	UserID    int64   `json:"user_id"`
	KeyID     int64   `json:"key_id"`
	AccountID int64   `json:"account_id"`
	GroupID   int64   `json:"group_id"`
	ElapsedMS float64 `json:"elapsed_ms"`
	Waiting   bool    `json:"waiting_first_output"`
}

type liveAccount struct {
	AccountID int64          `json:"account_id"`
	Requests  int            `json:"requests"`
	Failed    int            `json:"failed"`
	Upstream  int            `json:"upstream_errors"`
	Statuses  map[string]int `json:"statuses"`
}

// liveView summarises the last hour of gateway lifecycle events per minute.
func liveView(rows []domain.Observation, now time.Time) map[string]any {
	const minutes = 60
	start := now.Truncate(time.Minute).Add(-(minutes - 1) * time.Minute)
	buckets := make([]liveBucket, minutes)
	for n := range buckets {
		buckets[n].At = start.Add(time.Duration(n) * time.Minute)
	}
	slot := func(t time.Time) int {
		n := int(t.Sub(start) / time.Minute)
		if n < 0 || n >= minutes {
			return -1
		}
		return n
	}
	type state struct {
		last domain.Observation
		done bool
	}
	requests := map[string]*state{}
	accounts := map[int64]*liveAccount{}
	acct := func(id int64) *liveAccount {
		if accounts[id] == nil {
			accounts[id] = &liveAccount{AccountID: id, Statuses: map[string]int{}}
		}
		return accounts[id]
	}
	firsts := []float64{}
	for _, o := range rows {
		n := slot(o.OccurredAt)
		switch o.Kind {
		case "request.started", "request.active":
			st := requests[o.RequestID]
			if st == nil {
				st = &state{}
				requests[o.RequestID] = st
			}
			if !st.done {
				st.last = o
			}
		case "request.finished", "request.cancelled":
			st := requests[o.RequestID]
			if st == nil {
				st = &state{}
				requests[o.RequestID] = st
			}
			st.done, st.last = true, o
			if n < 0 {
				continue
			}
			b := &buckets[n]
			b.Requests++
			switch {
			case o.Kind == "request.cancelled" || o.Outcome == "cancelled":
				b.Cancelled++
			case o.Outcome == "failed":
				b.Failed++
			default:
				b.Succeeded++ // success, or completed with incomplete protocol evidence
			}
			if o.AccountID > 0 {
				a := acct(o.AccountID)
				a.Requests++
				if o.Outcome == "failed" {
					a.Failed++
				}
			}
			if o.Outcome == "success" && o.Has("first_effective_ms") && o.N("first_effective_ms") > 0 {
				firsts = append(firsts, o.N("first_effective_ms"))
			}
		case "attempt.failed":
			if n < 0 {
				continue
			}
			buckets[n].Upstream++
			if o.AccountID > 0 {
				a := acct(o.AccountID)
				a.Upstream++
				code := "传输错误"
				if o.UpstreamStatus > 0 {
					code = strconv.Itoa(o.UpstreamStatus)
				}
				a.Statuses[code]++
			}
		}
	}
	inflight := []liveRequest{}
	for id, st := range requests {
		o := st.last
		if st.done || now.Sub(o.ObservedAt) > 45*time.Second || id == "" {
			continue
		}
		elapsed := o.N("elapsed_ms") + float64(now.Sub(o.ObservedAt)/time.Millisecond)
		inflight = append(inflight, liveRequest{Request: id, Model: o.Model, UserID: o.UserID, KeyID: o.KeyID, AccountID: o.AccountID, GroupID: o.GroupID, ElapsedMS: elapsed, Waiting: o.N("has_output") != 1})
	}
	sort.Slice(inflight, func(a, b int) bool { return inflight[a].ElapsedMS > inflight[b].ElapsedMS })
	problem := []liveAccount{}
	for _, a := range accounts {
		if a.Failed > 0 || a.Upstream > 0 {
			problem = append(problem, *a)
		}
	}
	sort.Slice(problem, func(a, b int) bool {
		return problem[a].Failed*3+problem[a].Upstream > problem[b].Failed*3+problem[b].Upstream
	})
	if len(problem) > 8 {
		problem = problem[:8]
	}
	total := map[string]int{}
	for _, b := range buckets {
		total["requests"] += b.Requests
		total["succeeded"] += b.Succeeded
		total["failed"] += b.Failed
		total["cancelled"] += b.Cancelled
		total["upstream_errors"] += b.Upstream
	}
	sort.Float64s(firsts)
	pct := func(p float64) float64 {
		if len(firsts) == 0 {
			return 0
		}
		return firsts[int(p*float64(len(firsts)-1))]
	}
	return map[string]any{"from": start, "to": now, "buckets": buckets, "totals": total, "inflight_count": len(inflight), "inflight": inflight[:min(len(inflight), 20)], "problem_accounts": problem, "first_output_p50_ms": pct(.5), "first_output_p95_ms": pct(.95), "checked_at": now}
}

func (a *API) live(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	rows, err := a.Store.LiveObservations(r.Context(), now.Add(-61*time.Minute))
	if err != nil {
		failure(w, err)
		return
	}
	v := liveView(rows, now)
	refs := refSet{}
	for _, q := range v["inflight"].([]liveRequest) {
		refs.add("user", q.UserID)
		refs.add("key", q.KeyID)
		refs.add("account", q.AccountID)
		refs.add("group", q.GroupID)
	}
	for _, p := range v["problem_accounts"].([]liveAccount) {
		refs.add("account", p.AccountID)
	}
	v["names"] = a.names(r.Context(), refs)
	write(w, 200, v, "success")
}
