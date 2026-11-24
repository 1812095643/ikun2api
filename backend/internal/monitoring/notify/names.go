package notify

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/lib/pq"
)

// Identity is the display label of a user, key, upstream account or group.
type Identity struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
	Owner int64  `json:"owner,omitempty"` // key → user
}

// Names resolves display labels in bulk from the restricted identity view.
// A nil DB or a failed lookup returns what was found; callers fall back to IDs.
type Names struct{ DB *sql.DB }

var nameKinds = map[string]bool{"user": true, "key": true, "account": true, "group": true}

// Resolve takes {"user": [6, 7], "account": [31]} and returns {"user:6": {...}}.
func (n Names) Resolve(ctx context.Context, refs map[string][]int64) map[string]Identity {
	out := map[string]Identity{}
	if n.DB == nil {
		return out
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	for kind, ids := range refs {
		if !nameKinds[kind] || len(ids) == 0 {
			continue
		}
		if len(ids) > 500 {
			ids = ids[:500]
		}
		rows, err := n.DB.QueryContext(ctx, "SELECT id, owner_id, name, email FROM ops_monitor_v1_identity WHERE kind=$1 AND id = ANY($2)", kind, pq.Array(ids))
		if err != nil {
			continue
		}
		for rows.Next() {
			var id, owner int64
			var name, email string
			if rows.Scan(&id, &owner, &name, &email) == nil {
				out[fmt.Sprintf("%s:%d", kind, id)] = Identity{Name: cleanLabel(name), Email: cleanLabel(email), Owner: owner}
			}
		}
		rows.Close()
	}
	return out
}
