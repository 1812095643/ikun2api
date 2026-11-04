package service

import (
	"context"
	"strconv"
	"strings"
)

// 实时号池「用户视图」展示设置。
//
// 这四个配置只决定普通用户在号池页看到什么，不参与调度、计费与鉴权，
// 也不会在数据库里留下任何合成账号 —— 关掉开关后用户视图立刻回到真实数据。
const (
	// DefaultPoolUserSyntheticCount 合成账号数量的默认值（0 表示关闭）。
	DefaultPoolUserSyntheticCount = 0
	// DefaultPoolUserSyntheticBanned 合成账号中显示为不可用的默认数量。
	DefaultPoolUserSyntheticBanned = 0
	// DefaultPoolUserSyntheticPlatform 合成账号的默认平台。
	DefaultPoolUserSyntheticPlatform = "anthropic"
	// DefaultPoolUserVisibleGroupID 用户端只展示该分组下的真实账号（0 表示不限）。
	DefaultPoolUserVisibleGroupID int64 = 0
	// poolUserSyntheticMaxCount 合成账号数量上限，避免一次渲染出上千行。
	poolUserSyntheticMaxCount = 50
)

// poolSyntheticPlatformWhitelist 合成账号允许展示的平台，落到白名单外的值
// 一律退回默认值，避免前台出现无法渲染的未知平台。
var poolSyntheticPlatformWhitelist = []string{"anthropic", "openai", "gemini", "claude"}

// clampPoolSyntheticCount 把数量夹在 [0, poolUserSyntheticMaxCount]。
func clampPoolSyntheticCount(value int) int {
	if value < 0 {
		return 0
	}
	if value > poolUserSyntheticMaxCount {
		return poolUserSyntheticMaxCount
	}
	return value
}

// parsePoolUserSyntheticCount 解析合成账号数量。
func parsePoolUserSyntheticCount(value string) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return DefaultPoolUserSyntheticCount
	}
	return clampPoolSyntheticCount(parsed)
}

// parsePoolUserSyntheticBanned 解析合成账号中不可用的数量。
func parsePoolUserSyntheticBanned(value string) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return DefaultPoolUserSyntheticBanned
	}
	return clampPoolSyntheticCount(parsed)
}

// parsePoolUserSyntheticPlatform 解析合成账号平台。
func parsePoolUserSyntheticPlatform(value string) string {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if trimmed == "" {
		return DefaultPoolUserSyntheticPlatform
	}
	for _, allowed := range poolSyntheticPlatformWhitelist {
		if trimmed == allowed {
			return trimmed
		}
	}
	return DefaultPoolUserSyntheticPlatform
}

// parsePoolUserVisibleGroupID 解析用户端可见分组（0 表示不限）。
func parsePoolUserVisibleGroupID(value string) int64 {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || parsed < 0 {
		return DefaultPoolUserVisibleGroupID
	}
	return parsed
}

// GetPoolUserSyntheticCount 用户视图额外展示的合成账号数量（0 表示关闭）。
func (s *SettingService) GetPoolUserSyntheticCount(ctx context.Context) int {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyPoolUserSyntheticCount)
	if err != nil {
		return DefaultPoolUserSyntheticCount
	}
	return parsePoolUserSyntheticCount(value)
}

// GetPoolUserSyntheticBanned 合成账号中显示为不可用的数量。
func (s *SettingService) GetPoolUserSyntheticBanned(ctx context.Context) int {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyPoolUserSyntheticBanned)
	if err != nil {
		return DefaultPoolUserSyntheticBanned
	}
	return parsePoolUserSyntheticBanned(value)
}

// GetPoolUserSyntheticPlatform 合成账号展示的平台。
func (s *SettingService) GetPoolUserSyntheticPlatform(ctx context.Context) string {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyPoolUserSyntheticPlatform)
	if err != nil {
		return DefaultPoolUserSyntheticPlatform
	}
	return parsePoolUserSyntheticPlatform(value)
}

// GetPoolUserVisibleGroupID 用户端只展示该分组下的真实账号（0 表示不限）。
func (s *SettingService) GetPoolUserVisibleGroupID(ctx context.Context) int64 {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyPoolUserVisibleGroupID)
	if err != nil {
		return DefaultPoolUserVisibleGroupID
	}
	return parsePoolUserVisibleGroupID(value)
}

// PoolSyntheticPlan 一次用户视图渲染需要的合成账号参数。
type PoolSyntheticPlan struct {
	Count    int
	Banned   int
	Platform string
}

// empty 表示本次不需要追加任何合成账号。
func (p PoolSyntheticPlan) empty() bool { return p.Count <= 0 }

// available 返回显示为可用的合成账号数量。
func (p PoolSyntheticPlan) available() int { return p.Count - p.Banned }

// poolSyntheticPlan 读取管理员配置，得出本次要追加的合成账号。
// 设置读取失败或未注入 SettingService 时一律返回空计划，绝不影响真实数据。
func (s *AccountPoolService) poolSyntheticPlan(ctx context.Context) PoolSyntheticPlan {
	if s.settingService == nil {
		return PoolSyntheticPlan{}
	}
	count := s.settingService.GetPoolUserSyntheticCount(ctx)
	if count <= 0 {
		return PoolSyntheticPlan{}
	}
	banned := s.settingService.GetPoolUserSyntheticBanned(ctx)
	if banned > count {
		banned = count
	}
	return PoolSyntheticPlan{
		Count:    count,
		Banned:   banned,
		Platform: s.settingService.GetPoolUserSyntheticPlatform(ctx),
	}
}

// poolUserVisibleGroupID 用户端只展示该分组下的真实账号（0 表示不限）。
func (s *AccountPoolService) poolUserVisibleGroupID(ctx context.Context) int64 {
	if s.settingService == nil {
		return DefaultPoolUserVisibleGroupID
	}
	return s.settingService.GetPoolUserVisibleGroupID(ctx)
}
