package service

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"math"
	"strings"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/timezone"
)

// poolSyntheticRotationSeconds 合成账号属性的轮换周期，与伪名桶保持一致。
const poolSyntheticRotationSeconds = poolPseudoRotationSeconds

// poolFilterSyntheticUserItems 让合成账号也遵守用户当前选择的筛选条件。
// 少了这一步，「只看 openai」的用户仍然会看到 anthropic 合成号，穿帮。
func poolFilterSyntheticUserItems(items []PoolUserAccount, filter PoolAccountFilter) []PoolUserAccount {
	if len(items) == 0 {
		return items
	}
	search := strings.ToLower(strings.TrimSpace(filter.Search))
	kept := make([]PoolUserAccount, 0, len(items))
	for _, item := range items {
		if filter.Platform != "" && !strings.EqualFold(item.Platform, filter.Platform) {
			continue
		}
		if filter.Type != "" && !strings.EqualFold(item.Type, filter.Type) {
			continue
		}
		if filter.Status != "" && !strings.EqualFold(item.Status, filter.Status) {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(item.Name), search) {
			continue
		}
		kept = append(kept, item)
	}
	return kept
}

// buildSyntheticUserAccounts 为普通用户视图生成合成账号。
//
// 目的只是让号池看起来像一个真实在用的池子：账号名走同一套 7 天伪名，
// 邮箱与额度由 (用户, 序号, 桶) 派生，桶内保持稳定；使用中并发独立按短周期错峰变化。
// 合成账号永远不写库，也不会出现在管理员视图里。
func (s *AccountPoolService) buildSyntheticUserAccounts(_ context.Context, userID int64, plan PoolSyntheticPlan, now time.Time) []PoolUserAccount {
	if plan.empty() || userID <= 0 {
		return nil
	}
	items := make([]PoolUserAccount, 0, plan.Count)
	for index := 0; index < plan.Count; index++ {
		items = append(items, poolSyntheticUserAccount(userID, index, plan, now))
	}
	return items
}

// poolSyntheticUserAccount 生成第 index 个合成账号。
func poolSyntheticUserAccount(userID int64, index int, plan PoolSyntheticPlan, now time.Time) PoolUserAccount {
	// 负数 id 保证与真实账号永远不冲突，前端 row key 也不会撞。
	id := int64(-(index + 1))
	// 活动属性（伪名、并发、最近使用）跟着 7 天桶轮换；额度属性只用 (用户, 序号)
	// 派生。额度窗口必须连续演进：如果它也挂在 7 天桶上，每七天一过，一屏账号的
	// 进度条和倒计时会同时跳到毫不相干的位置，比"额度不动"更容易被看穿。
	seed := poolSyntheticSeed(userID, index, now)
	quotaSeed := poolSyntheticQuotaSeed(userID, index)

	platform := strings.TrimSpace(plan.Platform)
	if platform == "" {
		platform = DefaultPoolUserSyntheticPlatform
	}

	banned := index >= plan.available()
	status := poolSyntheticStatusActive
	if banned {
		status = poolSyntheticBannedStatus(index)
	}

	// 真实池的忙闲程度（0..1）。池子里一个人都没在请求的时候，合成账号不能还在
	// 自己涨今日用量、占着并发槽位 —— 那是整屏数字里最先被看穿的一处。
	demand := 1.0
	if plan.LoadRatio != nil {
		demand = math.Min(math.Max(*plan.LoadRatio, 0), 1)
	}

	// 上限固定为 3，使用中并发独立变化，不影响邮箱和额度的轮换。
	concurrency := 3
	inUse := poolSyntheticInUse(seed, concurrency, now)
	if !poolSyntheticFrequent(index) || demand < poolSyntheticIdleDemand {
		// 低频档位的号不会同时"三小时没动"又"占着两个槽位"；池子闲着的时候
		// 所有合成号都该是 0 并发，否则没人发请求却一屏都在占槽。
		inUse = 0
	}

	// 用量强度：5 小时窗口、7 天窗口和今日口径共用同一个强度，重度的号三个口径
	// 都吃紧。分开各算一次会露出"5 小时已经 90%、今日只有两个请求"这类矛盾。
	intensity := float64((quotaSeed>>32)%1001) / 1000

	// 额度窗口按「相位 + 阶梯」推导：读数只取决于账号与当前时刻，倒计时因此真的
	// 在往下走，而不是每次访问都把重置时间往后推一段。
	fiveHourProgress, fiveHourResetsAt := poolSyntheticWindowProgress(quotaSeed, poolSyntheticFiveHourPeriod, now)
	sevenDayProgress, sevenDayResetsAt := poolSyntheticWindowProgress(quotaSeed>>17, poolSyntheticSevenDayPeriod, now)
	// 阶梯宽度决定"这个数字多久动一次"：5 小时窗口按 2–3 分钟一格，7 天窗口按
	// 一小时一格。再宽就会整天不动（周额度第一天整整 24 小时挂在 0%），再窄就是
	// 每次刷新都在跳 —— 两种都不像真的。
	fiveHour := poolSyntheticStepUtilization(40+60*intensity, fiveHourProgress, 90+(quotaSeed>>48)%61)
	sevenDay := poolSyntheticStepUtilization(25+75*intensity, sevenDayProgress, 150+(quotaSeed>>40)%37)

	// 今日用量按配置时区「今天已过的比例」推进，跨零点自然归零。必须用
	// timezone.StartOfDay：直接取 now.Unix()%86400 是 UTC 日界，在北京时间早上
	// 八点归零，和真实账号（本地零点归零）并排看就是破绽。
	dayProgress := 0.0
	dayStart := timezone.StartOfDay(now)
	dayLength := timezone.EndOfDay(now).Add(time.Nanosecond).Sub(dayStart)
	if dayLength > 0 {
		dayProgress = math.Min(math.Max(now.Sub(dayStart).Seconds()/dayLength.Seconds(), 0), 1)
	}
	requests := int64((4 + 40*intensity) * (0.15 + 0.85*dayProgress) * (0.02 + 0.98*demand))
	tokens := requests * int64(900+(quotaSeed>>24)%2100)   // 每请求 900..3000 tokens
	cost := float64(tokens) * poolSyntheticTokenPriceUSD // 粗略折算，只用于展示

	if banned {
		// 不可用的账号：额度打满、今日没有新流量，符合"跑废了"的样子。
		inUse = 0
		fiveHour = 100
		sevenDay = 100
		requests = 0
		tokens = 0
		cost = 0
	}

	// 池子闲着的时候，"最近使用"也该落在几小时前的粗网格上，而不是每 20 秒
	// 刷新一次都显示刚刚用过。
	lastUsed := poolSyntheticLastUsed(seed, index, banned, now)
	if !banned && demand < poolSyntheticIdleDemand {
		at := poolSyntheticCoarseMoment(now, int64(3*60*60), seed>>28)
		lastUsed = &at
	}

	return PoolUserAccount{
		ID:          id,
		Name:        poolPseudoAccountName(userID, id, now),
		Platform:    platform,
		Type:        poolSyntheticAccountType,
		Status:      status,
		Schedulable: !banned,
		LastUsedAt:  poolFormatTimePtr(lastUsed),
		Today: PoolWindow{
			Requests:     requests,
			Tokens:       tokens,
			Cost:         cost,
			StandardCost: cost,
			UserCost:     cost,
		},
		Capacity:  PoolUserCapacity{InUse: inUse, Concurrency: concurrency},
		FiveHour:  poolSyntheticWindow(fiveHour, fiveHourResetsAt, now),
		SevenDay:  poolSyntheticWindow(sevenDay, sevenDayResetsAt, now),
		Synthetic: true,
	}
}

const (
	poolSyntheticAccountType    = "oauth"
	poolSyntheticStatusActive   = "active"
	poolSyntheticStatusError    = "error"
	poolSyntheticStatusDisabled = "disabled"
	// poolSyntheticTokenPriceUSD 合成账号每 token 的展示单价，仅用于让今日费用
	// 与今日 tokens 保持量级一致，不参与任何计费。
	poolSyntheticTokenPriceUSD = 0.0000025

	// poolSyntheticIdleDemand 判定「池子闲着」的负载阈值。低于它就当作没人在
	// 请求：不占并发槽、今日用量不涨、最近使用时间落到几小时前的粗网格。
	poolSyntheticIdleDemand = 0.05
)

// poolSyntheticBannedStatus 不可用账号的状态：error 与 disabled 交替，
// 避免整列清一色同一种故障，看起来不像是人为摆出来的。
func poolSyntheticBannedStatus(index int) string {
	if index%2 == 0 {
		return poolSyntheticStatusError
	}
	return poolSyntheticStatusDisabled
}

// poolSyntheticSeed 以 (用户, 序号, 轮换桶) 派生稳定哈希。
func poolSyntheticSeed(userID int64, index int, now time.Time) uint64 {
	bucket := now.Unix() / poolSyntheticRotationSeconds
	hasher := fnv.New64a()
	_, _ = fmt.Fprintf(hasher, "pool-synthetic|%d|%d|%d", userID, index, bucket)
	return hasher.Sum64()
}

// poolSyntheticInUse 按账号错峰，每 20–45 秒派生一份并发快照。
// 同一时间段重复读取结果一致，登录页与分享页共用；不产生数据库或调度状态。
func poolSyntheticInUse(seed uint64, capacity int, now time.Time) int {
	if capacity <= 0 {
		return 0
	}
	period := int64(20 + seed%26)
	offset := int64((seed >> 12) % uint64(period))
	bucket := (now.Unix() + offset) / period
	digest := sha256.Sum256(fmt.Appendf(nil, "pool-synthetic-activity|%d|%d", seed, bucket))
	return int(binary.LittleEndian.Uint64(digest[:8]) % uint64(capacity+1))
}

// 上游额度窗口的周期：会话窗口 5 小时，周窗口 7 天。
const (
	poolSyntheticFiveHourPeriod = 5 * time.Hour
	poolSyntheticSevenDayPeriod = 7 * 24 * time.Hour
)

// poolSyntheticWindowProgress 给出账号在窗口内的位置（0..1）与该窗口的重置时刻。
//
// 相位由 seed 派生，池子里的账号因此各自错峰，不会整池同一分钟一起归零。重置时刻
// 只取决于 (seed, 周期, now)：同一时刻重复读取完全一致，倒计时是真的在往下走，
// 不再是每次访问都取 now+随机分钟。
func poolSyntheticWindowProgress(seed uint64, period time.Duration, now time.Time) (float64, time.Time) {
	// 先把 seed 做一次 splitmix64 混合再取模。直接对 seed>>16 这类位段取模会出事：
	// 7 天窗口是 6.05e14 纳秒，而 48 位的值最大只有 2.8e14，相位会全部挤在周期的
	// 前一半，池子里的周额度就会出现"一批 0%、一批 90%"的假分布。
	mixed := seed + 0x9e3779b97f4a7c15
	mixed = (mixed ^ (mixed >> 30)) * 0xbf58476d1ce4e5b9
	mixed = (mixed ^ (mixed >> 27)) * 0x94d049bb133111eb
	mixed ^= mixed >> 31

	phase := time.Duration(mixed % uint64(period))
	aligned := now.Add(-phase)
	start := aligned.Truncate(period)
	progress := float64(aligned.Sub(start)) / float64(period)
	if progress < 0 {
		progress = 0
	} else if progress >= 1 {
		progress = 1 - 1e-9
	}
	return progress, start.Add(period).Add(phase)
}

// poolSyntheticStepUtilization 把窗口进度量化成阶梯后映射成利用率。
//
// 真实账号在窗口里是「用一阵、停一阵」，读数呈阶梯而不是连续爬升，所以先把进度
// 切成 steps 段，段内保持不变。峰值由用量强度决定（40%–100%），曲线前段快、
// 后段缓：刚开窗时迅速吃上去，接近上限后零星追加。
func poolSyntheticStepUtilization(peak, progress float64, steps uint64) float64 {
	if steps == 0 {
		steps = 1
	}
	step := uint64(progress * float64(steps))
	if step >= steps {
		step = steps - 1
	}
	shape := 1 - math.Pow(1-float64(step)/float64(steps), 1.6)
	value := peak * shape
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

// poolSyntheticWindow 组装一条额度窗口读数。
func poolSyntheticWindow(utilization float64, resetsAt time.Time, now time.Time) PoolUserQuotaWindow {
	remaining := int(resetsAt.Sub(now).Seconds())
	if remaining < 0 {
		remaining = 0
	}
	formatted := resetsAt.In(timezone.Location()).Format(time.RFC3339)
	return PoolUserQuotaWindow{
		Utilization:      utilization,
		ResetsAt:         &formatted,
		RemainingSeconds: remaining,
		Enabled:          true,
	}
}

// poolSyntheticQuotaSeed 只由 (用户, 序号) 派生，不参与 7 天轮换。
//
// 额度窗口是一条连续的时间线：会话窗口每 5 小时滚一次、周窗口每 7 天滚一次，
// 读数必须能一路对得上。曾经把额度挂在带时间桶的 seed 上，跨桶那一刻整池的
// 进度条和倒计时会一起跳到无关位置，谁看一眼都知道是拼的。
func poolSyntheticQuotaSeed(userID int64, index int) uint64 {
	hasher := fnv.New64a()
	_, _ = fmt.Fprintf(hasher, "pool-synthetic-quota|%d|%d", userID, index)
	return hasher.Sum64()
}

// poolSyntheticLastUsed 最近一次被调度的时刻。
//
// 不能写成 now 减一个随机分钟：那样每刷新一次，"最近使用"就整体往前挪一段，
// 同一分钟内看两遍就能发现它在跟着刷新走。这里锚定在该号自己网格的最近一个格点
// —— 网格内完全稳定，跨格才前进一格，且随时间单调不减。
//
// 网格宽度就是这号的使用频率：一半的号一直在跑（格宽 20–45 秒），其余按十分钟、
// 三小时停住。否则一屏账号全是"几秒前"，比全都写死还假。
func poolSyntheticLastUsed(seed uint64, index int, banned bool, now time.Time) *time.Time {
	if banned {
		// 废号早就不再被调度：停在几十小时前的同一时刻，不随网格推进。
		at := poolSyntheticCoarseMoment(now, int64(30*60), seed>>20).
			Add(-time.Duration(30+(seed>>20)%90) * time.Hour)
		return &at
	}
	at := poolSyntheticCoarseMoment(now, poolSyntheticIdlePeriod(seed, index), seed>>28)
	return &at
}

// poolSyntheticTier 给每个号分配一个使用频率档位。
//
// 档位只看序号、不掺时间种子：它决定"最近使用"的网格宽度，网格宽度一变，
// 时间戳就可能往回跳。按序号轮转还有个好处——一屏十二个号里必然同时有刚用完
// 的、十几分钟前用完的和两三小时没轮到的；纯靠哈希抽签很容易整屏都是"几秒前"。
func poolSyntheticTier(index int) int {
	if index < 0 {
		index = 0
	}
	return index % 10
}

// poolSyntheticFrequent 判断这个号是否属于"一直在跑"的档位。
//
// inUse 只给高频号：「最近使用在三小时前」和「正在占用两个槽位」写在同一行，
// 是这块最容易露馅的组合。
func poolSyntheticFrequent(index int) bool {
	return poolSyntheticTier(index) < 5
}

// poolSyntheticIdlePeriod 返回最近使用时间的网格宽度（秒）。
func poolSyntheticIdlePeriod(seed uint64, index int) int64 {
	switch tier := poolSyntheticTier(index); {
	case tier < 5:
		// 与 InUse 共用同一套 period，保证"占着槽"和"刚刚用过"对得上。
		return int64(20 + seed%26)
	case tier < 8:
		return 10 * 60
	default:
		return 3 * 60 * 60
	}
}

// poolSyntheticCoarseMoment 把时间对齐到 period 秒的粗粒度网格，偏移由 salt 决定。
//
// 直接对齐整点会让"最近使用"全是 09:00、09:15、09:30 这种整分钟，一眼就是算
// 出来的；带 salt 的偏移让落点落在任意秒上，同时保持同一网格内完全一致。
func poolSyntheticCoarseMoment(now time.Time, period int64, salt uint64) time.Time {
	if period <= 0 {
		return now
	}
	offset := int64(salt % uint64(period))
	aligned := (now.Unix()+offset)/period*period - offset
	return time.Unix(aligned, 0).In(timezone.Location())
}

// poolPaginateUserItems 按页切分用户视图条目，与真实账号共用同一套边界处理。
func poolPaginateUserItems(items []PoolUserAccount, page, pageSize int) []PoolUserAccount {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		return items
	}
	start, end := len(items), len(items)
	if len(items) > 0 && page-1 <= (len(items)-1)/pageSize {
		start = (page - 1) * pageSize
		end = start + min(pageSize, len(items)-start)
	}
	return items[start:end]
}

// poolAccountGroupMatches 判断账号是否属于指定分组。
func poolAccountGroupMatches(acc *Account, groupID int64) bool {
	if acc == nil || groupID <= 0 {
		return false
	}
	for _, group := range acc.Groups {
		if group != nil && group.ID == groupID {
			return true
		}
	}
	return false
}
