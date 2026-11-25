package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
)

func TaskKey(o domain.Observation) string {
	return fmt.Sprintf("%s:%d:%d:%s", o.Site, o.UserID, o.KeyID, o.TaskID)
}
func IsTaskFact(kind string) bool {
	switch kind {
	case "media.accepted", "media.pending", "media.completed", "media.failed", "media.cancelled", "billing.applied", "billing.failed", "billing.metadata_failed":
		return true
	}
	return false
}

func (s *Store) TaskPage(ctx context.Context, after string, limit int, priority []string) ([]domain.Observation, string, error) {
	if limit < 1 || limit > 500 || len(priority) > 1000 {
		return nil, "", errors.New("task page exceeds budget")
	}
	rows, err := s.db.QueryContext(ctx, "SELECT DISTINCT scope FROM ops_monitor_task_facts WHERE scope>$1 ORDER BY scope LIMIT $2", after, limit+1)
	if err != nil {
		return nil, "", err
	}
	scopes := []string{}
	for rows.Next() {
		var v string
		if err = rows.Scan(&v); err != nil {
			rows.Close()
			return nil, "", err
		}
		scopes = append(scopes, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(scopes) > limit {
		scopes = scopes[:limit]
		next = scopes[len(scopes)-1]
	}
	scopes = append(scopes, priority...)
	raw, err := json.Marshal(scopes)
	if err != nil {
		return nil, "", err
	}
	out, err := readJSON[domain.Observation](ctx, s.db, "SELECT body FROM ops_monitor_task_facts WHERE scope IN (SELECT value FROM jsonb_array_elements_text($1::jsonb)) ORDER BY occurred", string(raw))
	return out, next, err
}
