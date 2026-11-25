//go:build integration

package repository

import (
	"context"
	dbmigrations "github.com/MACOS-DO/sub4api/migrations"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestBridgeMigrationPreservesExistingGroupAccess(t *testing.T) {
	ctx := context.Background()
	tx := testTx(t)
	_, err := tx.ExecContext(ctx, `ALTER TABLE groups DROP COLUMN bridge_required`)
	require.NoError(t, err)
	var id int64
	require.NoError(t, tx.QueryRowContext(ctx, `INSERT INTO groups(name,platform) VALUES ('bridge-upgrade','anthropic') RETURNING id`).Scan(&id))
	migration, err := dbmigrations.FS.ReadFile("276_group_bridge_required.sql")
	require.NoError(t, err)
	for range 2 {
		_, err = tx.ExecContext(ctx, string(migration))
		require.NoError(t, err)
		var enabled bool
		require.NoError(t, tx.QueryRowContext(ctx, `SELECT bridge_required FROM groups WHERE id=$1`, id).Scan(&enabled))
		require.False(t, enabled, "upgrade must not lock out existing direct clients")
	}
	_, err = tx.ExecContext(ctx, `UPDATE groups SET bridge_required=true WHERE id=$1`, id)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	var enabled bool
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT bridge_required FROM groups WHERE id=$1`, id).Scan(&enabled))
	require.True(t, enabled, "reapplying migration preserves explicit opt-in")
}
