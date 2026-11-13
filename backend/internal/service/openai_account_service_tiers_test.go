package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/config"
	"github.com/MACOS-DO/sub4api/internal/pkg/ctxkey"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func tierTestAccount(kind string, settings any) *Account {
	a := &Account{ID: 7, Platform: PlatformOpenAI, Type: kind, Status: StatusActive, Schedulable: true, Concurrency: 1,
		Credentials: map[string]any{"api_key": "fixture-key"}, Extra: map[string]any{"openai_responses_supported": true}}
	if settings != nil {
		a.Extra[OpenAIServiceTiersExtraKey] = settings
	}
	return a
}

func TestOpenAIAccountServiceTiers_DefaultsAndValidation(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeSetupToken, AccountTypeAPIKey} {
		a := tierTestAccount(kind, nil)
		require.True(t, a.SupportsOpenAIServiceTier("priority"))
		require.True(t, a.SupportsOpenAIServiceTier("ultrafast"))
		a.Extra[OpenAIServiceTiersExtraKey] = map[string]any{"fast_enabled": false, "ultrafast_enabled": true}
		require.False(t, a.SupportsOpenAIServiceTier("fast"))
		require.True(t, a.SupportsOpenAIServiceTier("ultrafast"))
		require.True(t, a.SupportsOpenAIServiceTier("default"))
	}
	for _, raw := range []any{nil, true, []any{}, map[string]any{"fast_enabled": "false"}, map[string]any{"ultrafast_enabled": nil}, map[string]any{"fast_multiplier": -1}, map[string]any{"fast_multiplier": 0}, map[string]any{"fast_multiplier": 1001}, map[string]any{"fast_multiplier": 1.12345}, map[string]any{"fast_multiplier": math.Inf(1)}, map[string]any{"model_multipliers": map[string]any{"GPT-*": map[string]any{"ultrafast_multiplier": 6}}}} {
		require.Error(t, ValidateOpenAIAccountServiceTiersExtra(PlatformOpenAI, map[string]any{OpenAIServiceTiersExtraKey: raw}), "raw=%v", raw)
	}
	require.NoError(t, ValidateOpenAIAccountServiceTiersExtra(PlatformOpenAI, nil))
	require.NoError(t, ValidateOpenAIAccountServiceTiersExtra(PlatformOpenAI, map[string]any{OpenAIServiceTiersExtraKey: map[string]any{"fast_multiplier": 2.1234}}))
	bad := tierTestAccount(AccountTypeAPIKey, true)
	require.False(t, bad.SupportsOpenAIServiceTier("ultrafast"))
	require.True(t, bad.SupportsOpenAIServiceTier("default"))
}

func TestOpenAIServiceTierPrecedenceHTTPAndWebSocket(t *testing.T) {
	svc := newOpenAIGatewayServiceWithSettings(t, DefaultOpenAIFastPolicySettings())
	for _, tt := range []struct {
		name, request, key, want string
		groupFast, groupUltra    bool
	}{
		{name: "existing request", request: "fast", want: "priority"},
		{name: "existing omitted"},
		{name: "key fast", key: "priority", request: "ultrafast", want: "priority"},
		{name: "key ultra omitted", key: "ultrafast", want: "ultrafast"},
		{name: "key ultra explicit default", key: "ultrafast", request: "default", want: "ultrafast"},
		{name: "admin fast wins", key: "ultrafast", groupFast: true, want: "priority"},
		{name: "admin ultra wins", key: "priority", groupUltra: true, want: "ultrafast"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := tierTestAccount(AccountTypeAPIKey, nil)
			group := &Group{ID: 1, Platform: PlatformOpenAI, Hydrated: true, Status: StatusActive, ForceOpenAIFast: tt.groupFast, ForceOpenAIUltrafast: tt.groupUltra}
			body := []byte(`{"model":"gpt-6.1-sol","input":"hi"}`)
			if tt.request != "" {
				body = []byte(`{"model":"gpt-6.1-sol","input":"hi","service_tier":"` + tt.request + `"}`)
			}
			ctx := context.WithValue(context.Background(), ctxkey.Group, group)
			ctx = WithOpenAIRequestServiceTier(ctx, &APIKey{ForcedServiceTier: tt.key}, body)
			out, err := svc.applyOpenAIFastPolicyToBody(ctx, a, "gpt-6.1-sol", body)
			require.NoError(t, err)
			require.Equal(t, tt.want, gjson.GetBytes(out, "service_tier").String())
			frame := append([]byte(`{"type":"response.create",`), body[1:]...)
			out, blocked, err := svc.applyOpenAIFastPolicyToWSResponseCreate(ctx, a, "gpt-6.1-sol", frame)
			require.NoError(t, err)
			require.Nil(t, blocked)
			require.Equal(t, tt.want, gjson.GetBytes(out, "service_tier").String())
		})
	}
	a := tierTestAccount(AccountTypeOAuth, map[string]any{"ultrafast_enabled": false})
	body := []byte(`{"model":"gpt-6.1-sol","service_tier":"ultrafast"}`)
	_, err := svc.applyOpenAIFastPolicyToBody(context.Background(), a, "gpt-6.1-sol", body)
	require.IsType(t, &OpenAIFastBlockedError{}, err)
	frame := []byte(`{"type":"response.create","model":"gpt-6.1-sol","service_tier":"ultrafast"}`)
	_, blocked, err := svc.applyOpenAIFastPolicyToWSResponseCreate(context.Background(), a, "gpt-6.1-sol", frame)
	require.NoError(t, err)
	require.NotNil(t, blocked)
	unrelated := []byte(`{"type":"response.cancel","service_tier":"ultrafast"}`)
	out, blocked, err := svc.applyOpenAIFastPolicyToWSResponseCreate(context.Background(), a, "gpt-6.1-sol", unrelated)
	require.NoError(t, err)
	require.Nil(t, blocked)
	require.Equal(t, unrelated, out)
}

func TestOpenAIServiceTierAccountEligibilityMatchesPolicy(t *testing.T) {
	a := tierTestAccount(AccountTypeAPIKey, map[string]any{"fast_enabled": false, "ultrafast_enabled": false})
	body := []byte(`{"model":"gpt-6.1-sol","service_tier":"ultrafast"}`)
	ctx := WithOpenAIRequestServiceTier(context.Background(), &APIKey{}, body)
	require.Equal(t, "service_tier_unsupported", openAIAccountServiceTierEligibilityReason(ctx, a, "gpt-6.1-sol"))
	require.Equal(t, "service_tier_unsupported", openAICompatibleAccountEligibilityFailureReasonBeforeProfit(ctx, a, PlatformOpenAI, "gpt-6.1-sol", false, ""))
	scheduler := &defaultOpenAIAccountScheduler{service: &OpenAIGatewayService{}}
	ok, reason := scheduler.isAccountRequestCompatibleReason(ctx, a, OpenAIAccountScheduleRequest{RequestedModel: "gpt-6.1-sol"})
	require.False(t, ok)
	require.Equal(t, "service_tier_unsupported", reason)
	settings := &OpenAIFastPolicySettings{Rules: []OpenAIFastPolicyRule{{ServiceTier: OpenAIFastTierUltrafast, Action: BetaPolicyActionFilter, Scope: BetaPolicyScopeAll}}}
	ctx = withOpenAIFastPolicyContext(ctx, settings)
	require.Empty(t, openAIAccountServiceTierEligibilityReason(ctx, a, "gpt-6.1-sol"))
	svc := &OpenAIGatewayService{}
	out, err := svc.applyOpenAIFastPolicyToBody(ctx, a, "gpt-6.1-sol", body)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(out, "service_tier").Exists())
	// Key forcing on another provider has no effect.
	a.Platform = PlatformGrok
	ctx = WithOpenAIRequestServiceTier(context.Background(), &APIKey{ForcedServiceTier: "ultrafast"}, []byte(`{"model":"grok-4"}`))
	require.Empty(t, openAIAccountServiceTierEligibilityReason(ctx, a, "grok-4"))
	require.Empty(t, openAIForcedServiceTier(ctx, a))
}

func TestOpenAIAccountTierPricingAllTokenBucketsAndOverrides(t *testing.T) {
	billing := NewBillingService(&config.Config{}, nil)
	a := tierTestAccount(AccountTypeAPIKey, map[string]any{
		"fast_multiplier": 3.0, "ultrafast_multiplier": 9.0,
		"model_multipliers": map[string]any{
			"gpt-*":       map[string]any{"ultrafast_multiplier": 7.0},
			"gpt-6-astra": map[string]any{"ultrafast_multiplier": 6.0},
			"gpt-6.1-sol": map[string]any{"ultrafast_multiplier": 8.0},
		},
	})
	tokens := UsageTokens{InputTokens: 1000, OutputTokens: 300, CacheReadTokens: 700, CacheCreationTokens: 600, CacheCreation30mTokens: 200, CacheCreation12hTokens: 400}
	for _, tt := range []struct {
		model, tier string
		want        float64
	}{
		{"gpt-6-astra", "ultrafast", 6}, {"gpt-6.1-sol", "ultrafast", 8}, {"gpt-6-astra", "priority", 3}, {"gpt-6.1-sol", "priority", 3},
	} {
		t.Run(tt.model+tt.tier, func(t *testing.T) {
			standard, err := billing.CalculateCostUnified(CostInput{Model: tt.model, Tokens: tokens, RateMultiplier: .7})
			require.NoError(t, err)
			fast, err := billing.CalculateCostUnified(CostInput{Model: tt.model, Tokens: tokens, RateMultiplier: .7, ServiceTier: tt.tier, ServiceTierMultiplier: openAIAccountServiceTierMultiplier(a, tt.model, tt.tier)})
			require.NoError(t, err)
			require.InDelta(t, standard.InputCost*tt.want, fast.InputCost, 1e-12)
			require.InDelta(t, standard.OutputCost*tt.want, fast.OutputCost, 1e-12)
			require.InDelta(t, standard.CacheReadCost*tt.want, fast.CacheReadCost, 1e-12)
			require.InDelta(t, standard.CacheCreationCost*tt.want, fast.CacheCreationCost, 1e-12)
			require.InDelta(t, standard.ActualCost*tt.want, fast.ActualCost, 1e-12)
			require.Equal(t, tt.want, fast.ServiceTierMultiplier)
		})
	}
	for _, tt := range []struct {
		model string
		want  float64
	}{{"gpt-6-astra", 6}, {"gpt-6.1-sol", 8}} {
		cost, err := billing.CalculateCostUnified(CostInput{Model: tt.model, Tokens: tokens, RateMultiplier: 1, ServiceTier: "ultrafast"})
		require.NoError(t, err)
		require.Equal(t, tt.want, cost.ServiceTierMultiplier)
	}
	// More-specific wildcard wins independently of map iteration order; absent Fast inherits its account default.
	for i := 0; i < 20; i++ {
		require.Equal(t, 7.0, *openAIAccountServiceTierMultiplier(a, "gpt-5.6-sol", "ultrafast"))
	}
}

func TestOpenAIForcedServiceTierCacheRoundtripAndValidation(t *testing.T) {
	for _, tier := range []string{"", "priority", "ultrafast"} {
		require.NoError(t, validateCreateAPIKeyRequest(CreateAPIKeyRequest{ForcedServiceTier: tier}))
		require.NoError(t, validateUpdateAPIKeyRequest(UpdateAPIKeyRequest{ForcedServiceTier: &tier}))
	}
	for _, tier := range []string{"fast", "default", " ULTRAFAST ", "unknown"} {
		require.Error(t, validateCreateAPIKeyRequest(CreateAPIKeyRequest{ForcedServiceTier: tier}))
		require.Error(t, validateUpdateAPIKeyRequest(UpdateAPIKeyRequest{ForcedServiceTier: &tier}))
	}
	key := &APIKey{ID: 2, Key: "fixture-local-key", UserID: 1, ForcedServiceTier: "priority", User: &User{ID: 1, Status: StatusActive}, Group: &Group{ID: 3, Platform: PlatformOpenAI, Hydrated: true, ForceOpenAIUltrafast: true}}
	svc := &APIKeyService{}
	raw, err := json.Marshal(&APIKeyAuthCacheEntry{Snapshot: svc.snapshotFromAPIKey(context.Background(), key)})
	require.NoError(t, err)
	var entry APIKeyAuthCacheEntry
	require.NoError(t, json.Unmarshal(raw, &entry))
	recovered, used, err := svc.applyAuthCacheEntry(key.Key, &entry)
	require.NoError(t, err)
	require.True(t, used)
	require.Equal(t, "priority", recovered.ForcedServiceTier)
	require.True(t, recovered.Group.ForceOpenAIUltrafast)
	require.False(t, APIKeyUpdateFields{ForcedServiceTier: true}.IsEmpty())
}

func TestOpenAIForcedUltrafastForwardAndUsageSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{false, true} {
		t.Run(fmtBool(stream), func(t *testing.T) {
			response := `{"id":"resp_tier_fixture","model":"gpt-6.1-sol","status":"completed","output":[],"usage":{"input_tokens":1200,"output_tokens":300}}`
			responseBody := response
			contentType := "application/json"
			if stream {
				responseBody = "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":" + response + "}\n\n"
				contentType = "text/event-stream"
			}
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(responseBody))}}
			usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
			svc := newOpenAIRecordUsageServiceForTest(usageRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
			svc.httpUpstream = upstream
			svc.cfg.Security.URLAllowlist.Enabled = false
			svc.settingService = NewSettingService(&openAIFastPolicyRepoStub{values: map[string]string{}}, &config.Config{})
			body := []byte(`{"model":"gpt-6.1-sol","input":"hi","stream":` + fmtBool(stream) + `}`)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
			key := &APIKey{ID: 2, User: &User{ID: 1}, ForcedServiceTier: "ultrafast"}
			ctx := WithOpenAIRequestServiceTier(context.Background(), key, body)
			a := tierTestAccount(AccountTypeAPIKey, map[string]any{"ultrafast_multiplier": 8.0})
			result, err := svc.Forward(ctx, c, a, body)
			require.NoError(t, err)
			require.Equal(t, "ultrafast", gjson.GetBytes(upstream.lastBody, "service_tier").String())
			require.Equal(t, "ultrafast", *result.ServiceTier)
			result.RequestID = "tier-fixture-" + fmtBool(stream)
			result.Duration = time.Second
			require.NoError(t, svc.RecordUsage(ctx, &OpenAIRecordUsageInput{APIKey: key, User: key.User, Account: a, Result: result}))
			require.NotNil(t, usageRepo.lastLog)
			require.NotNil(t, usageRepo.lastLog.ServiceTierMultiplier)
			require.Equal(t, 8.0, *usageRepo.lastLog.ServiceTierMultiplier)
			a.Extra[OpenAIServiceTiersExtraKey] = map[string]any{"ultrafast_multiplier": 11.0}
			require.Equal(t, 8.0, *usageRepo.lastLog.ServiceTierMultiplier, "history must not depend on the current account settings")
		})
	}
}

func fmtBool(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func TestOpenAIServiceTierSchedulerSkipsUnsupportedStickyAccount(t *testing.T) {
	for _, advanced := range []bool{false, true} {
		t.Run(fmtBool(advanced), func(t *testing.T) {
			resetOpenAIAdvancedSchedulerSettingCacheForTest()
			defer resetOpenAIAdvancedSchedulerSettingCacheForTest()
			groupID := int64(71)
			group := &Group{ID: groupID, Platform: PlatformOpenAI, Hydrated: true, Status: StatusActive}
			blocked := tierTestAccount(AccountTypeAPIKey, map[string]any{"ultrafast_enabled": false})
			blocked.ID = 70
			blocked.Priority = 0
			allowed := tierTestAccount(AccountTypeAPIKey, map[string]any{"ultrafast_enabled": true})
			allowed.ID = 71
			allowed.Priority = 100
			blocked.GroupIDs = []int64{groupID}
			allowed.GroupIDs = []int64{groupID}
			cfg := &config.Config{}
			cfg.Gateway.Scheduling.LoadBatchEnabled = true
			cfg.Gateway.OpenAIWS.LBTopK = 1
			cfg.Gateway.OpenAIWS.SchedulerScoreWeights.Priority = 1
			settings := &openAIAdvancedSchedulerSettingRepoStub{values: map[string]string{openAIAdvancedSchedulerSettingKey: fmtBool(advanced)}}
			svc := &OpenAIGatewayService{cfg: cfg, accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{*blocked, *allowed}}, cache: &schedulerTestGatewayCache{sessionBindings: map[string]int64{"fixture-sticky": blocked.ID}}, concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}), rateLimitService: &RateLimitService{settingService: NewSettingService(settings, cfg)}}
			ctx := context.WithValue(context.Background(), ctxkey.Group, group)
			ctx = WithOpenAIRequestServiceTier(ctx, &APIKey{ForcedServiceTier: "ultrafast"}, nil)
			selection, _, err := svc.SelectAccountWithScheduler(ctx, &groupID, "", "fixture-sticky", "gpt-6.1-sol", nil, OpenAIUpstreamTransportAny, false)
			require.NoError(t, err)
			require.NotNil(t, selection)
			require.Equal(t, allowed.ID, selection.Account.ID)
			if selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
		})
	}
}

func TestOpenAIAccountTierReservationUsesHighestApplicablePrice(t *testing.T) {
	cfg := &config.Config{}
	svc := &OpenAIGatewayService{cfg: cfg, billingService: NewBillingService(cfg, nil)}
	group := &Group{ID: 71, Platform: PlatformOpenAI, Hydrated: true, Status: StatusActive, RateMultiplier: 1}
	key := &APIKey{ID: 2, UserID: 1, User: &User{ID: 1}, GroupID: &group.ID, Group: group, ForcedServiceTier: "ultrafast"}
	accounts := []Account{*tierTestAccount(AccountTypeAPIKey, map[string]any{"ultrafast_multiplier": 6.0}), *tierTestAccount(AccountTypeAPIKey, map[string]any{"ultrafast_multiplier": 9.0})}
	ctx := context.WithValue(context.Background(), ctxkey.Group, group)
	ctx = WithOpenAIRequestServiceTier(ctx, key, nil)
	ctx = context.WithValue(ctx, openAIInflightTierAccountsContextKey{}, accounts)
	pricing, err := svc.billingService.GetModelPricing("gpt-6.1-sol")
	require.NoError(t, err)
	tokens := UsageTokens{InputTokens: 1000, OutputTokens: 300}
	baseline := svc.billingService.computeTokenBreakdown(pricing, tokens, 1, "", false)
	cost := svc.estimateOpenAIServiceTierTokenCost(ctx, key, "gpt-6.1-sol", pricing, tokens, 1)
	require.InDelta(t, baseline.ActualCost*9, cost, 1e-12)
	group.FreeOpenAIFast = true
	key.ForcedServiceTier = "priority"
	ctx = WithOpenAIRequestServiceTier(ctx, key, nil)
	cost = svc.estimateOpenAIServiceTierTokenCost(ctx, key, "gpt-6.1-sol", pricing, tokens, 1)
	require.InDelta(t, baseline.ActualCost, cost, 1e-12)
}

func TestOpenAIAccountTierDowngradeCannotIncreaseCustomCharge(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	svc := newOpenAIRecordUsageServiceForTest(usageRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	account := tierTestAccount(AccountTypeAPIKey, map[string]any{"fast_multiplier": 10.0, "ultrafast_multiplier": 6.0})
	tier := "ultrafast"
	result := &OpenAIForwardResult{RequestID: "fixture-tier-downgrade", Model: "gpt-6.1-sol", ServiceTier: &tier, UpstreamResponseServiceTier: "priority", Usage: OpenAIUsage{InputTokens: 1000, OutputTokens: 300}}
	key := &APIKey{ID: 2, User: &User{ID: 1}}
	require.NoError(t, svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{APIKey: key, User: key.User, Account: account, Result: result}))
	require.NotNil(t, usageRepo.lastLog.ServiceTierMultiplier)
	require.Equal(t, 6.0, *usageRepo.lastLog.ServiceTierMultiplier)
}
