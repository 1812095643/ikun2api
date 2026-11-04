package service

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAccountPoolSyntheticDefaultsAndPagination(t *testing.T) {
	svc := NewAccountPoolService(nil, nil, nil, nil, nil)
	require.True(t, svc.poolSyntheticPlan(context.Background()).empty())
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	plan := PoolSyntheticPlan{Count: 3, Banned: 1, Platform: "anthropic"}
	items := svc.buildSyntheticUserAccounts(context.Background(), 42, plan, now)
	require.Len(t, items, 3)
	for _, item := range items {
		require.True(t, item.Synthetic)
		require.Negative(t, item.ID)
	}
	require.True(t, items[0].Schedulable)
	require.False(t, items[2].Schedulable)
	require.Empty(t, poolPaginateUserItems(items, math.MaxInt, 200))
	require.Len(t, poolPaginateUserItems(items, 1, 2), 2)
	require.Len(t, poolPaginateUserItems(items, 2, 2), 1)
	require.Empty(t, poolFilterSyntheticUserItems(items, PoolAccountFilter{Platform: "gemini"}))
}
