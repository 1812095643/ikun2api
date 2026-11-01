//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminGroupClaudeDesktopCreateAndUpdate(t *testing.T) {
	for _, platform := range []string{PlatformAnthropic, PlatformOpenAI} {
		for _, only := range []bool{false, true} {
			repo := &groupRepoStubForAdmin{}
			s := &adminServiceImpl{groupRepo: repo}
			g, err := s.CreateGroup(context.Background(), &CreateGroupInput{Name: "desktop-test", Platform: platform, RateMultiplier: 1, ClaudeCodeOnly: only, AllowClaudeDesktop: true})
			require.NoError(t, err)
			require.Equal(t, platform == PlatformAnthropic && only, g.AllowClaudeDesktop)
		}
	}
	g := &Group{ID: 5, Name: "desktop", Platform: PlatformAnthropic, ClaudeCodeOnly: true}
	repo := &groupRepoStubForAdmin{getByID: g}
	s := &adminServiceImpl{groupRepo: repo}
	enabled := true
	disabled := false
	updated, err := s.UpdateGroup(context.Background(), g.ID, &UpdateGroupInput{AllowClaudeDesktop: &enabled})
	require.NoError(t, err)
	require.True(t, updated.AllowClaudeDesktop)
	updated, err = s.UpdateGroup(context.Background(), g.ID, &UpdateGroupInput{Name: "unrelated"})
	require.NoError(t, err)
	require.True(t, updated.AllowClaudeDesktop, "partial updates must retain the Desktop choice")
	updated, err = s.UpdateGroup(context.Background(), g.ID, &UpdateGroupInput{AllowClaudeDesktop: &disabled})
	require.NoError(t, err)
	require.False(t, updated.AllowClaudeDesktop)
	_, err = s.UpdateGroup(context.Background(), g.ID, &UpdateGroupInput{AllowClaudeDesktop: &enabled})
	require.NoError(t, err)
	updated, err = s.UpdateGroup(context.Background(), g.ID, &UpdateGroupInput{ClaudeCodeOnly: &disabled})
	require.NoError(t, err)
	require.False(t, updated.AllowClaudeDesktop, "turning the parent restriction off clears the exception")
}

func TestAdminGroupClaudeDesktopDuplicatePreservesOnlyApplicableOption(t *testing.T) {
	g := &Group{Platform: PlatformAnthropic, ClaudeCodeOnly: true, AllowClaudeDesktop: true}
	copy := cloneGroupForDuplicate(g, "operation")
	require.True(t, copy.AllowClaudeDesktop)
	g.ClaudeCodeOnly = false
	copy = cloneGroupForDuplicate(g, "operation")
	require.False(t, copy.AllowClaudeDesktop)
}
