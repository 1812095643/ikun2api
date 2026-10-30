package service

import (
	"context"
	"sort"
	"strings"
)

func plazaGroupMetadata(g *Group) *PlazaGroup {
	return &PlazaGroup{
		ID: g.ID, Name: g.Name, Description: g.Description, Platform: g.Platform,
		SubscriptionType: g.SubscriptionType, RateMultiplier: g.RateMultiplier,
		PeakRateEnabled: g.PeakRateEnabled, PeakStart: g.PeakStart, PeakEnd: g.PeakEnd,
		PeakRateMultiplier: g.PeakRateMultiplier, IsExclusive: g.IsExclusive,
		ImageRateIndependent: g.ImageRateIndependent, ImageRateMultiplier: g.ImageRateMultiplier,
		VideoRateIndependent: g.VideoRateIndependent, VideoRateMultiplier: g.VideoRateMultiplier,
		LongContextPricingEnabled: g.LongContextPricingEnabled,
	}
}

// ListWorkspaceGroups 为尚未配置渠道目录的普通分组补齐现有网关模型列表。
// 已配置渠道的分组继续使用原目录；定价复用实收解析，权限仍由 handler 统一过滤。
func (s *ModelPlazaService) ListWorkspaceGroups(ctx context.Context, listModels func(context.Context, *Group) []string) ([]PlazaGroup, error) {
	result, err := s.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	groups, err := s.groupRepo.ListActive(ctx)
	if err != nil {
		return nil, err
	}
	configured := make(map[int64]bool, len(result))
	for _, group := range result {
		configured[group.ID] = true
	}
	officialMemo := make(map[string]*PlazaOfficialPricing)
	for i := range groups {
		group := &groups[i]
		if configured[group.ID] || !isConcreteRequestPlatform(group.Platform) {
			continue
		}
		entry := plazaGroupMetadata(group)
		seen := make(map[string]bool)
		for _, raw := range listModels(ctx, group) {
			name := strings.TrimSpace(raw)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			model := PlazaModel{Name: name, Platform: group.Platform}
			s.fillDisplayPricing(ctx, &model, group)
			model.OfficialPricing = s.lookupOfficialPricing(ctx, name, officialMemo)
			entry.Models = append(entry.Models, model)
		}
		if len(entry.Models) == 0 {
			continue
		}
		sort.SliceStable(entry.Models, func(i, j int) bool { return entry.Models[i].Name < entry.Models[j].Name })
		result = append(result, *entry)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].RateMultiplier != result[j].RateMultiplier {
			return result[i].RateMultiplier < result[j].RateMultiplier
		}
		return result[i].Name < result[j].Name
	})
	return result, nil
}
