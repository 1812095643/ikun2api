package handler

import (
	"math"
	"regexp"
	"strings"

	"github.com/MACOS-DO/sub4api/internal/service"
)

var modelPlazaOriginalFamily = regexp.MustCompile(`^(?:(?:anthropic[.:])?claude(?:[-._]|\d|$)|(?:openai[.:])?(?:gpt(?:[-._]|$)|chatgpt-|o[134](?:-|$)|codex(?:-|$)))`)

// Project only the plaza DTO. Billing and cached channel configuration stay intact.
// Both price columns use known Claude/GPT originals so the response does not expose
// channel overrides, including cache prices and custom context tiers.
func modelPlazaDisplayModel(model service.PlazaModel, longContext bool) service.PlazaModel {
	if model.Pricing != nil && model.Pricing.BillingMode != "" && model.Pricing.BillingMode != service.BillingModeToken {
		return model
	}
	name := strings.ToLower(strings.TrimSpace(model.Name))
	if at := strings.LastIndex(name, "/"); at >= 0 {
		name = name[at+1:]
	}
	original := model.OfficialPricing
	if !modelPlazaOriginalFamily.MatchString(name) || original == nil ||
		(validPlazaPrice(original.InputPrice) == nil && validPlazaPrice(original.OutputPrice) == nil) {
		return model
	}
	display := &service.ChannelModelPricing{
		BillingMode: service.BillingModeToken,
		InputPrice:  validPlazaPrice(original.InputPrice), OutputPrice: validPlazaPrice(original.OutputPrice),
		CacheReadPrice: validPlazaPrice(original.CacheReadPrice), CacheWritePrice: validPlazaPrice(original.CacheWritePrice),
		CacheWrite1hPrice: validPlazaPrice(original.CacheWrite1hPrice),
	}
	if model.Pricing != nil {
		display.ReasoningEffortMultipliers = model.Pricing.ReasoningEffortMultipliers
	}
	model.LongContextBasis = ""
	if longContext && len(original.Intervals) > 1 {
		display.Intervals = append([]service.PricingInterval(nil), original.Intervals...)
		model.LongContextBasis = service.ContextPricingBasisWholeRequest
	}
	model.Pricing = display
	return model
}

func validPlazaPrice(value *float64) *float64 {
	if value == nil || *value < 0 || math.IsNaN(*value) || math.IsInf(*value, 0) {
		return nil
	}
	return value
}
