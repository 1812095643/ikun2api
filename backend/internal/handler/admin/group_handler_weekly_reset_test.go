package admin

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGroupRequests_WeeklyResetPreferencePreservesOmissionAndFalse(t *testing.T) {
	for _, body := range []string{`{}`, `{"prefer_soonest_weekly_reset":false}`, `{"prefer_soonest_weekly_reset":true}`} {
		var create CreateGroupRequest
		var update UpdateGroupRequest
		require.NoError(t, json.Unmarshal([]byte(body), &create))
		require.NoError(t, json.Unmarshal([]byte(body), &update))
		if body == `{}` {
			require.Nil(t, create.PreferSoonestWeeklyReset)
			require.Nil(t, update.PreferSoonestWeeklyReset)
		} else {
			require.NotNil(t, create.PreferSoonestWeeklyReset)
			require.NotNil(t, update.PreferSoonestWeeklyReset)
			require.Equal(t, body == `{"prefer_soonest_weekly_reset":true}`, *create.PreferSoonestWeeklyReset)
			require.Equal(t, *create.PreferSoonestWeeklyReset, *update.PreferSoonestWeeklyReset)
		}
	}
}
