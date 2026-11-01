//go:build unit

package admin

import (
	"net/http"
	"testing"

	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestMenuVisibilityPartialUpdatePreservesOtherSettings(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeySiteName:       "Independent site",
		service.SettingKeyMenuVisibility: `{"/news":false}`,
	})
	r := doUpdateSettings(t, h, map[string]any{"site_name": "New name"}, nil)
	require.Equal(t, http.StatusOK, r.Code)
	require.Equal(t, `{"/news":false}`, repo.values[service.SettingKeyMenuVisibility])
	r = doUpdateSettings(t, h, map[string]any{"menu_visibility": map[string]bool{"/news": true, "/admin/tickets": false}}, nil)
	require.Equal(t, http.StatusOK, r.Code)
	require.Equal(t, "New name", repo.values[service.SettingKeySiteName])
	require.Equal(t, map[string]bool{"/news": true, "/admin/tickets": false}, service.ParseMenuVisibility(repo.values[service.SettingKeyMenuVisibility]))
}

func TestMenuVisibilityRejectsInvalidPathsAtomically(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{service.SettingKeySiteName: "Keep"})
	for _, path := range []string{"https://other.example", "//other.example", "/../settings", "/news?token=secret"} {
		r := doUpdateSettings(t, h, map[string]any{"site_name": "Unexpected", "menu_visibility": map[string]bool{path: false}}, nil)
		require.Equal(t, http.StatusBadRequest, r.Code)
		require.Equal(t, "Keep", repo.values[service.SettingKeySiteName])
	}
}

func TestCustomPageURLSupportsLocalDeployments(t *testing.T) {
	for _, value := range []string{"/status-detection/", "/page?a=1", "https://example.com/page"} {
		require.NoError(t, validateCustomPageURL(value), value)
	}
	for _, value := range []string{"//example.com/page", `/\example.com`, "/%5cexample.com", "javascript:alert(1)", "/page\n"} {
		require.Error(t, validateCustomPageURL(value), value)
	}
}
