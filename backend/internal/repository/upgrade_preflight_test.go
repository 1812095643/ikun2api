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
