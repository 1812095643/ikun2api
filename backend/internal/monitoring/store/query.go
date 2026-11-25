package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
)

// 配置写入、事件控制与规则评估共用短期数据库锁，通知领取独立并行。
const ConfigLock int64 = 709311824

func jsonEqual(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	xx, _ := json.Marshal(x)
	yy, _ := json.Marshal(y)
	return string(xx) == string(yy)
}

type IncidentFilter struct {
	Platform, Model, Status, Severity, Rule string
	GroupID, AccountID                      int64
	Start, End                              time.Time
	BeforeTime                              int64
	BeforeID                                string
	Limit                                   int
}
type IncidentPage struct {
	Items      []domain.Incident `json:"items"`
	HasMore    bool              `json:"has_more"`
	BeforeTime int64             `json:"before_time"`
	BeforeID   string            `json:"before_id"`
}

func (s *Store) ListIncidents(ctx context.Context, f IncidentFilter) (IncidentPage, error) {
	if f.Limit < 1 || f.Limit > 100 {
		f.Limit = 30
	}
	where, args := []string{"TRUE"}, []any{}
	add := func(expr string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(expr, len(args)))
	}
	if f.Platform != "" {
		add("body->>'platform'=$%d", f.Platform)
	}
	if f.GroupID > 0 {
		add("body->>'group_id'=$%d", fmt.Sprint(f.GroupID))
	}
	if f.Model != "" {
		add("body->>'model'=$%d", f.Model)
	}
	if f.AccountID > 0 {
		add("body#>>'{sample,account_id}'=$%d", fmt.Sprint(f.AccountID))
	}
	if f.Status != "" {
		switch f.Status {
		case "active":
			where = append(where, "body->>'status' NOT IN ('resolved','closed','superseded')")
		case "acknowledged":
			where = append(where, "body->>'acknowledged'='true'")
		case "silenced":
			add("COALESCE(NULLIF(body->>'silenced_until',''),'0001-01-01')::timestamptz>$%d", time.Now())
		default:
			add("body->>'status'=$%d", f.Status)
		}
	}
	if f.Severity != "" {
		add("body->>'severity'=$%d", f.Severity)
	}
	if f.Rule != "" {
		add("body->>'rule_id'=$%d", f.Rule)
	}
	if !f.Start.IsZero() {
		add("updated>=$%d", f.Start.UnixMilli())
	}
	if !f.End.IsZero() {
		add("updated<=$%d", f.End.UnixMilli())
	}
	if f.BeforeTime > 0 && f.BeforeID != "" {
		args = append(args, f.BeforeTime, f.BeforeID)
		where = append(where, fmt.Sprintf("(updated,id)<($%d,$%d)", len(args)-1, len(args)))
	}
	args = append(args, f.Limit+1)
	items, err := readJSON[domain.Incident](ctx, s.db, "SELECT (body - 'recent_hits' - 'members' - 'legacy_variables') || jsonb_build_object('updated_cursor',updated,'explanation', (body->'explanation') - 'facts' - 'inferences' - 'missing_evidence' - 'advice') FROM ops_monitor_incidents WHERE "+strings.Join(where, " AND ")+fmt.Sprintf(" ORDER BY updated DESC,id DESC LIMIT $%d", len(args)), args...)
	if err != nil {
		return IncidentPage{}, err
	}
	page := IncidentPage{Items: items, HasMore: len(items) > f.Limit}
	if page.HasMore {
		page.Items = items[:f.Limit]
	}
	if len(page.Items) > 0 {
		last := page.Items[len(page.Items)-1]
		page.BeforeTime = last.UpdatedCursor
		page.BeforeID = last.ID
	}
	return page, nil
}

func (s *Store) Audit(ctx context.Context, actor, action, channelID, targetID string) error {
	raw, _ := json.Marshal(map[string]string{"action": action, "channel_id": channelID, "target_id": targetID})
	_, err := s.db.ExecContext(ctx, `INSERT INTO ops_monitor_mutations(id,actor,body,created) VALUES(md5(random()::text || clock_timestamp()::text),$1,$2,$3)`, actor, string(raw), time.Now().UnixMilli())
	return err
}

type QueueHealth struct {
	Pending         int64  `json:"pending"`
	Uncertain       int64  `json:"uncertain"`
	NeedsAttention  int64  `json:"needs_attention"`
	OldestPendingMS *int64 `json:"oldest_pending_ms"`
}

func (s *Store) QueueHealth(ctx context.Context) (QueueHealth, error) {
	var h QueueHealth
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FILTER(WHERE state IN ('pending','leased')),count(*) FILTER(WHERE state='uncertain'),count(*) FILTER(WHERE state='dead'),min(next_attempt) FILTER(WHERE state IN ('pending','leased')) FROM ops_monitor_notification_outbox WHERE state IN ('pending','leased','uncertain','dead')`).Scan(&h.Pending, &h.Uncertain, &h.NeedsAttention, &h.OldestPendingMS)
	return h, err
}
