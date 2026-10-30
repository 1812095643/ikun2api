package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"github.com/MACOS-DO/sub4api/migrations"
	"io/fs"
	"strings"
)

// ValidateUpgradeSchema performs only reads. Unknown future migrations and
// changed checksums stop a switch before the candidate touches the database.
func ValidateUpgradeSchema(ctx context.Context, db *sql.DB) error {
	return validateUpgradeSchemaFS(ctx, db, migrations.FS)
}
func validateUpgradeSchemaFS(ctx context.Context, db *sql.DB, files fs.FS) error {
	rows, err := db.QueryContext(ctx, "SELECT filename, checksum FROM schema_migrations ORDER BY filename")
	if err != nil {
		return fmt.Errorf("cannot inspect existing migration ledger: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, checksum string
		if err := rows.Scan(&name, &checksum); err != nil {
			return err
		}
		data, err := fs.ReadFile(files, name)
		if err != nil {
			return fmt.Errorf("unsupported migration %s: use a compatible release before switching", name)
		}
		h := sha256.Sum256([]byte(strings.TrimSpace(string(data))))
		actual := hex.EncodeToString(h[:])
		if actual != checksum && !isMigrationChecksumCompatible(name, checksum, actual) {
			return fmt.Errorf("migration checksum mismatch: %s", name)
		}
	}
	return rows.Err()
}
