package repository

import (
	"testing"

	dbent "github.com/MACOS-DO/sub4api/ent"
	"github.com/stretchr/testify/require"
)

func TestGroupClaudeDesktopMapperRetainsOptIn(t *testing.T) {
	group := groupEntityToService(&dbent.Group{ID: 1, Platform: "anthropic", ClaudeCodeOnly: true, AllowClaudeDesktop: true})
	require.True(t, group.ClaudeCodeOnly)
	require.True(t, group.AllowClaudeDesktop)
}
