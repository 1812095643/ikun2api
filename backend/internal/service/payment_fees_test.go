package service

import (
	"context"
	"math"
	"testing"

	"github.com/MACOS-DO/sub4api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestPaymentFeeRatesPersistOverrideFreeAndReset(t *testing.T) {
	repo := &paymentConfigSettingRepoStub{values: map[string]string{SettingRechargeFeeRate: "0.6"}}
	svc := &PaymentConfigService{settingRepo: repo}
	rates := map[string]float64{"alipay": 0.29, "wxpay": 0}
	require.NoError(t, svc.UpdatePaymentConfig(context.Background(), UpdatePaymentConfigRequest{RechargeFeeRates: &rates}))
	cfg := svc.parsePaymentConfig(repo.values)
	require.Equal(t, 0.29, cfg.RechargeFeeRateFor("alipay"))
	require.Equal(t, 0.29, cfg.RechargeFeeRateFor("alipay_direct"))
	require.Zero(t, cfg.RechargeFeeRateFor("wxpay"))
	require.Zero(t, cfg.RechargeFeeRateFor("wxpay_direct"))
	require.Equal(t, 0.6, cfg.RechargeFeeRateFor("stripe"))
	require.Equal(t, "100.29", payment.CalculatePayAmount(100, cfg.RechargeFeeRateFor("alipay")))
	require.Equal(t, "100.00", payment.CalculatePayAmount(100, cfg.RechargeFeeRateFor("wxpay")))
	enabled := true
	require.NoError(t, svc.UpdatePaymentConfig(context.Background(), UpdatePaymentConfigRequest{Enabled: &enabled}))
	require.NotContains(t, repo.updates, SettingRechargeFeeRates)
	require.Equal(t, 0.29, svc.parsePaymentConfig(repo.values).RechargeFeeRateFor("alipay"))
	rates = map[string]float64{}
	require.NoError(t, svc.UpdatePaymentConfig(context.Background(), UpdatePaymentConfigRequest{RechargeFeeRates: &rates}))
	require.JSONEq(t, "{}", repo.values[SettingRechargeFeeRates])
	require.Equal(t, 0.6, svc.parsePaymentConfig(repo.values).RechargeFeeRateFor("alipay"))
}

func TestPaymentFeeRatesValidationRejectsInvalidMoneyAndAliases(t *testing.T) {
	for _, rate := range []float64{-1, 100.01, math.NaN(), math.Inf(1), 0.123} {
		require.Error(t, validateRechargeFeeRate(rate))
	}
	for _, rate := range []float64{0, 0.29, 0.6, 2.55, 100} {
		require.NoError(t, validateRechargeFeeRate(rate))
	}
	for _, method := range []string{"alipay_direct", " wxpay", "", "Alipay", "a\nb"} {
		require.Error(t, normalizeRechargeFeeRates(map[string]float64{method: 0.6}))
	}
	repo := &paymentConfigSettingRepoStub{values: map[string]string{SettingRechargeFeeRates: `{"alipay":0.6}`}}
	rates := map[string]float64{"wxpay": -1}
	require.Error(t, (&PaymentConfigService{settingRepo: repo}).UpdatePaymentConfig(context.Background(), UpdatePaymentConfigRequest{RechargeFeeRates: &rates}))
	require.Nil(t, repo.updates)
	require.JSONEq(t, `{"alipay":0.6}`, repo.values[SettingRechargeFeeRates])
}

func TestPaymentMethodLimitsExposeEffectiveFeeRates(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	_, err := client.PaymentProviderInstance.Create().SetProviderKey(payment.TypeEasyPay).
		SetName("test-only gateway").SetConfig("{}").SetSupportedTypes("alipay,wxpay,ldc").SetEnabled(true).Save(ctx)
	require.NoError(t, err)
	svc := &PaymentConfigService{entClient: client, settingRepo: &paymentConfigSettingRepoStub{values: map[string]string{
		SettingRechargeFeeRate: "0.6", SettingRechargeFeeRates: `{"alipay":0.29,"wxpay":0}`,
	}}}
	limits, err := svc.GetAvailableMethodLimits(ctx)
	require.NoError(t, err)
	require.Equal(t, 0.29, limits.Methods["alipay"].FeeRate)
	require.Zero(t, limits.Methods["wxpay"].FeeRate)
	require.Equal(t, 0.6, limits.Methods["ldc"].FeeRate)
	methods, err := svc.GetMethodLimits(ctx, []string{"alipay_direct", "wxpay_direct"})
	require.NoError(t, err)
	require.Len(t, methods, 2)
	require.Equal(t, 0.29, methods[0].FeeRate)
	require.Zero(t, methods[1].FeeRate)
}
