//go:build unit

package handler

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestModelPlazaOriginalDisplayDoesNotExposeChannelOverrides(t *testing.T) {
	for _, name := range []string{"claude-opus-4-6", "GPT-5.6-SOL", "openai/gpt-6-sol", "anthropic.claude-sonnet-4-5", "o3", "codex-mini"} {
		t.Run(name, func(t *testing.T) {
			channel := &service.ChannelModelPricing{
				BillingMode: service.BillingModeToken,
				InputPrice:  testPtr(91e-6), OutputPrice: testPtr(92e-6),
				CacheReadPrice: testPtr(93e-6), CacheWritePrice: testPtr(94e-6), CacheWrite1hPrice: testPtr(95e-6),
				Intervals: []service.PricingInterval{{TierLabel: "PRIVATE_CHANNEL_TIER", InputPrice: testPtr(99e-6)}},
			}
			original := &service.PlazaOfficialPricing{
				InputPrice: testPtr(4e-6), OutputPrice: testPtr(20e-6),
				CacheReadPrice: testPtr(0.4e-6), CacheWritePrice: testPtr(5e-6), CacheWrite1hPrice: testPtr(8e-6),
			}
			g := &service.PlazaGroup{ID: 1, RateMultiplier: 0.65, Models: []service.PlazaModel{{Name: name, Platform: "anthropic", Pricing: channel, OfficialPricing: original}}}
			dto := toModelPlazaGroupDTO(g, map[int64]float64{1: 0.5})
			raw, err := json.Marshal(dto)
			require.NoError(t, err)
			var data map[string]any
			require.NoError(t, json.Unmarshal(raw, &data))
			model := data["models"].([]any)[0].(map[string]any)
			prices := model["pricing"].(map[string]any)
			for field, expected := range map[string]float64{"input_price": 4e-6, "output_price": 20e-6, "cache_read_price": 0.4e-6, "cache_write_price": 5e-6, "cache_write_1h_price": 8e-6} {
				require.InDelta(t, expected, prices[field], 1e-12, field)
			}
			require.NotContains(t, string(raw), "PRIVATE_CHANNEL_TIER")
			require.Equal(t, 0.5, *dto.UserRateMultiplier)
			require.Equal(t, 91e-6, *g.Models[0].Pricing.InputPrice, "display projection must not alter billing configuration")
			require.Equal(t, "PRIVATE_CHANNEL_TIER", channel.Intervals[0].TierLabel)
		})
	}
}

func TestModelPlazaOriginalDisplayMissingCachesStayMissing(t *testing.T) {
	m := service.PlazaModel{Name: "gpt-6-sol", Pricing: &service.ChannelModelPricing{CacheWritePrice: testPtr(99e-6), CacheWrite1hPrice: testPtr(98e-6)}, OfficialPricing: &service.PlazaOfficialPricing{InputPrice: testPtr(2e-6)}}
	display := modelPlazaDisplayModel(m, true)
	require.Nil(t, display.Pricing.CacheWritePrice)
	require.Nil(t, display.Pricing.CacheWrite1hPrice)
	require.Nil(t, display.Pricing.CacheReadPrice)
	require.Equal(t, 99e-6, *m.Pricing.CacheWritePrice)
}

func TestModelPlazaOriginalDisplayUsesOnlyOfficialContextTiers(t *testing.T) {
	originalTiers := []service.PricingInterval{{MinTokens: 0, MaxTokens: testPtrInt(200000), InputPrice: testPtr(2e-6)}, {MinTokens: 200000, InputPrice: testPtr(4e-6)}}
	m := service.PlazaModel{Name: "gpt-6-sol", Pricing: &service.ChannelModelPricing{Intervals: []service.PricingInterval{{TierLabel: "private"}}}, OfficialPricing: &service.PlazaOfficialPricing{InputPrice: testPtr(2e-6), Intervals: originalTiers}}
	withTiers := modelPlazaDisplayModel(m, true)
	require.Equal(t, originalTiers, withTiers.Pricing.Intervals)
	require.Equal(t, service.ContextPricingBasisWholeRequest, withTiers.LongContextBasis)
	withoutTiers := modelPlazaDisplayModel(m, false)
	require.Empty(t, withoutTiers.Pricing.Intervals)
	require.Len(t, withoutTiers.OfficialPricing.Intervals, 2)
	require.Equal(t, "private", m.Pricing.Intervals[0].TierLabel)
}

func TestModelPlazaDisplayKeepsOtherFamiliesUnknownOriginalsAndMedia(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		original   bool
	}{{"deepseek-chat", "token", true}, {"gemini-2.5-pro", "token", true}, {"gpt-unlisted", "token", false}, {"gpt-image-2", "image", true}} {
		t.Run(tc.name, func(t *testing.T) {
			m := service.PlazaModel{Name: tc.name, Pricing: &service.ChannelModelPricing{BillingMode: service.BillingMode(tc.mode), InputPrice: testPtr(9e-6)}}
			if tc.original {
				m.OfficialPricing = &service.PlazaOfficialPricing{InputPrice: testPtr(2e-6)}
			}
			require.Same(t, m.Pricing, modelPlazaDisplayModel(m, true).Pricing)
		})
	}
	require.Nil(t, validPlazaPrice(testPtr(math.NaN())))
	require.Nil(t, validPlazaPrice(testPtr(math.Inf(1))))
	require.Nil(t, validPlazaPrice(testPtr(-1)))
	require.Equal(t, 0.0, *validPlazaPrice(testPtr(0)))
}

func testPtrInt(value int) *int { return &value }
