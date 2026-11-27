package store

import (
	"context"
	"fmt"
	"time"

	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
)

type RequestOutcomeCounts struct {
	Success   int `json:"success"`
	Failed    int `json:"failed"`
	Cancelled int `json:"cancelled"`
	Unknown   int `json:"unknown"`
}

func (s *Store) IncidentContext(ctx context.Context, i *domain.Incident) (RequestOutcomeCounts, error) {
	var result RequestOutcomeCounts
	accountID := int64(0)
	if i.Sample != nil {
		accountID = i.Sample.AccountID
	}
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE((SELECT name FROM groups WHERE id=$1),''),COALESCE((SELECT name FROM accounts WHERE id=$2),'')`, i.GroupID, accountID).Scan(&i.GroupName, &i.SampleAccountName)
	if err != nil {
		return result, err
	}
	err = s.db.QueryRowContext(ctx, `SELECT count(*) FILTER(WHERE o.body->>'outcome'='success'),
	 count(*) FILTER(WHERE o.body->>'outcome'='failed'),count(*) FILTER(WHERE o.body->>'outcome'='cancelled'),
	 count(*) FILTER(WHERE o.body->>'outcome' IS NULL OR o.body->>'outcome' NOT IN ('success','failed','cancelled'))
	 FROM ops_monitor_incident_impact p LEFT JOIN ops_monitor_request_outcomes o ON o.request_id=p.value
	 WHERE p.incident_id=$1 AND p.kind='request'`, i.ID).Scan(&result.Success, &result.Failed, &result.Cancelled, &result.Unknown)
	return result, err
}

type IncidentRequest struct {
	RequestID    string     `json:"request_id"`
	Outcome      string     `json:"outcome"`
	AccountID    int64      `json:"account_id"`
	AccountName  string     `json:"account_name"`
	GroupID      int64      `json:"group_id"`
	GroupName    string     `json:"group_name"`
	StartedAt    *time.Time `json:"started_at"`
	EndedAt      *time.Time `json:"ended_at"`
	ClientStatus int        `json:"client_status"`
}

type IncidentRequestPage struct {
	Items   []IncidentRequest `json:"items"`
	HasMore bool              `json:"has_more"`
	AfterID string            `json:"after_id"`
}

func (s *Store) IncidentRequests(ctx context.Context, id, after string, limit int) (IncidentRequestPage, error) {
	limit = incidentPageLimit(limit)
	items, err := readJSON[IncidentRequest](ctx, s.db, `SELECT jsonb_build_object(
	 'request_id',p.value,'outcome',COALESCE(o.body->>'outcome','unknown'),
	 'account_id',COALESCE((o.body->>'account_id')::bigint,0),'account_name',COALESCE(a.name,''),
	 'group_id',COALESCE((o.body->>'group_id')::bigint,0),'group_name',COALESCE(g.name,''),
	 'started_at',CASE WHEN COALESCE((o.body#>>'{metrics,elapsed_ms}')::numeric,(o.body#>>'{metrics,duration_ms}')::numeric,0)>0 THEN
	 to_timestamp((o.occurred-COALESCE((o.body#>>'{metrics,elapsed_ms}')::numeric,(o.body#>>'{metrics,duration_ms}')::numeric))/1000) END,
	 'ended_at',to_timestamp(o.occurred/1000.0),'client_status',COALESCE((o.body->>'client_http_status')::int,0))
	 FROM (SELECT value FROM ops_monitor_incident_impact WHERE incident_id=$1 AND kind='request' AND value>$2 ORDER BY value LIMIT $3) p
	 LEFT JOIN ops_monitor_request_outcomes o ON o.request_id=p.value
	 LEFT JOIN accounts a ON a.id=(o.body->>'account_id')::bigint LEFT JOIN groups g ON g.id=(o.body->>'group_id')::bigint
	 ORDER BY p.value`, id, after, limit+1)
	page := IncidentRequestPage{Items: items, HasMore: len(items) > limit}
	if page.HasMore {
		page.Items = items[:limit]
	}
	if len(page.Items) > 0 {
		page.AfterID = page.Items[len(page.Items)-1].RequestID
	}
	return page, err
}

type IncidentAccount struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Deleted bool   `json:"deleted"`
	Cursor  string `json:"cursor"`
}

type IncidentAccountPage struct {
	Items   []IncidentAccount `json:"items"`
	HasMore bool              `json:"has_more"`
	AfterID string            `json:"after_id"`
}

func (s *Store) IncidentAccounts(ctx context.Context, id, after string, limit int) (IncidentAccountPage, error) {
	limit = incidentPageLimit(limit)
	items, err := readJSON[IncidentAccount](ctx, s.db, `SELECT jsonb_build_object('id',p.value::bigint,'name',COALESCE(a.name,''),
	 'deleted',a.id IS NULL OR a.deleted_at IS NOT NULL,'cursor',p.value)
	 FROM (SELECT value FROM ops_monitor_incident_impact WHERE incident_id=$1 AND kind='account' AND value>$2 ORDER BY value LIMIT $3) p
	 LEFT JOIN accounts a ON a.id=p.value::bigint ORDER BY p.value`, id, after, limit+1)
	page := IncidentAccountPage{Items: items, HasMore: len(items) > limit}
	if page.HasMore {
		page.Items = items[:limit]
	}
	if len(page.Items) > 0 {
		page.AfterID = page.Items[len(page.Items)-1].Cursor
	}
	return page, err
}

type IncidentHistoryPage struct {
	Items    []domain.IncidentTransition `json:"items"`
	HasMore  bool                        `json:"has_more"`
	BeforeID int64                       `json:"before_id"`
}

func (s *Store) IncidentHistory(ctx context.Context, id string, before int64, limit int, kind string) (IncidentHistoryPage, error) {
	limit = incidentPageLimit(limit)
	where := ""
	args := []any{id, limit + 1}
	if before > 0 {
		args = append(args, before)
		where = fmt.Sprintf(" AND id<$%d", len(args))
	}
	switch kind {
	case "operations":
		where += " AND body->>'action' IN ('ack','unack','silence','unsilence','close','configuration')"
	case "observations":
		where += " AND COALESCE(body->>'action','legacy') IN ('observation','legacy')"
	}
	items, err := readJSON[domain.IncidentTransition](ctx, s.db, `SELECT h.body || jsonb_build_object('transition_id',h.id,
	 'recorded_at',to_timestamp(h.created/1000.0),'action',COALESCE(h.body->>'action','legacy'),
	 'actor_name',COALESCE(NULLIF(u.username,''),NULLIF(u.email,''),h.body->>'actor',''))
	 FROM (SELECT id,body,created FROM ops_monitor_incident_transitions WHERE incident_id=$1`+where+` ORDER BY id DESC LIMIT $2) h
	 LEFT JOIN users u ON u.id=CASE WHEN h.body->>'actor'~'^[0-9]{1,18}$' THEN (h.body->>'actor')::bigint END ORDER BY h.id DESC`, args...)
	page := IncidentHistoryPage{Items: items, HasMore: len(items) > limit}
	if page.HasMore {
		page.Items = items[:limit]
	}
	if len(page.Items) > 0 {
		page.BeforeID = page.Items[len(page.Items)-1].TransitionID
	}
	return page, err
}

func incidentPageLimit(limit int) int {
	if limit < 1 || limit > 100 {
		return 30
	}
	return limit
}
