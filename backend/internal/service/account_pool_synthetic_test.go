package service

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/timezone"
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
			require.InDelta(t, first.SevenDay.Utilization, item.SevenDay.Utilization, 1.5, "weekly usage may drift, but must not jump")
			require.GreaterOrEqual(t, item.SevenDay.Utilization, first.SevenDay.Utilization, "weekly usage only climbs inside a window")
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
		if first.Schedulable && poolSyntheticFrequent(index) {
			require.True(t, changes[index], "active synthetic account %d must change within five minutes", index)
		}
		if first.Schedulable && !poolSyntheticFrequent(index) {
			require.Zero(t, first.Capacity.InUse, "an account idle for hours must not hold a slot")
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

func TestAccountPoolSyntheticWindowProgressCountsDown(t *testing.T) {
	seed := uint64(0x9e3779b97f4a7c15)
	now := time.Date(2026, 11, 6, 8, 0, 0, 0, time.UTC)
	progress, resetsAt := poolSyntheticWindowProgress(seed, poolSyntheticFiveHourPeriod, now)
	require.GreaterOrEqual(t, progress, 0.0)
	require.Less(t, progress, 1.0)
	require.True(t, resetsAt.After(now), "a window must reset in the future")
	require.LessOrEqual(t, resetsAt.Sub(now), poolSyntheticFiveHourPeriod)

	// 同一窗口内：重置时刻固定不变，进度单调前进。
	later := now.Add(2 * time.Hour)
	progressLater, resetsAtLater := poolSyntheticWindowProgress(seed, poolSyntheticFiveHourPeriod, later)
	if later.Before(resetsAt) {
		require.Equal(t, resetsAt, resetsAtLater, "the reset time must not slide forward on every read")
		require.Greater(t, progressLater, progress)
	}

	// 越过重置点后：进度归零，窗口整体顺延一个周期。
	progressNext, resetsAtNext := poolSyntheticWindowProgress(seed, poolSyntheticFiveHourPeriod, resetsAt.Add(time.Second))
	require.Less(t, progressNext, 0.01)
	require.Equal(t, resetsAt.Add(poolSyntheticFiveHourPeriod), resetsAtNext)
}

func TestAccountPoolSyntheticStepUtilizationIsMonotonic(t *testing.T) {
	const steps = uint64(37)
	previous := 0.0
	for step := 0; step < int(steps); step++ {
		progress := (float64(step) + 0.5) / float64(steps)
		value := poolSyntheticStepUtilization(90, progress, steps)
		require.GreaterOrEqual(t, value, previous, "usage inside one window only climbs")
		require.LessOrEqual(t, value, 100.0)
		previous = value
	}
	require.Zero(t, poolSyntheticStepUtilization(90, 0, steps))
	require.Equal(t, 100.0, poolSyntheticStepUtilization(120, 1, steps))
	require.Zero(t, poolSyntheticStepUtilization(90, 0.5, 0))
}

func TestAccountPoolSyntheticQuotaWindowsVaryPerAccount(t *testing.T) {
	now := time.Date(2026, 11, 6, 8, 0, 0, 0, time.UTC)
	plan := PoolSyntheticPlan{Count: 12, Banned: 2, Platform: "anthropic"}
	fiveHourResets := map[string]bool{}
	sevenDayResets := map[string]bool{}
	fiveHourValues := map[float64]bool{}
	for index := 0; index < plan.Count; index++ {
		item := poolSyntheticUserAccount(42, index, plan, now)
		require.NotNil(t, item.FiveHour.ResetsAt)
		require.NotNil(t, item.SevenDay.ResetsAt)
		require.True(t, item.FiveHour.Enabled)
		require.True(t, item.SevenDay.Enabled)
		require.Greater(t, item.FiveHour.RemainingSeconds, 0)
		require.LessOrEqual(t, item.FiveHour.RemainingSeconds, int(poolSyntheticFiveHourPeriod.Seconds()))
		require.Greater(t, item.SevenDay.RemainingSeconds, 0)
		require.LessOrEqual(t, item.SevenDay.RemainingSeconds, int(poolSyntheticSevenDayPeriod.Seconds()))
		if !item.Schedulable {
			require.Equal(t, 100.0, item.FiveHour.Utilization)
			require.Equal(t, 100.0, item.SevenDay.Utilization)
			continue
		}
		fiveHourResets[*item.FiveHour.ResetsAt] = true
		sevenDayResets[*item.SevenDay.ResetsAt] = true
		fiveHourValues[item.FiveHour.Utilization] = true
	}
	require.Greater(t, len(fiveHourResets), 1, "accounts must not reset their session window at the same minute")
	require.Greater(t, len(sevenDayResets), 1, "accounts must not share one weekly reset moment")
	require.Greater(t, len(fiveHourValues), 1, "session utilization must not be one flat number")
}

func TestAccountPoolSyntheticTodayGrowsThroughTheDay(t *testing.T) {
	plan := PoolSyntheticPlan{Count: 3, Banned: 0, Platform: "anthropic"}
	loc := timezone.Location()
	morning := poolSyntheticUserAccount(42, 0, plan, time.Date(2026, 11, 6, 1, 0, 0, 0, loc))
	evening := poolSyntheticUserAccount(42, 0, plan, time.Date(2026, 11, 6, 20, 0, 0, 0, loc))
	require.Less(t, morning.Today.Requests, evening.Today.Requests)
	require.Less(t, morning.Today.Tokens, evening.Today.Tokens)
	require.Less(t, morning.Today.Cost, evening.Today.Cost)

	// 日界必须跟真实账号一致：本地零点归零，而不是 UTC 零点（北京时间早上八点）。
	justAfterMidnight := poolSyntheticUserAccount(42, 0, plan, time.Date(2026, 11, 7, 0, 5, 0, 0, loc))
	require.Less(t, justAfterMidnight.Today.Requests, evening.Today.Requests, "a new local day starts from a clean slate")
	require.Less(t, justAfterMidnight.Today.Requests, int64(20))
	// 同一时刻按 UTC 表述时，"今天"必须是本地那一天，不能提前归零。
	beforeMidnight := poolSyntheticUserAccount(42, 0, plan, time.Date(2026, 11, 6, 23, 50, 0, 0, loc))
	require.Greater(t, beforeMidnight.Today.Requests, justAfterMidnight.Today.Requests)
}

func TestAccountPoolSyntheticLastUsedOnlyMovesForward(t *testing.T) {
	now := time.Date(2026, 11, 6, 9, 0, 0, 0, timezone.Location())
	plan := PoolSyntheticPlan{Count: 12, Banned: 2, Platform: "anthropic"}
	for index := 0; index < plan.Count; index++ {
		var previous time.Time
		for seconds := 0; seconds <= 1800; seconds += 5 {
			at := now.Add(time.Duration(seconds) * time.Second)
			item := poolSyntheticUserAccount(42, index, plan, at)
			require.NotNil(t, item.LastUsedAt)
			stamp, err := time.Parse(time.RFC3339, *item.LastUsedAt)
			require.NoError(t, err)
			require.False(t, stamp.After(at), "last_used_at must never point into the future")
			if !previous.IsZero() {
				require.False(t, stamp.Before(previous), "last_used_at must not drift backwards while the page is open")
			}
			previous = stamp
		}
	}
}

func TestAccountPoolSyntheticLastUsedBucketsAreStable(t *testing.T) {
	now := time.Date(2026, 11, 6, 9, 0, 0, 0, timezone.Location())
	plan := PoolSyntheticPlan{Count: 12, Banned: 0, Platform: "anthropic"}
	// 连续两秒读取必须完全一致：否则每次刷新"最近使用"都在爬，比不动还假。
	for index := 0; index < plan.Count; index++ {
		first := poolSyntheticUserAccount(42, index, plan, now)
		second := poolSyntheticUserAccount(42, index, plan, now.Add(time.Second))
		require.Equal(t, *first.LastUsedAt, *second.LastUsedAt)
	}
	// 全池不能都是一个"刚刚"，也不能全是很久以前。
	fresh, stale := 0, 0
	for index := 0; index < plan.Count; index++ {
		item := poolSyntheticUserAccount(42, index, plan, now)
		stamp, err := time.Parse(time.RFC3339, *item.LastUsedAt)
		require.NoError(t, err)
		switch age := now.Sub(stamp); {
		case age < 2*time.Minute:
			fresh++
		case age > 30*time.Minute:
			stale++
		}
	}
	require.GreaterOrEqual(t, fresh, 3, "part of the pool should be actively running")
	require.GreaterOrEqual(t, stale, 1, "part of the pool should be visibly idle")
}

func TestAccountPoolSyntheticQuotaSurvivesTheNameRotation(t *testing.T) {
	plan := PoolSyntheticPlan{Count: 6, Banned: 0, Platform: "anthropic"}
	// 7 天伪名桶的边界上，名字会换，但额度读数必须接着上一秒继续走 ——
	// 整池进度条和倒计时同时跳到无关位置，是最容易被看穿的破绽。
	edge := time.Unix((time.Date(2026, 11, 6, 9, 0, 0, 0, timezone.Location()).Unix()/poolPseudoRotationSeconds+1)*poolPseudoRotationSeconds, 0)
	before := poolSyntheticUserAccount(42, 0, plan, edge.Add(-time.Second))
	after := poolSyntheticUserAccount(42, 0, plan, edge.Add(time.Second))
	require.InDelta(t, before.FiveHour.Utilization, after.FiveHour.Utilization, 2.0)
	require.InDelta(t, before.SevenDay.Utilization, after.SevenDay.Utilization, 2.0)
	require.Equal(t, *before.SevenDay.ResetsAt, *after.SevenDay.ResetsAt)
	// 今日用量不参与这条断言：桶边界必然落在某个整周刻度上，可能是本地零点前后，
	// 跨过零点时它本来就该归零。今日口径只由「今日已过的比例」和用量强度决定，
	// 完全不读 7 天桶，这一点由 TestAccountPoolSyntheticTodayGrowsThroughTheDay 覆盖。
}

func TestAccountPoolSyntheticQuotaFollowsRealLoad(t *testing.T) {
	now := time.Date(2026, 11, 6, 14, 0, 0, 0, timezone.Location())
	idle, busy := 0.0, 1.0
	plan := PoolSyntheticPlan{Count: 8, Banned: 0, Platform: "anthropic"}
	idlePlan := plan
	idlePlan.LoadRatio = &idle
	busyPlan := plan
	busyPlan.LoadRatio = &busy

	busySlots := 0
	for index := 0; index < plan.Count; index++ {
		idleItem := poolSyntheticUserAccount(42, index, idlePlan, now)
		busyItem := poolSyntheticUserAccount(42, index, busyPlan, now)

		require.Zero(t, idleItem.Capacity.InUse, "an idle pool must not hold concurrency slots")
		require.Zero(t, idleItem.Today.Requests, "an idle pool must not accumulate today usage")
		require.Zero(t, idleItem.Today.Tokens)
		require.Greater(t, busyItem.Today.Requests, int64(0), "a busy pool keeps plausible traffic")
		if !poolSyntheticFrequent(index) {
			// 低频档位本来就不占槽位，"几小时没动静"与"零并发"是自洽的。
			require.Zero(t, busyItem.Capacity.InUse)
		}
		busySlots += busyItem.Capacity.InUse

		// 窗口额度是历史累积量，不随瞬时负载倒退 —— 否则一没人用，进度条就自己
		// 往回缩，比不动更容易被看穿。
		require.InDelta(t, busyItem.FiveHour.Utilization, idleItem.FiveHour.Utilization, 0.001)
		require.InDelta(t, busyItem.SevenDay.Utilization, idleItem.SevenDay.Utilization, 0.001)
		require.Equal(t, *busyItem.FiveHour.ResetsAt, *idleItem.FiveHour.ResetsAt)
	}
	require.Greater(t, busySlots, 0, "a busy pool keeps at least a few slots taken")
}

func TestPoolRealLoadRatio(t *testing.T) {
	require.Nil(t, poolRealLoadRatio(nil), "no accounts means no signal")
	require.Nil(t, poolRealLoadRatio([]PoolUserAccount{
		{Capacity: PoolUserCapacity{InUse: 0, Concurrency: 0}},
	}), "accounts without a concurrency limit carry no load signal")

	half := []PoolUserAccount{
		{Capacity: PoolUserCapacity{InUse: 3, Concurrency: 3}},
		{Capacity: PoolUserCapacity{InUse: 0, Concurrency: 3}},
	}
	require.InDelta(t, 0.5, *poolRealLoadRatio(half), 0.001)

	// 合成账号自己不能进分母，否则展示行越多比例越假。
	mixed := append(append([]PoolUserAccount{}, half...), PoolUserAccount{
		Synthetic: true,
		Capacity:  PoolUserCapacity{InUse: 3, Concurrency: 3},
	})
	require.InDelta(t, 0.5, *poolRealLoadRatio(mixed), 0.001)

	over := []PoolUserAccount{{Capacity: PoolUserCapacity{InUse: 9, Concurrency: 3}}}
	require.InDelta(t, 1.0, *poolRealLoadRatio(over), 0.001, "oversubscription clamps at 1")
}
