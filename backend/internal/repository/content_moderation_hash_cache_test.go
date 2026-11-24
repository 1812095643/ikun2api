package repository

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestContentModerationHashCacheExpiresAndIgnoresLegacy(t *testing.T) {
	server := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer func() { _ = rdb.Close() }()
	ctx := context.Background()
	cache := NewContentModerationHashCache(rdb)
	require.NoError(t, rdb.SAdd(ctx, "content_moderation:flagged_hashes", "old-hash").Err())
	hit, err := cache.HasFlaggedInputHash(ctx, "old-hash")
	require.NoError(t, err)
	require.False(t, hit)
	require.NoError(t, cache.RecordFlaggedInputHash(ctx, "new-hash"))
	hit, err = cache.HasFlaggedInputHash(ctx, "new-hash")
	require.NoError(t, err)
	require.True(t, hit)
	require.NoError(t, rdb.ZAdd(ctx, contentModerationFlaggedHashSetKey, redis.Z{Score: float64(time.Now().Add(-time.Second).Unix()), Member: "expired"}).Err())
	hit, err = cache.HasFlaggedInputHash(ctx, "expired")
	require.NoError(t, err)
	require.False(t, hit)
	count, err := cache.CountFlaggedInputHashes(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
	deleted, err := cache.DeleteFlaggedInputHash(ctx, "new-hash")
	require.NoError(t, err)
	require.True(t, deleted)
	require.LessOrEqual(t, server.TTL(contentModerationFlaggedHashSetKey), 48*time.Hour)
}
