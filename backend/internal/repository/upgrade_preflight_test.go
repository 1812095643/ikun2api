//go:build unit

package repository

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"testing"
	"testing/fstest"
)

func TestUpgradePreflightReadsWithoutMutation(t *testing.T) {
	for _, tc := range []struct {
		name, file, checksum string
		ok                   bool
	}{{"same", "001_init.sql", fmt.Sprintf("%x", sha256.Sum256([]byte("fixture"))), true}, {"changed", "001_init.sql", "invalid", false}, {"future", "999_future.sql", "unknown", false}} {
		t.Run(tc.name, func(t *testing.T) {
			db, m, e := sqlmock.New()
			require.NoError(t, e)
			defer db.Close()
			m.ExpectQuery("SELECT filename, checksum FROM schema_migrations ORDER BY filename").WillReturnRows(sqlmock.NewRows([]string{"filename", "checksum"}).AddRow(tc.file, tc.checksum))
			e = validateUpgradeSchemaFS(context.Background(), db, fstest.MapFS{"001_init.sql": &fstest.MapFile{Data: []byte("\nfixture\n")}})
			if tc.ok {
				require.NoError(t, e)
			} else {
				require.Error(t, e)
			}
			require.NoError(t, m.ExpectationsWereMet())
		})
	}
}

func TestUpgradeKnownIKUNMigrationLineEndingCompatibility(t *testing.T) {
	const name = "259_claude_device_identities.sql"
	const lf = "b82daf89494264508194b8a5261ad6adeb321a85fce4dd27f42a297aca94ceb5"
	const crlf = "1aec38250cfa0b97d9c7556e8127881096672da2e50fc0f46fe16e2349abb213"
	require.True(t, isMigrationChecksumCompatible(name, crlf, lf))
	require.True(t, isMigrationChecksumCompatible(name, lf, crlf))
	require.False(t, isMigrationChecksumCompatible(name, "unknown", lf))
	require.False(t, isMigrationChecksumCompatible(name, crlf, "modified-sql"))
	require.False(t, isMigrationChecksumCompatible("260_workspace_upstream_balances.sql", crlf, lf))
}
