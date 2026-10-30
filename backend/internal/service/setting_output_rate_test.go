//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/MACOS-DO/sub4api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestSettingService_UsageOutputRateMode(t *testing.T) {
	for _, mode := range []string{"generation", "total"} {
		t.Run(mode, func(t *testing.T) {
			repo := &bmUpdateRepoStub{}
			svc := NewSettingService(repo, &config.Config{})
			require.NoError(t, svc.UpdateSettings(context.Background(), &SystemSettings{UsageOutputRateMode: mode}))
			require.Equal(t, mode, repo.updates[SettingKeyUsageOutputRateMode])

			reader := NewSettingService(&settingPublicRepoStub{values: map[string]string{SettingKeyUsageOutputRateMode: mode}}, &config.Config{})
			public, err := reader.GetPublicSettings(context.Background())
			require.NoError(t, err)
			require.Equal(t, mode, public.UsageOutputRateMode)
			injected, err := reader.GetPublicSettingsForInjection(context.Background())
			require.NoError(t, err)
			require.Equal(t, mode, injected.(*PublicSettingsInjectionPayload).UsageOutputRateMode)
		})
	}
}

func TestSettingService_UsageOutputRateModeDefaultsAndValidation(t *testing.T) {
	svc := NewSettingService(&settingPublicRepoStub{}, &config.Config{})
	public, err := svc.GetPublicSettings(context.Background())
	require.NoError(t, err)
	require.Equal(t, "generation", public.UsageOutputRateMode)

	repo := &bmUpdateRepoStub{}
	writer := NewSettingService(repo, &config.Config{})
	require.Error(t, writer.UpdateSettings(context.Background(), &SystemSettings{UsageOutputRateMode: "unexpected"}))
	require.Empty(t, repo.updates)
}
