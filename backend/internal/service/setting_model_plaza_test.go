//go:build unit

package service

import (
	"context"
	"math"
	"testing"

	"github.com/MACOS-DO/sub4api/internal/config"
	infraerrors "github.com/MACOS-DO/sub4api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestModelPlazaRechargeRatioPersistsAndReachesRuntime(t *testing.T) {
	ctx := context.Background()
	repo := &settingUpdateRepoStub{}
	svc := NewSettingService(repo, &config.Config{})
	require.NoError(t, svc.UpdateSettings(ctx, &SystemSettings{ModelPlazaCreditsPerCNY: 10}))
	require.Equal(t, "10", repo.updates[SettingKeyModelPlazaCreditsPerCNY])
	reader := NewSettingService(&settingGetAllRepoStub{values: repo.updates}, &config.Config{})
	settings, err := reader.GetAllSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, 10.0, settings.ModelPlazaCreditsPerCNY)
	runtime := NewSettingService(&settingPublicRepoStub{values: repo.updates}, &config.Config{}).GetModelPlazaRuntime(ctx)
	require.Equal(t, 10.0, runtime.CreditsPerCNY)
}

func TestModelPlazaRechargeRatioRejectsInvalidValuesBeforeWrite(t *testing.T) {
	for _, ratio := range []float64{-1, math.NaN(), math.Inf(1)} {
		repo := &settingUpdateRepoStub{}
		err := NewSettingService(repo, &config.Config{}).UpdateSettings(context.Background(), &SystemSettings{ModelPlazaCreditsPerCNY: ratio})
		require.Equal(t, "INVALID_MODEL_PLAZA_RECHARGE_RATIO", infraerrors.Reason(err))
		require.Nil(t, repo.updates)
	}
}

func TestModelPlazaRechargeRatioAbsentOrInvalidDisablesConversion(t *testing.T) {
	for _, raw := range []string{"", "0", "-1", "NaN", "+Inf", "bad"} {
		require.Equal(t, 0.0, parseModelPlazaCreditsPerCNY(raw), raw)
	}
	require.Equal(t, 10.5, parseModelPlazaCreditsPerCNY(" 10.5 "))
}
