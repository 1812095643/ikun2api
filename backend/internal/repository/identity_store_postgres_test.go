//go:build unit

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func claudeIdentityTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("CLAUDE_IDENTITY_TEST_DSN")
	if dsn == "" {
		t.Skip("requires isolated CLAUDE_IDENTITY_TEST_DSN")
	}
	base, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	schema := fmt.Sprintf("claude_identity_test_%d", time.Now().UnixNano())
	_, err = base.Exec("CREATE SCHEMA " + schema)
	require.NoError(t, err)
	db, err := sql.Open("postgres", dsn+" search_path="+schema)
	require.NoError(t, err)
	db.SetMaxOpenConns(12)
	t.Cleanup(func() { _ = db.Close(); _, _ = base.Exec("DROP SCHEMA " + schema + " CASCADE"); _ = base.Close() })
	_, err = db.Exec(`CREATE TABLE accounts(id BIGINT PRIMARY KEY, platform TEXT, type TEXT, deleted_at TIMESTAMPTZ);
 INSERT INTO accounts VALUES(41,'anthropic','oauth',NULL),(42,'anthropic','setup-token',NULL),(43,'anthropic','oauth',NULL),(44,'openai','oauth',NULL);`)
	require.NoError(t, err)
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "259_claude_device_identities.sql"))
	require.NoError(t, err)
	_, err = db.Exec(string(migration))
	require.NoError(t, err)
	return db
}

func TestClaudeIdentityPostgresConcurrentWinnerAndMigration(t *testing.T) {
	db := claudeIdentityTestDB(t)
	ctx := context.Background()
	server := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	store := NewIdentityStore(db, redisClient)
	cache := NewIdentityCache(redisClient)
	old := strings.Repeat("a", 64)
	raw, err := json.Marshal(&service.Fingerprint{ClientID: old, UserAgent: "claude-cli/2.1.220"})
	require.NoError(t, err)
	server.Set(fingerprintKey(43), string(raw))
	server.SetTTL(fingerprintKey(43), time.Second)
	svc, err := service.ProvideIdentityService(cache, store)
	require.NoError(t, err)
	server.FastForward(2 * time.Second)
	inherited, err := svc.GetOrCreateFingerprint(ctx, 43, nil)
	require.NoError(t, err)
	require.Equal(t, old, inherited.ClientID)
	// Independent service instances share the SQL uniqueness constraint.
	second := service.NewIdentityService(cache, NewIdentityStore(db, redisClient))
	const count = 64
	results := make(chan string, count)
	failures := make(chan error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id, err := store.GetOrCreateDeviceID(ctx, 41, fmt.Sprintf("%064x", i+1))
			if err != nil {
				failures <- err
			} else {
				results <- id
			}
		}(i)
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	winner, err := store.GetDeviceID(ctx, 41)
	require.NoError(t, err)
	require.True(t, service.ValidClaudeDeviceID(winner))
	for id := range results {
		require.Equal(t, winner, id)
	}
	first, err := svc.GetOrCreateFingerprint(ctx, 41, nil)
	require.NoError(t, err)
	require.Equal(t, winner, first.ClientID)
	server.FlushAll()
	restored, err := second.GetOrCreateFingerprint(ctx, 41, nil)
	require.NoError(t, err)
	require.Equal(t, winner, restored.ClientID)
	other, err := second.GetOrCreateFingerprint(ctx, 42, nil)
	require.NoError(t, err)
	require.NotEqual(t, winner, other.ClientID)
	// A later legacy cache value must not replace the database winner.
	raw, err = json.Marshal(&service.Fingerprint{ClientID: strings.Repeat("f", 64)})
	require.NoError(t, err)
	server.Set(fingerprintKey(41), string(raw))
	require.NoError(t, store.MigrateLegacyFingerprints(ctx))
	restored, err = second.GetOrCreateFingerprint(ctx, 41, nil)
	require.NoError(t, err)
	require.Equal(t, winner, restored.ClientID)
	_, err = store.GetOrCreateDeviceID(ctx, 44, old)
	require.ErrorIs(t, err, sql.ErrNoRows)
	_, err = store.GetOrCreateDeviceID(ctx, 41, "invalid")
	require.Error(t, err)
	var rows int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM claude_device_identities WHERE account_id=41").Scan(&rows))
	require.Equal(t, 1, rows)
	_, err = db.Exec("DELETE FROM accounts WHERE id=42")
	require.NoError(t, err)
	id, err := store.GetDeviceID(ctx, 42)
	require.NoError(t, err)
	require.Empty(t, id)
}
