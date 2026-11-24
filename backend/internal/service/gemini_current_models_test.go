package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MACOS-DO/sub4api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCurrentGeminiCatalogPricingAndReasoningAliases(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "resources", "model-pricing", "model_prices_and_context_window.json"))
	require.NoError(t, err)
	prices := &PricingService{}
	prices.pricingData, err = prices.parsePricingData(data)
	require.NoError(t, err)
	for _, tc := range []struct {
		name                 string
		input, output, cache float64
	}{
		{"gemini-4-neon", 3e-6, 15e-6, .15e-6},
		{"gemini-4-hydrogen", 1.5e-6, 7.5e-6, .075e-6},
		{"gemini-3.5-flash-lite", .3e-6, 2.5e-6, .03e-6},
		{"gemini-3.7-flash", .75e-6, 3.75e-6, .075e-6},
		{"gemini-3.8-flash", .75e-6, 3.75e-6, .075e-6},
	} {
		p := prices.GetIdentifiedModelPricing(tc.name)
		require.NotNil(t, p, tc.name)
		require.InDelta(t, tc.input, p.InputCostPerToken, 1e-15)
		require.InDelta(t, tc.output, p.OutputCostPerToken, 1e-15)
		require.InDelta(t, tc.cache, p.CacheReadInputTokenCost, 1e-15)
	}
	for _, model := range []string{"gemini-4-neon", "gemini-4-hydrogen"} {
		base := prices.GetIdentifiedModelPricing(model)
		for _, tier := range []string{"low", "medium", "high", "xhigh"} {
			require.Same(t, base, prices.GetIdentifiedModelPricing("models/"+model+"-"+tier))
		}
		require.Zero(t, base.CacheStorageCostPerTokenPerHour)
	}
	// 单独发布或管理员覆盖的档位价格优先，不能强行压成基础价。
	special := &LiteLLMModelPricing{InputCostPerToken: 9e-6}
	prices.pricingData["gemini-4-neon-xhigh"] = special
	require.Same(t, special, prices.GetIdentifiedModelPricing("gemini-4-neon-xhigh"))
	require.Equal(t, "gemini-4-neon-cyber", normalizeGeminiThinkingTierAlias("gemini-4-neon-cyber"))
}

func TestCurrentGeminiFallbackTokenAndImageBilling(t *testing.T) {
	billing := NewBillingService(&config.Config{}, nil)
	for _, tc := range []struct {
		model string
		total float64
	}{
		{"gemini-4-neon", 18.15}, {"gemini-4-hydrogen", 9.075},
	} {
		for _, suffix := range []string{"", "-low", "-medium", "-high", "-xhigh"} {
			cost, err := billing.CalculateCost("models/"+tc.model+suffix, UsageTokens{InputTokens: 1000000, OutputTokens: 1000000, CacheReadTokens: 1000000}, 1)
			require.NoError(t, err)
			require.InDelta(t, tc.total, cost.TotalCost, 1e-10)
			cost, err = billing.CalculateCostWithServiceTier(tc.model+suffix, UsageTokens{InputTokens: 1000000, OutputTokens: 1000000, CacheReadTokens: 1000000}, 1, "priority")
			require.NoError(t, err)
			require.InDelta(t, tc.total*1.8, cost.TotalCost, 1e-10)
		}
	}
	cost, err := billing.CalculateCost("gemini-nano-banana-2.1", UsageTokens{InputTokens: 1000, OutputTokens: 1200, ImageOutputTokens: 1120}, 1)
	require.NoError(t, err)
	require.InDelta(t, .0336, cost.ImageOutputCost, 1e-12)
	require.InDelta(t, .0357, cost.TotalCost, 1e-12)
}

func TestNanoBanana21HasImageMetadataAndNoInventedAntigravityRoute(t *testing.T) {
	require.True(t, isImageGenerationModel("gemini-nano-banana-2.1"))
	require.True(t, isImageGenerationModel("models/gemini-nano-banana-2.1"))
	require.False(t, isImageGenerationModel("gemini-nano-banana-2.1-preview"))
	ag := &Account{Platform: PlatformAntigravity}
	require.False(t, ag.IsModelSupported("gemini-nano-banana-2.1"))
}
