package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

func (r *usageLogRepository) SavePoolShare(ctx context.Context, userID int64, hash string, expires time.Time) error {
	_, err := r.sql.ExecContext(ctx, `INSERT INTO pool_share_links(user_id,token_hash,expires_at) VALUES($1,$2,$3)
	ON CONFLICT(user_id) DO UPDATE SET token_hash=EXCLUDED.token_hash,expires_at=EXCLUDED.expires_at,created_at=NOW()`, userID, hash, expires)
	return err
}
func (r *usageLogRepository) ResolvePoolShare(ctx context.Context, hash string, now time.Time) (int64, error) {
	var id int64
	err := scanSingleRow(ctx, r.sql, `SELECT s.user_id FROM pool_share_links s JOIN users u ON u.id=s.user_id
	WHERE s.token_hash=$1 AND s.expires_at>$2 AND u.status='active' AND u.deleted_at IS NULL`, []any{hash, now}, &id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}
func (r *usageLogRepository) RevokePoolShare(ctx context.Context, userID int64) error {
	_, err := r.sql.ExecContext(ctx, "DELETE FROM pool_share_links WHERE user_id=$1", userID)
	return err
}
