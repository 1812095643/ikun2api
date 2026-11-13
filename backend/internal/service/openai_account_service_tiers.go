package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/ctxkey"
	infraerrors "github.com/MACOS-DO/sub4api/internal/pkg/errors"
	"github.com/MACOS-DO/sub4api/internal/pkg/openai"
)

const OpenAIServiceTiersExtraKey = "openai_service_tiers"

type OpenAIServiceTierRates struct {
	FastMultiplier      *float64 `json:"fast_multiplier,omitempty"`
	UltrafastMultiplier *float64 `json:"ultrafast_multiplier,omitempty"`
}

type OpenAIAccountServiceTiers struct {
	FastEnabled      bool `json:"fast_enabled"`
	UltrafastEnabled bool `json:"ultrafast_enabled"`
	OpenAIServiceTierRates
	ModelMultipliers map[string]OpenAIServiceTierRates `json:"model_multipliers,omitempty"`
}

func readOpenAIAccountServiceTiers(extra map[string]any) (OpenAIAccountServiceTiers, error) {
	result := OpenAIAccountServiceTiers{FastEnabled: true, UltrafastEnabled: true}
	raw, exists := extra[OpenAIServiceTiersExtraKey]
	if !exists {
		return result, nil
	}
	data, err := json.Marshal(raw)
	if err != nil || string(data) == "null" || len(data) == 0 || data[0] != '{' {
		return result, fmt.Errorf("%s must be an object", OpenAIServiceTiersExtraKey)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return result, err
	}
	for _, key := range []string{"fast_enabled", "ultrafast_enabled"} {
		if value, exists := fields[key]; exists && string(value) != "true" && string(value) != "false" {
			return result, fmt.Errorf("%s must be a boolean", key)
		}
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return result, err
	}
	return result, nil
}

// ValidateOpenAIAccountServiceTiersExtra rejects malformed prices before persisting an account.
func ValidateOpenAIAccountServiceTiersExtra(platform string, extra map[string]any) error {
	if platform != PlatformOpenAI {
		return nil
	}
	cfg, err := readOpenAIAccountServiceTiers(extra)
	invalid := func(message string) error { return infraerrors.BadRequest("INVALID_OPENAI_SERVICE_TIERS", message) }
	if err != nil {
		return invalid(err.Error())
	}
	validateRates := func(rates OpenAIServiceTierRates) error {
		for _, value := range []*float64{rates.FastMultiplier, rates.UltrafastMultiplier} {
			if value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0) || *value <= 0 || *value > 1000 || math.Abs(*value*1e4-math.Round(*value*1e4)) > 1e-7) {
				return invalid("service tier multipliers must be greater than 0 and at most 1000, with up to 4 decimal places")
			}
		}
		return nil
	}
	if err := validateRates(cfg.OpenAIServiceTierRates); err != nil {
		return err
	}
	if len(cfg.ModelMultipliers) > 100 {
		return invalid("at most 100 model service tier overrides are allowed")
	}
	for model, rates := range cfg.ModelMultipliers {
		if strings.TrimSpace(model) != model || model == "" || len(model) > 200 || model != strings.ToLower(model) || strings.ContainsAny(model, "\r\n\t") {
			return invalid("model patterns must be nonempty lowercase model IDs without surrounding whitespace")
		}
		if err := validateRates(rates); err != nil {
			return err
		}
	}
	return nil
}

func (a *Account) SupportsOpenAIServiceTier(tier string) bool {
	if a == nil || a.Platform != PlatformOpenAI || (!a.IsOpenAIOAuthLike() && !a.IsOpenAIApiKey()) {
		return true
	}
	tier = normalizedOpenAIServiceTierValue(tier)
	if tier != OpenAIFastTierPriority && tier != OpenAIFastTierUltrafast {
		return true
	}
	cfg, err := readOpenAIAccountServiceTiers(a.Extra)
	if err != nil {
		return false
	} // malformed stored settings must not enable a disabled tier
	switch normalizedOpenAIServiceTierValue(tier) {
	case OpenAIFastTierPriority:
		return cfg.FastEnabled
	case OpenAIFastTierUltrafast:
		return cfg.UltrafastEnabled
	default:
		return true
	}
}

// Overrides are applied to the Standard unit prices, never multiplied on top of an existing Fast rate.
func openAIAccountServiceTierMultiplier(a *Account, model, tier string) *float64 {
	if a == nil || a.Platform != PlatformOpenAI || (!a.IsOpenAIOAuthLike() && !a.IsOpenAIApiKey()) {
		return nil
	}
	cfg, err := readOpenAIAccountServiceTiers(a.Extra)
	if err != nil {
		return nil
	}
	tier = normalizedOpenAIServiceTierValue(tier)
	selectRate := func(r OpenAIServiceTierRates) *float64 {
		switch tier {
		case OpenAIFastTierPriority:
			return r.FastMultiplier
		case OpenAIFastTierUltrafast:
			return r.UltrafastMultiplier
		default:
			return nil
		}
	}
	model = normalizeKnownOpenAICodexModel(strings.ToLower(strings.TrimSpace(model)))
	// Exact first, then the most specific wildcard; lexical ties are deterministic.
	patterns := make([]string, 0, len(cfg.ModelMultipliers))
	for pattern := range cfg.ModelMultipliers {
		patterns = append(patterns, pattern)
	}
	sort.Slice(patterns, func(i, j int) bool {
		if (patterns[i] == model) != (patterns[j] == model) {
			return patterns[i] == model
		}
		left := len(strings.ReplaceAll(patterns[i], "*", ""))
		right := len(strings.ReplaceAll(patterns[j], "*", ""))
		if left != right {
			return left > right
		}
		return patterns[i] < patterns[j]
	})
	for _, pattern := range patterns {
		if matchModelPattern(pattern, model) {
			if rate := selectRate(cfg.ModelMultipliers[pattern]); rate != nil {
				return rate
			}
		}
	}
	return selectRate(cfg.OpenAIServiceTierRates)
}

func openAIModelUltrafastPricingRatio(model string) float64 {
	normalized := normalizeKnownOpenAICodexModel(model)
	if openai.IsGPT61SolModelSpelling(normalized) {
		return 8
	}
	if isOpenAIGPT6AstraModel(normalized) {
		return 6
	}
	return 0
}

func pricingWithServiceTierOverride(pricing *ModelPricing, tier string, multiplier *float64) *ModelPricing {
	if pricing == nil || multiplier == nil || *multiplier <= 0 || math.IsNaN(*multiplier) || math.IsInf(*multiplier, 0) {
		return pricing
	}
	cloned := *pricing
	switch normalizedOpenAIServiceTierValue(tier) {
	case OpenAIFastTierPriority:
		cloned.FastMultiplier = multiplier
	case OpenAIFastTierUltrafast:
		cloned.UltrafastMultiplier = *multiplier
	default:
		return pricing
	}
	return &cloned
}

func validateForcedOpenAIServiceTier(tier string) error {
	switch tier {
	case "", OpenAIFastTierPriority, OpenAIFastTierUltrafast:
		return nil
	default:
		return infraerrors.BadRequest("INVALID_FORCED_SERVICE_TIER", "forced_service_tier must be empty, priority or ultrafast")
	}
}

type openAIRequestServiceTierContext struct{ requested, keyForced string }
type openAIRequestServiceTierContextKey struct{}

// WithOpenAIRequestServiceTier captures each HTTP request/WS turn, not a previous turn's effective tier.
func WithOpenAIRequestServiceTier(ctx context.Context, key *APIKey, body []byte) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	preference := openAIRequestServiceTierContext{requested: normalizedOpenAIServiceTierValue(openAIRequestPayloadView(body).Get("service_tier").String())}
	if key != nil {
		preference.keyForced = key.ForcedServiceTier
	}
	return context.WithValue(ctx, openAIRequestServiceTierContextKey{}, preference)
}

func openAIForcedServiceTier(ctx context.Context, account *Account) string {
	if ctx == nil || account == nil || account.Platform != PlatformOpenAI {
		return ""
	}
	group, _ := ctx.Value(ctxkey.Group).(*Group)
	if IsGroupContextValid(group) && groupSupportsOpenAIFast(group.Platform) {
		if group.ForceOpenAIUltrafast {
			return OpenAIFastTierUltrafast
		}
		if group.ForceOpenAIFast {
			return OpenAIFastTierPriority
		}
	}
	preference, _ := ctx.Value(openAIRequestServiceTierContextKey{}).(openAIRequestServiceTierContext)
	if validateForcedOpenAIServiceTier(preference.keyForced) == nil {
		return preference.keyForced
	}
	return ""
}

func (s *OpenAIGatewayService) resolveEffectiveOpenAIServiceTier(ctx context.Context, account *Account, model, rawTier string) (string, error) {
	tier := normalizedOpenAIServiceTierValue(rawTier)
	if forced := openAIForcedServiceTier(ctx, account); forced != "" {
		tier = forced
	} else if rawTier != "" && tier == "" {
		return rawTier, nil // ingress validation owns unknown/non-string fields; preserve existing pass-through semantics
	}
	if tier == "" {
		if s.shouldForceOpenAIFastPriorityForMissingTier(ctx, account, model) {
			tier = OpenAIFastTierPriority
		}
	} else {
		action, message := s.evaluateOpenAIFastPolicy(ctx, account, model, tier)
		switch action {
		case BetaPolicyActionFilter:
			tier = ""
		case OpenAIFastPolicyActionForcePriority:
			tier = OpenAIFastTierPriority
		case BetaPolicyActionBlock:
			if message == "" {
				message = fmt.Sprintf("openai service_tier=%s is not allowed for model %s", tier, model)
			}
			return tier, &OpenAIFastBlockedError{Message: message}
		}
	}
	if !account.SupportsOpenAIServiceTier(tier) {
		return tier, &OpenAIFastBlockedError{Message: fmt.Sprintf("the selected account does not support service_tier=%s", tier)}
	}
	return tier, nil
}

func openAIAccountServiceTierEligibilityReason(ctx context.Context, account *Account, model string) string {
	if account == nil || account.Platform != PlatformOpenAI || ctx == nil {
		return ""
	}
	preference, _ := ctx.Value(openAIRequestServiceTierContextKey{}).(openAIRequestServiceTierContext)
	tier := preference.requested
	if forced := openAIForcedServiceTier(ctx, account); forced != "" {
		tier = forced
	}
	settings := openAIFastPolicySettingsFromContext(ctx)
	matcher := tier
	if matcher == "" {
		matcher = OpenAIFastTierMissing
	}
	mappedModel := account.GetMappedModel(model)
	action, _ := evaluateOpenAIFastPolicyWithSettings(settings, openAIFastPolicyUserID(ctx), account, mappedModel, matcher)
	switch action {
	case BetaPolicyActionFilter, BetaPolicyActionBlock:
		return "" // preserve existing explicit policy rejection semantics
	case OpenAIFastPolicyActionForcePriority:
		tier = OpenAIFastTierPriority
	}
	if account.SupportsOpenAIServiceTier(tier) {
		return ""
	}
	return "service_tier_unsupported"
}

// serviceTierBillingSnapshot is separate from the group multiplier and long-context surcharge.
func serviceTierBillingSnapshot(tier string, cost *CostBreakdown) *float64 {
	if cost == nil || cost.ServiceTierMultiplier <= 0 {
		return nil
	}
	switch normalizedOpenAIServiceTierValue(tier) {
	case OpenAIFastTierPriority, OpenAIFastTierUltrafast:
		value := cost.ServiceTierMultiplier
		return &value
	default:
		return nil
	}
}

// Reservation uses the largest applicable account tier price before a credential
// is selected. It reads the existing scheduler snapshot, never a paid endpoint.
type openAIInflightTierAccountsContextKey struct{}

func (s *OpenAIGatewayService) withOpenAIInflightTierAccounts(ctx context.Context, key *APIKey) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if key == nil || (key.Group != nil && key.Group.Platform != PlatformOpenAI && key.Group.Platform != PlatformComposite) {
		return ctx
	}
	probe := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	preference, _ := ctx.Value(openAIRequestServiceTierContextKey{}).(openAIRequestServiceTierContext)
	if preference.requested == "" && openAIForcedServiceTier(ctx, probe) == "" && s.settingService == nil {
		return ctx
	}
	if s.settingService != nil && openAIFastPolicySettingsFromContext(ctx) == nil {
		if settings, err := s.settingService.GetOpenAIFastPolicySettings(ctx); err == nil {
			ctx = withOpenAIFastPolicyContext(ctx, settings)
		}
	}
	if preference.requested == "" && openAIForcedServiceTier(ctx, probe) == "" {
		forcesMissing := false
		if settings := openAIFastPolicySettingsFromContext(ctx); settings != nil {
			for _, rule := range settings.Rules {
				if rule.ServiceTier == OpenAIFastTierMissing && (rule.Action == OpenAIFastPolicyActionForcePriority || rule.FallbackAction == OpenAIFastPolicyActionForcePriority) {
					forcesMissing = true
					break
				}
			}
		}
		if !forcesMissing {
			return ctx
		}
	}
	if s.schedulerSnapshot != nil {
		if accounts, _, err := s.schedulerSnapshot.ListSchedulableAccounts(ctx, key.GroupID, PlatformOpenAI, false); err == nil {
			return context.WithValue(ctx, openAIInflightTierAccountsContextKey{}, accounts)
		}
	}
	return ctx
}

func (s *OpenAIGatewayService) estimateOpenAIServiceTierTokenCost(ctx context.Context, key *APIKey, model string, pricing *ModelPricing, tokens UsageTokens, rate float64) float64 {
	baseline := (float64(tokens.InputTokens)*pricing.InputPricePerToken + float64(tokens.OutputTokens)*pricing.OutputPricePerToken) * rate
	if s.billingService == nil || (key != nil && key.Group != nil && key.Group.Platform != PlatformOpenAI && key.Group.Platform != PlatformComposite) {
		return baseline
	}
	accounts, known := ctx.Value(openAIInflightTierAccountsContextKey{}).([]Account)
	if !known {
		accounts = []Account{{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}}
	}
	preference, _ := ctx.Value(openAIRequestServiceTierContextKey{}).(openAIRequestServiceTierContext)
	highest := 0.0
	for i := range accounts {
		account := &accounts[i]
		tier, err := s.resolveEffectiveOpenAIServiceTier(ctx, account, account.GetMappedModel(model), preference.requested)
		if err != nil {
			continue
		}
		if groupBillsOpenAIFastAtStandard(key, account, tier) {
			tier = ""
		}
		if tier == "" || tier == "default" || tier == "auto" || tier == "scale" {
			highest = math.Max(highest, baseline)
			continue
		}
		effective := s.billingService.applyModelSpecificPricingPolicyEx(model, pricing, false, time.Time{})
		effective = pricingWithServiceTierOverride(effective, tier, openAIAccountServiceTierMultiplier(account, model, tier))
		cost := s.billingService.computeTokenBreakdown(effective, tokens, rate, tier, false)
		highest = math.Max(highest, cost.ActualCost)
	}
	if highest == 0 {
		return baseline
	}
	return highest
}
