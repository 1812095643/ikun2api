package dto

import (
	"encoding/json"
	"testing"

	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGroupMapper_WeeklyResetPreferenceIsAdminOnly(t *testing.T) {
	group := &service.Group{ID: 1, Platform: service.PlatformAnthropic, PreferSoonestWeeklyReset: true}
	ordinary, err := json.Marshal(GroupFromService(group))
	require.NoError(t, err)
	require.NotContains(t, string(ordinary), "prefer_soonest_weekly_reset")
	admin, err := json.Marshal(GroupFromServiceAdmin(group))
	require.NoError(t, err)
	require.Contains(t, string(admin), `"prefer_soonest_weekly_reset":true`)
}
