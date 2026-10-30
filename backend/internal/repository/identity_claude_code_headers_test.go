package repository

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newClaudeCodeHeaderCacheTest(t *testing.T) (*identityCache, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return &identityCache{rdb: client}, server
}

func TestIdentityClaudeCodeHeaders_PartialReplacementAndExpiry(t *testing.T) {
	cache, server := newClaudeCodeHeaderCacheTest(t)
	ctx := context.Background()
	empty, err := cache.GetClaudeCodeHeaders(ctx, 1)
	require.NoError(t, err)
	require.Empty(t, empty)
	initial := http.Header{
		"anthropic-version": {"2023-06-01"}, "anthropic-beta": {"oauth"},
		"anthropic-dangerous-direct-browser-access": {"true"},
	}

	require.NoError(t, cache.UpdateClaudeCodeHeaders(ctx, 1, initial))
	got, err := cache.GetClaudeCodeHeaders(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, initial, got)
	require.Equal(t, fingerprintTTL, server.TTL(claudeCodeHeadersKey(1)))
	server.FastForward(24 * time.Hour)
	got, err = cache.GetClaudeCodeHeaders(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, initial, got)
	require.Equal(t, fingerprintTTL-24*time.Hour, server.TTL(claudeCodeHeadersKey(1)), "reads do not renew expiry")
	require.NoError(t, cache.UpdateClaudeCodeHeaders(ctx, 1, http.Header{}))
	require.Equal(t, fingerprintTTL-24*time.Hour, server.TTL(claudeCodeHeadersKey(1)), "missing response fields do not renew expiry")
	require.NoError(t, cache.UpdateClaudeCodeHeaders(ctx, 1, http.Header{"anthropic-version": {""}}))
	got, err = cache.GetClaudeCodeHeaders(ctx, 1)
	require.NoError(t, err)
	initial["anthropic-version"] = []string{""}
	require.Equal(t, initial, got)
	require.Equal(t, fingerprintTTL, server.TTL(claudeCodeHeadersKey(1)))
	other, err := cache.GetClaudeCodeHeaders(ctx, 2)
	require.NoError(t, err)
	require.Empty(t, other)
	server.FastForward(fingerprintTTL)
	expired, err := cache.GetClaudeCodeHeaders(ctx, 1)
	require.NoError(t, err)
	require.Empty(t, expired)
}

func TestIdentityClaudeCodeHeaders_ConcurrentPartialUpdates(t *testing.T) {
	cache, _ := newClaudeCodeHeaderCacheTest(t)
	ctx := context.Background()
	updates := http.Header{
		"anthropic-version": {"2023-06-01"}, "anthropic-beta": {"oauth"},
		"anthropic-dangerous-direct-browser-access": {"true"},
	}

	start := make(chan struct{})
	errors := make(chan error, len(updates))
	var workers sync.WaitGroup
	for name, values := range updates {
		workers.Add(1)
		go func(name string, values []string) {
			defer workers.Done()
			<-start
			errors <- cache.UpdateClaudeCodeHeaders(ctx, 1, http.Header{name: values})
		}(name, values)
	}
	close(start)
	workers.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	got, err := cache.GetClaudeCodeHeaders(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, updates, got, "independent updates must not discard each other's fields")
}

func TestIdentityClaudeCodeHeadersRejectsLegacySessionFields(t *testing.T) {
	cache, server := newClaudeCodeHeaderCacheTest(t)
	ctx := context.Background()
	server.HSet(claudeCodeHeadersKey(41), "X-Claude-Code-Compaction", "not even JSON", "authorization", `["secret"]`, "anthropic-version", `["2023-06-01"]`)
	got, err := cache.GetClaudeCodeHeaders(ctx, 41)
	require.NoError(t, err)
	require.Equal(t, http.Header{"anthropic-version": {"2023-06-01"}}, got)
	require.Empty(t, server.HGet(claudeCodeHeadersKey(41), "X-Claude-Code-Compaction"))
	require.NoError(t, cache.UpdateClaudeCodeHeaders(ctx, 41, http.Header{"x-cc-compaction-request": {"secret"}}))
	require.Empty(t, server.HGet(claudeCodeHeadersKey(41), "x-cc-compaction-request"))
}
