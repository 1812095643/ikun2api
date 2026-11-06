package repository

import (
	"testing"

	dbent "github.com/MACOS-DO/sub4api/ent"
	"github.com/stretchr/testify/require"
)

func TestGroupMapper_WeeklyResetPreference(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		group := groupEntityToService(&dbent.Group{ID: 1, Platform: "anthropic", PreferSoonestWeeklyReset: enabled})
		require.Equal(t, enabled, group.PreferSoonestWeeklyReset)
	}
}
