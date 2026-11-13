//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func downstreamBool(v bool) *bool { return &v }
func TestDownstreamChannelPriceScopeBillingAndCacheComponents(t *testing.T) {
	for _, model := range []string{"gpt-5.6-luna", "claude-sonnet-4"} {
		for _, enabled := range []bool{false, true} {
			for _, downstream := range []bool{false, true} {
				t.Run(model+"/"+map[bool]string{true: "applies", false: "excluded"}[enabled]+"/"+map[bool]string{true: "reseller", false: "regular"}[downstream], func(t *testing.T) {
					r := newResolverWithChannel(t, []ChannelModelPricing{{Platform: "anthropic", Models: []string{model}, BillingMode: BillingModeToken, ApplyToDownstream: downstreamBool(enabled), InputPrice: pricingMultiplier(9e-6), OutputPrice: pricingMultiplier(40e-6), CacheWritePrice: pricingMultiplier(11e-6), CacheReadPrice: pricingMultiplier(3e-6)}})
					r.billingService.initFallbackPricing()
					r.billingService.fallbackPrices[model] = &ModelPricing{InputPricePerToken: 4e-6, OutputPricePerToken: 20e-6, CacheCreationPricePerToken: 5e-6, CacheReadPricePerToken: 0.4e-6}
					baseline, baseErr := r.billingService.CalculateCost(model, UsageTokens{InputTokens: 1000000, OutputTokens: 1000000, CacheCreationTokens: 1000000, CacheReadTokens: 1000000}, 0.5)
					require.NoError(t, baseErr)
					gid := int64(100)
					resolved := r.Resolve(context.Background(), PricingInput{Model: model, GroupID: &gid, IsDownstream: downstream})
					channel := !downstream || enabled
					if channel {
						require.Equal(t, PricingSourceChannel, resolved.Source)
					} else {
						require.NotEqual(t, PricingSourceChannel, resolved.Source)
					}
					cost, e := r.billingService.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: model, GroupID: &gid, IsDownstream: downstream, Tokens: UsageTokens{InputTokens: 1000000, OutputTokens: 1000000, CacheCreationTokens: 1000000, CacheReadTokens: 1000000}, RateMultiplier: 0.5, Resolver: r})
					require.NoError(t, e)
					if channel {
						require.InDelta(t, 9, cost.InputCost, 1e-8)
						require.InDelta(t, 40, cost.OutputCost, 1e-8)
						require.InDelta(t, 11, cost.CacheCreationCost, 1e-8)
						require.InDelta(t, 3, cost.CacheReadCost, 1e-8)
						require.InDelta(t, 31.5, cost.ActualCost, 1e-8)
					} else {
						require.InDelta(t, baseline.InputCost, cost.InputCost, 1e-8)
						require.InDelta(t, baseline.OutputCost, cost.OutputCost, 1e-8)
						require.InDelta(t, baseline.CacheCreationCost, cost.CacheCreationCost, 1e-8)
						require.InDelta(t, baseline.CacheReadCost, cost.CacheReadCost, 1e-8)
						require.InDelta(t, baseline.ActualCost, cost.ActualCost, 1e-8)
					}
				})
			}
		}
	}
}
func TestDownstreamChannelScopePreservesDefaultAndDoesNotChangeGroupCard(t *testing.T) {
	r := newResolverWithChannel(t, []ChannelModelPricing{{Platform: "anthropic", Models: []string{"claude-sonnet-4"}, InputPrice: pricingMultiplier(9e-6)}})
	gid := int64(100)
	require.Equal(t, PricingSourceChannel, r.Resolve(context.Background(), PricingInput{Model: "claude-sonnet-4", GroupID: &gid, IsDownstream: true}).Source)
	g := &Group{ID: 100, LongContextPricingEnabled: true, ModelPricing: []ChannelModelPricing{{Models: []string{"claude-sonnet-4"}, ApplyToDownstream: downstreamBool(false), InputPrice: pricingMultiplier(7e-6)}}}
	resolved := r.Resolve(context.Background(), PricingInput{Model: "claude-sonnet-4", GroupID: &gid, Group: g, IsDownstream: true})
	require.Equal(t, PricingSourceGroup, resolved.Source)
	require.InDelta(t, 7e-6, resolved.BasePricing.InputPricePerToken, 1e-12)
}
func TestDownstreamPricingDisplayScheduleMatchesBilling(t *testing.T) {
	r := newResolverWithChannel(t, []ChannelModelPricing{{Platform: "anthropic", Models: []string{"claude-sonnet-4"}, InputPrice: pricingMultiplier(9e-6), ApplyToDownstream: downstreamBool(false)}})
	g := &Group{ID: 100, LongContextPricingEnabled: true}
	normal, e := r.billingService.ResolveContextPricingSchedule(context.Background(), r, ContextPricingScheduleInput{Model: "claude-sonnet-4", Group: g})
	require.NoError(t, e)
	reseller, e := r.billingService.ResolveContextPricingSchedule(WithDownstreamPricing(context.Background(), true), r, ContextPricingScheduleInput{Model: "claude-sonnet-4", Group: g})
	require.NoError(t, e)
	require.InDelta(t, 9e-6, *normal.Tiers[0].Input, 1e-12)
	require.InDelta(t, 3e-6, *reseller.Tiers[0].Input, 1e-12)
	// An account statistics calculation lacks a customer identity, so it retains channel pricing.
	require.Equal(t, PricingSourceChannel, r.Resolve(context.Background(), PricingInput{Model: "claude-sonnet-4", GroupID: &g.ID}).Source)
}
func TestDownstreamRateChangeInvalidatesExistingCachedRate(t *testing.T) {
	rate := 0.8
	repo := &userGroupRateResolverRepoStub{rate: &rate}
	r := newUserGroupRateResolver(repo, nil, time.Minute, nil, "")
	require.Equal(t, 0.8, r.Resolve(context.Background(), 8, 10, 1))
	rate = 0.4
	require.Equal(t, 0.8, r.Resolve(context.Background(), 8, 10, 1))
	InvalidateUserGroupRateCache()
	require.Equal(t, 0.4, r.Resolve(context.Background(), 8, 10, 1))
}
