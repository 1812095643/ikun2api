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

func TestAccountPoolSyntheticConcurrencyChangesOverTime(t *testing.T) {
	now := time.Date(2026, 11, 6, 8, 0, 0, 0, time.UTC)
	plan := PoolSyntheticPlan{Count: 12, Banned: 2, Platform: "anthropic"}
	changes := make(map[int]bool)
	transitionTimes := make(map[int64]bool)
	for index := 0; index < plan.Count; index++ {
		first := poolSyntheticUserAccount(42, index, plan, now)
		previous := first.Capacity.InUse
		for seconds := int64(0); seconds <= 300; seconds++ {
			at := now.Add(time.Duration(seconds) * time.Second)
			item := poolSyntheticUserAccount(42, index, plan, at)
			require.Equal(t, item.Capacity, poolSyntheticUserAccount(42, index, plan, at).Capacity)
			require.Equal(t, first.Name, item.Name, "activity must not rotate the account label")
			require.Equal(t, first.SevenDay.Utilization, item.SevenDay.Utilization)
			require.Equal(t, 3, item.Capacity.Concurrency)
			require.GreaterOrEqual(t, item.Capacity.InUse, 0)
			require.LessOrEqual(t, item.Capacity.InUse, 3)
			if !item.Schedulable {
				require.Zero(t, item.Capacity.InUse)
			}
			if item.Capacity.InUse != previous {
				changes[index] = true
				transitionTimes[seconds] = true
			}
			previous = item.Capacity.InUse
		}
		if first.Schedulable {
			require.True(t, changes[index], "active synthetic account %d must change within five minutes", index)
		}
	}
	require.Greater(t, len(transitionTimes), 10, "accounts must not all switch at one shared instant")
}

func TestAccountPoolSyntheticConcurrencySnapshotsAreStableAndScoped(t *testing.T) {
	now := time.Date(2026, 11, 6, 8, 0, 0, 0, time.UTC)
	seed := poolSyntheticSeed(42, 0, now)
	period := int64(20 + seed%26)
	offset := int64((seed >> 12) % uint64(period))
	start := time.Unix((now.Unix()+offset)/period*period-offset, 0)
	for second := int64(0); second < period; second++ {
		require.Equal(t, poolSyntheticInUse(seed, 3, start), poolSyntheticInUse(seed, 3, start.Add(time.Duration(second)*time.Second)))
	}
	first := []int{}
	otherAccount := []int{}
	otherUser := []int{}
	for second := 0; second < 300; second += 30 {
		at := now.Add(time.Duration(second) * time.Second)
		first = append(first, poolSyntheticInUse(seed, 3, at))
		otherAccount = append(otherAccount, poolSyntheticInUse(poolSyntheticSeed(42, 1, now), 3, at))
		otherUser = append(otherUser, poolSyntheticInUse(poolSyntheticSeed(43, 0, now), 3, at))
	}
	require.NotEqual(t, first, otherAccount)
	require.NotEqual(t, first, otherUser)
	require.Zero(t, poolSyntheticInUse(seed, 0, now))
	require.Zero(t, poolSyntheticInUse(seed, -1, now))
}
