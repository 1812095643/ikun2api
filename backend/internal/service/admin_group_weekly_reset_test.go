//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminGroupWeeklyReset_DefaultCreateUpdateAndDuplicate(t *testing.T) {
	for _, platform := range []string{PlatformAnthropic, PlatformOpenAI, PlatformGemini} {
		for _, choice := range []*bool{nil, weeklyResetBool(true), weeklyResetBool(false)} {
			repo := &groupRepoStubForAdmin{}
			svc := &adminServiceImpl{groupRepo: repo}
			group, err := svc.CreateGroup(context.Background(), &CreateGroupInput{Name: "weekly-reset", Platform: platform, RateMultiplier: 1, PreferSoonestWeeklyReset: choice})
			require.NoError(t, err)
			require.Equal(t, platform == PlatformAnthropic && (choice == nil || *choice), group.PreferSoonestWeeklyReset)
		}
	}
	group := &Group{ID: 1, Name: "weekly-reset", Platform: PlatformAnthropic, PreferSoonestWeeklyReset: true}
	svc := &adminServiceImpl{groupRepo: &groupRepoStubForAdmin{getByID: group}}
	updated, err := svc.UpdateGroup(context.Background(), 1, &UpdateGroupInput{PreferSoonestWeeklyReset: weeklyResetBool(false)})
	require.NoError(t, err)
	require.False(t, updated.PreferSoonestWeeklyReset)
	updated, err = svc.UpdateGroup(context.Background(), 1, &UpdateGroupInput{Name: "unrelated"})
	require.NoError(t, err)
	require.False(t, updated.PreferSoonestWeeklyReset)
	require.False(t, cloneGroupForDuplicate(group, "operation").PreferSoonestWeeklyReset)
	updated, err = svc.UpdateGroup(context.Background(), 1, &UpdateGroupInput{PreferSoonestWeeklyReset: weeklyResetBool(true)})
	require.NoError(t, err)
	require.True(t, updated.PreferSoonestWeeklyReset)
	require.True(t, cloneGroupForDuplicate(group, "operation").PreferSoonestWeeklyReset)
}

func weeklyResetBool(value bool) *bool { return &value }
