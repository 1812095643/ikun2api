package service

import (
	"context"
	"errors"
	"github.com/MACOS-DO/sub4api/internal/config"
	"github.com/stretchr/testify/require"
	"math"
	"testing"
	"time"
)

func universalFloat(v float64) *float64 { return &v }
func TestUniversalSubscriptionSnapshotValidation(t *testing.T) {
	base := UniversalPlanSnapshot{Name: "月卡", ValidityDays: 30, TotalCreditUSD: universalFloat(100), DailyLimitUSD: universalFloat(10)}
	require.NoError(t, base.Validate())
	for _, v := range []float64{0, -1, math.NaN(), math.Inf(1), 1e12} {
		p := base
		p.TotalCreditUSD = universalFloat(v)
		require.Error(t, p.Validate())
	}
	p := base
	p.TotalCreditUSD = nil
	require.Error(t, p.Validate())
	p = base
	p.ValidityDays = 0
	require.Error(t, p.Validate())
	saved, err := DecodeUniversalSnapshot(base.Map())
	require.NoError(t, err)
	require.Equal(t, base, saved)
}
func TestUniversalSubscriptionLimitsCountAllPendingRequests(t *testing.T) {
	x := &UniversalSubscription{TotalCreditUSD: universalFloat(100), DailyLimitUSD: universalFloat(10), WeeklyLimitUSD: universalFloat(50), MonthlyLimitUSD: universalFloat(90), TotalUsedUSD: 40, DailyUsedUSD: 4, WeeklyUsedUSD: 20, MonthlyUsedUSD: 40}
	require.NoError(t, checkUniversalLimits(x, 2, 4, 4, 4, 4))
	require.Error(t, checkUniversalLimits(x, 2.01, 4, 4, 4, 4), "pending Key usage must consume the same daily limit")
	require.ErrorIs(t, checkUniversalLimits(x, 0, 60, 0, 0, 0), ErrUniversalExhausted)
	require.Error(t, checkUniversalLimits(x, 1, 0, 0, 30, 0))
	require.Error(t, checkUniversalLimits(x, 1, 0, 0, 0, 50))
}
func TestUniversalSubscriptionCalendarDayAndRollingWindows(t *testing.T) {
	start := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	x := &UniversalSubscription{DailyWindowStart: start, WeeklyWindowStart: start, MonthlyWindowStart: start, DailyUsedUSD: 3, WeeklyUsedUSD: 5, MonthlyUsedUSD: 8, TotalUsedUSD: 8}
	normalizeUniversalWindows(x, start.Add(31*24*time.Hour))
	require.Zero(t, x.DailyUsedUSD)
	require.Zero(t, x.WeeklyUsedUSD)
	require.Zero(t, x.MonthlyUsedUSD)
	require.Equal(t, float64(8), x.TotalUsedUSD)
	require.Equal(t, start.Add(28*24*time.Hour), x.WeeklyWindowStart)
	require.Equal(t, start.Add(30*24*time.Hour), x.MonthlyWindowStart)
}
func TestAccountModelAllowlistPreservesClaudeAliasesAndMappings(t *testing.T) {
	extra, err := NormalizeAccountModelAllowlistExtra(map[string]any{AccountModelAllowlistExtraKey: map[string]any{"enabled": true, "models": []string{"claude-sonnet-4-5", "claude-haiku-*"}}})
	require.NoError(t, err)
	a := &Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth, Extra: extra}
	require.True(t, a.AllowsConfiguredModel("claude-sonnet-4-5-20250929"))
	require.True(t, a.AllowsConfiguredModel("claude-haiku-4-5"))
	require.False(t, a.AllowsConfiguredModel("claude-opus-4-5"))
	require.False(t, a.IsModelSupported("claude-opus-4-5"))
	require.True(t, a.IsModelSupported("claude-haiku-4-5"))
	_, err = NormalizeAccountModelAllowlistExtra(map[string]any{AccountModelAllowlistExtraKey: map[string]any{"enabled": true, "models": []string{}}})
	require.Error(t, err)
	a.Extra = map[string]any{AccountModelAllowlistExtraKey: "broken"}
	require.False(t, a.AllowsConfiguredModel("claude-sonnet-4-5"))
	a.Extra = nil
	require.True(t, a.AllowsConfiguredModel("claude-opus-4-5"))
}
func TestUniversalFundingSourceRejectsUnknownAndNoWalletFallback(t *testing.T) {
	value, err := normalizeFundingSource("")
	require.NoError(t, err)
	require.Equal(t, FundingSourceBalance, value)
	_, err = normalizeFundingSource("auto")
	require.Error(t, err)
	require.True(t, IsUniversalFunding(&APIKey{FundingSource: FundingSourceUniversal}))
	require.False(t, IsUniversalFunding(&APIKey{}))
}

func TestUniversalBillingErrorsKeepQuotaStatusAndHideDatabaseDetails(t *testing.T) {
	status, _, message := UniversalBillingErrorDetails(errors.New("private_db_detail"))
	require.Equal(t, 503, status)
	require.NotContains(t, message, "private_db_detail")
	status, _, message = UniversalBillingErrorDetails(ErrUniversalExhausted)
	require.Equal(t, 403, status)
	require.Contains(t, message, "不会扣除普通余额")
}

func TestUniversalSubscriptionDoesNotBecomeFreeInSimpleMode(t *testing.T) {
	svc := &BillingCacheService{cfg: &config.Config{RunMode: config.RunModeSimple}}
	err := svc.CheckBillingEligibility(context.Background(), &User{ID: 1}, &APIKey{FundingSource: FundingSourceUniversal}, nil, nil, "")
	require.ErrorIs(t, err, ErrUniversalUnavailable)
}

type universalCaptureBillingRepo struct {
	UsageBillingRepository
	command *UsageBillingCommand
}

func (r *universalCaptureBillingRepo) Apply(_ context.Context, c *UsageBillingCommand) (*UsageBillingApplyResult, error) {
	r.command = c
	return nil, errors.New("simulated database unavailable")
}
func TestUniversalUsageBillingNeverFallsBackToWalletOnFailure(t *testing.T) {
	repo := &universalCaptureBillingRepo{}
	p := &postUsageBillingParams{Cost: &CostBreakdown{TotalCost: 1, ActualCost: 1.2}, User: &User{ID: 1}, APIKey: &APIKey{ID: 11, UserID: 1, FundingSource: FundingSourceUniversal}, Account: &Account{ID: 5, Type: AccountTypeOAuth, Platform: PlatformAnthropic}}
	ctx := WithInflightReservation(context.Background(), &InflightReservation{universalHoldID: "00000000-0000-0000-0000-000000000001"})
	applied, err := applyUsageBilling(ctx, "universal-test-request", &UsageLog{Model: "claude-sonnet-4-5"}, p, &billingDeps{}, repo)
	require.Error(t, err)
	require.False(t, applied)
	require.NotNil(t, repo.command)
	require.Zero(t, repo.command.BalanceCost)
	require.Zero(t, repo.command.SubscriptionCost)
	require.Nil(t, repo.command.SubscriptionID)
	require.Equal(t, 1.2, repo.command.UniversalCost)
	require.Equal(t, BillingTypeSubscription, repo.command.BillingType)
	_, err = applyUsageBilling(ctx, "universal-test-request", nil, p, &billingDeps{}, nil)
	require.ErrorIs(t, err, ErrUniversalUnavailable)
	repo.command = nil
	_, err = applyUsageBilling(context.Background(), "universal-test-request", nil, p, &billingDeps{}, repo)
	require.ErrorIs(t, err, ErrUniversalUnavailable)
	require.Nil(t, repo.command)
}
