package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/redis/go-redis/v9"
)

type identityStore struct {
	db  *sql.DB
	rdb *redis.Client
}

func NewIdentityStore(db *sql.DB, rdb *redis.Client) service.IdentityStore {
	return &identityStore{db: db, rdb: rdb}
}

func (s *identityStore) GetDeviceID(ctx context.Context, accountID int64) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, "SELECT device_id FROM claude_device_identities WHERE account_id = $1", accountID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

const createClaudeDeviceIdentitySQL = `INSERT INTO claude_device_identities (account_id, device_id)
 SELECT id, $2 FROM accounts WHERE id = $1 AND deleted_at IS NULL
 AND platform = 'anthropic' AND type IN ('oauth', 'setup-token')
 ON CONFLICT (account_id) DO UPDATE SET account_id = EXCLUDED.account_id
 RETURNING device_id`

func (s *identityStore) GetOrCreateDeviceID(ctx context.Context, accountID int64, candidate string) (string, error) {
	if !service.ValidClaudeDeviceID(candidate) {
		return "", fmt.Errorf("invalid Claude device identity candidate")
	}
	var winner string
	// ON CONFLICT returns the stored winner, never the racing candidate. Keeping
	// identity outside account.extra also prevents admin edits/imports from rotating it.
	err := s.db.QueryRowContext(ctx, createClaudeDeviceIdentitySQL, accountID, candidate).Scan(&winner)
	return winner, err
}

func (s *identityStore) MigrateLegacyFingerprints(ctx context.Context) error {
	var cursor uint64
	for {
		keys, next, err := s.rdb.Scan(ctx, cursor, fingerprintKeyPrefix+"*", 200).Result()
		if err != nil {
			return err
		}
		for _, key := range keys {
			accountID, err := strconv.ParseInt(strings.TrimPrefix(key, fingerprintKeyPrefix), 10, 64)
			if err != nil || accountID <= 0 {
				continue
			}
			existing, err := s.GetDeviceID(ctx, accountID)
			if err != nil {
				return err
			}
			if existing != "" {
				continue
			}
			raw, err := s.rdb.Get(ctx, key).Result()
			if err == redis.Nil {
				continue
			}
			if err != nil {
				return err
			}
			var fp service.Fingerprint
			if json.Unmarshal([]byte(raw), &fp) != nil || !service.ValidClaudeDeviceID(fp.ClientID) {
				continue
			}
			if _, err := s.GetOrCreateDeviceID(ctx, accountID, fp.ClientID); err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		cursor = next
		if cursor == 0 {
			return nil
		}
	}
}
