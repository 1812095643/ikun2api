package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

type modelRowScanner interface{ Scan(...any) error }

const modelRecordColumns = "config,state,history,notification,revision,refresh_requested,lease_until,last_checked_at,next_check_at"

func scanModelRecord(row modelRowScanner) (upstreamModelRecord, error) {
	r := upstreamModelRecord{Config: defaultModelConfig(), Accounts: []upstreamModelAccountState{}, History: []upstreamModelHistory{}}
	var config, state, history, notice []byte
	if err := row.Scan(&config, &state, &history, &notice, &r.Revision, &r.Summary.Queued, &r.lease, &r.Summary.CheckedAt, &r.next); err != nil {
		return r, err
	}
	for _, v := range []struct {
		b      []byte
		target any
	}{{config, &r.Config}, {state, &r.Accounts}, {history, &r.History}, {notice, &r.notice}} {
		if err := json.Unmarshal(v.b, v.target); err != nil {
			return r, err
		}
	}
	r.Summary.Enabled = r.Config.Enabled
	r.Summary.Checking = r.lease != nil && r.lease.After(time.Now())
	r.NotificationError = r.notice.Error
	return r, nil
}
func (h *Handler) loadModelRecord(ctx context.Context, id string) (upstreamModelRecord, error) {
	return scanModelRecord(h.db.QueryRowContext(ctx, "SELECT "+modelRecordColumns+" FROM workspace_upstream_models WHERE upstream_id=$1", id))
}
func modelStatesForGroup(states []upstreamModelAccountState, g upstreamBalanceGroup) []upstreamModelAccountState {
	old := map[int64]upstreamModelAccountState{}
	for _, a := range states {
		old[a.AccountID] = a
	}
	out := []upstreamModelAccountState{}
	for _, a := range g.Accounts {
		s := old[a.ID]
		if s.Source != modelSource(a) {
			s = upstreamModelAccountState{AccountID: a.ID, Source: modelSource(a), Baseline: []string{}, Current: []string{}, IgnoredAdded: []string{}, IgnoredRemoved: []string{}}
		}
		out = append(out, s)
	}
	return out
}
func publicModelRecord(r upstreamModelRecord, g upstreamBalanceGroup) upstreamModelRecord {
	r.Accounts = modelStatesForGroup(r.Accounts, g)
	r.Summary.Pending = 0
	r.Summary.Errors = 0
	for i := range r.Accounts {
		d := pendingModelChange(r.Accounts[i])
		r.Summary.Pending += len(d.Added) + len(d.Removed)
		if r.Accounts[i].Error != "" {
			r.Summary.Errors++
		}
		r.Accounts[i].Source = ""
	}
	return r
}
func (h *Handler) attachModelSummaries(ctx context.Context, groups []upstreamBalanceGroup, records []upstreamBalanceRecord) error {
	if len(records) == 0 {
		return nil
	}
	rows, err := h.db.QueryContext(ctx, "SELECT upstream_id,"+modelRecordColumns+" FROM workspace_upstream_models")
	if err != nil {
		return err
	}
	defer rows.Close()
	byID := map[string]upstreamBalanceGroup{}
	for _, g := range groups {
		byID[g.ID] = g
	}
	for rows.Next() {
		var id string
		r, err := scanModelRecord(modelIDScanner{rows: rows, id: &id})
		if err != nil {
			return err
		}
		g, ok := byID[id]
		if !ok {
			continue
		}
		r = publicModelRecord(r, g)
		for i := range records {
			if records[i].ID == id {
				records[i].Models = r.Summary
				break
			}
		}
	}
	return rows.Err()
}

type modelIDScanner struct {
	rows *sql.Rows
	id   *string
}

func (s modelIDScanner) Scan(args ...any) error { return s.rows.Scan(append([]any{s.id}, args...)...) }
func (h *Handler) modelGroup(ctx context.Context, id string) (upstreamBalanceGroup, error) {
	groups, err := h.discoverUpstreamBalances(ctx)
	if err != nil {
		return upstreamBalanceGroup{}, err
	}
	if err = h.syncUpstreamBalances(ctx, groups); err != nil {
		return upstreamBalanceGroup{}, err
	}
	for _, g := range groups {
		if g.ID == id {
			return g, nil
		}
	}
	return upstreamBalanceGroup{}, notFound()
}
