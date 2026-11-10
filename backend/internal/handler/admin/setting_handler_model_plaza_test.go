//go:build unit

package admin

import (
	"net/http"
	"testing"

	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestModelPlazaRechargeSettingPartialSaves(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{service.SettingKeyModelPlazaCreditsPerCNY: "10", service.SettingKeySiteName: "Plaza Test"})
	rec := doUpdateSettings(t, h, map[string]any{"model_plaza_credits_per_cny": 20}, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "20", repo.values[service.SettingKeyModelPlazaCreditsPerCNY])
	require.Equal(t, "Plaza Test", repo.values[service.SettingKeySiteName])
	rec = doUpdateSettings(t, h, map[string]any{"site_name": "Renamed"}, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "20", repo.values[service.SettingKeyModelPlazaCreditsPerCNY])
	rec = doUpdateSettings(t, h, map[string]any{"model_plaza_credits_per_cny": 0}, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "0", repo.values[service.SettingKeyModelPlazaCreditsPerCNY])
	rec = doUpdateSettings(t, h, map[string]any{"model_plaza_credits_per_cny": -1}, nil)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "0", repo.values[service.SettingKeyModelPlazaCreditsPerCNY])
}
