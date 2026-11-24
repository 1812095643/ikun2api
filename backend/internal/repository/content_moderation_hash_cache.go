package repository

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/redis/go-redis/v9"
)

const contentModerationFlaggedHashSetKey = "content_moderation:flagged_hashes:v2"

type contentModerationHashCache struct {
	rdb *redis.Client
}

func NewContentModerationHashCache(rdb *redis.Client) service.ContentModerationHashCache {
	return &contentModerationHashCache{rdb: rdb}
}

func (c *contentModerationHashCache) RecordFlaggedInputHash(ctx context.Context, inputHash string) error {
	inputHash = strings.TrimSpace(inputHash)
	if c == nil || c.rdb == nil || inputHash == "" {
		return nil
	}
	_, err := c.rdb.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.ZAdd(ctx, contentModerationFlaggedHashSetKey, redis.Z{Score: float64(time.Now().Add(24 * time.Hour).Unix()), Member: inputHash})
		p.ZRemRangeByScore(ctx, contentModerationFlaggedHashSetKey, "-inf", strconv.FormatInt(time.Now().Unix(), 10))
		p.ZRemRangeByRank(ctx, contentModerationFlaggedHashSetKey, 0, -100001)
		p.Expire(ctx, contentModerationFlaggedHashSetKey, 48*time.Hour)
		return nil
	})
	return err
}

func (c *contentModerationHashCache) HasFlaggedInputHash(ctx context.Context, inputHash string) (bool, error) {
	inputHash = strings.TrimSpace(inputHash)
	if c == nil || c.rdb == nil || inputHash == "" {
		return false, nil
	}
	expires, err := c.rdb.ZScore(ctx, contentModerationFlaggedHashSetKey, inputHash).Result()
	if err == redis.Nil {
		return false, nil
	}
	return expires > float64(time.Now().Unix()), err
}

func (c *contentModerationHashCache) DeleteFlaggedInputHash(ctx context.Context, inputHash string) (bool, error) {
	inputHash = strings.TrimSpace(inputHash)
	if c == nil || c.rdb == nil || inputHash == "" {
		return false, nil
	}
	deleted, err := c.rdb.ZRem(ctx, contentModerationFlaggedHashSetKey, inputHash).Result()
	if err != nil {
		return false, err
	}
	return deleted > 0, nil
}

func (c *contentModerationHashCache) ClearFlaggedInputHashes(ctx context.Context) (int64, error) {
	if c == nil || c.rdb == nil {
		return 0, nil
	}
	deleted, err := c.CountFlaggedInputHashes(ctx)
	if err != nil {
		return 0, err
	}
	if deleted == 0 {
		return 0, nil
	}
	if err := c.rdb.Del(ctx, contentModerationFlaggedHashSetKey).Err(); err != nil {
		return 0, err
	}
	return deleted, nil
}

func (c *contentModerationHashCache) CountFlaggedInputHashes(ctx context.Context) (int64, error) {
	if c == nil || c.rdb == nil {
		return 0, nil
	}
	return c.rdb.ZCount(ctx, contentModerationFlaggedHashSetKey, "("+strconv.FormatInt(time.Now().Unix(), 10), "+inf").Result()
}
