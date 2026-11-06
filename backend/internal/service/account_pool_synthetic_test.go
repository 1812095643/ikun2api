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

func TestAccountPoolSyntheticAliasesAndCapacity(t *testing.T) {
	now := time.Date(2026, 11, 6, 8, 0, 0, 0, time.UTC)
	service := NewAccountPoolService(nil, nil, nil, nil, nil)
	plan := PoolSyntheticPlan{Count: 50, Banned: 5, Platform: "anthropic"}
	first := service.buildSyntheticUserAccounts(context.Background(), 42, plan, now)
	second := service.buildSyntheticUserAccounts(context.Background(), 42, plan, now.Add(time.Second))
	names := map[string]bool{}
	for index, item := range first {
		require.Regexp(t, `^[A-Z][a-z]+\*{3}[a-z]{2,5}[0-9]{2,4}@(outlook\.(com|jp)|hotmail\.com|live\.com)$`, item.Name)
		require.Equal(t, item.Name, second[index].Name)
		require.False(t, names[item.Name], "synthetic account labels must not collide within a pool")
		names[item.Name] = true
		require.Equal(t, 3, item.Capacity.Concurrency)
		require.GreaterOrEqual(t, item.Capacity.InUse, 0)
		require.LessOrEqual(t, item.Capacity.InUse, 3)
	}
}
