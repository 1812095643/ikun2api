package service

import (
	"context"
	"encoding/json"
	"math"
	"regexp"

	infraerrors "github.com/MACOS-DO/sub4api/internal/pkg/errors"
	"github.com/shopspring/decimal"
)

const SettingRechargeFeeRates = "RECHARGE_FEE_RATES"

var paymentFeeMethodPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func validateRechargeFeeRate(value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 100 {
		return infraerrors.BadRequest("INVALID_RECHARGE_FEE_RATE", "手续费率需为 0 至 100 之间的数值")
	}
	if decimal.NewFromFloat(value).Exponent() < -2 {
		return infraerrors.BadRequest("INVALID_RECHARGE_FEE_RATE", "手续费率最多保留两位小数")
	}
	return nil
}

func normalizeRechargeFeeRates(rates map[string]float64) error {
	if len(rates) > 64 {
		return infraerrors.BadRequest("INVALID_RECHARGE_FEE_RATES", "支付方式手续费配置过多")
	}
	for method, rate := range rates {
		if !paymentFeeMethodPattern.MatchString(method) || NormalizeVisibleMethod(method) != method {
			return infraerrors.BadRequest("INVALID_RECHARGE_FEE_METHOD", "请按实际支付方式设置手续费")
		}
		if err := validateRechargeFeeRate(rate); err != nil {
			return err
		}
	}
	return nil
}

func parseRechargeFeeRates(raw string) map[string]float64 {
	rates := map[string]float64{}
	if raw == "" {
		return rates
	}
	if json.Unmarshal([]byte(raw), &rates) != nil || normalizeRechargeFeeRates(rates) != nil || rates == nil {
		return map[string]float64{}
	}
	return rates
}

// RechargeFeeRateFor 按用户选择的支付方式覆盖默认费率；明确的 0 表示免手续费。
func (cfg *PaymentConfig) RechargeFeeRateFor(method string) float64 {
	if rate, exists := cfg.RechargeFeeRates[NormalizeVisibleMethod(method)]; exists {
		return rate
	}
	return cfg.RechargeFeeRate
}

func (s *PaymentConfigService) paymentFeeConfig(ctx context.Context) (*PaymentConfig, error) {
	if s.settingRepo == nil {
		return &PaymentConfig{}, nil
	}
	values, err := s.settingRepo.GetMultiple(ctx, []string{SettingRechargeFeeRate, SettingRechargeFeeRates})
	if err != nil {
		return nil, err
	}
	return s.parsePaymentConfig(values), nil
}
