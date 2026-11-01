//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClaudeDesktopClientIdentification(t *testing.T) {
	for _, ua := range []string{
		"claude-cli/2.1.299 (external, claude-desktop-3p, agent-sdk/0.3.299)",
		"claude-cli/2.1.314 (external, claude-desktop)",
		"claude-cli/2.1.316 (external, claude-desktop-3p)",
	} {
		require.True(t, IsClaudeDesktopUserAgent(ua), ua)
	}
	for _, ua := range []string{
		"", "curl/8.10.0", "python-httpx/0.28.1", "Claude-Desktop/4.4212.0",
		"Mozilla/5.0 Electron/40.0.0 Claude/4.4212.0", "claude-cli/2.1.316 (external, cli)",
		"claude-cli/2.1.316 (external, sdk-cli, agent-sdk/0.3.316)",
		"claude-cli/2.1.316 (external, fake-claude-desktop-3p)",
		"claude-cli/2.1.316 (external, claude-desktop-3p-fake)",
		"fake-claude-cli/2.1.316 (external, claude-desktop-3p)",
	} {
		require.False(t, IsClaudeDesktopUserAgent(ua), ua)
	}
}

func TestClaudeDesktopGroupRestrictionMatrix(t *testing.T) {
	for _, only := range []bool{false, true} {
		for _, allowDesktop := range []bool{false, true} {
			for _, client := range []string{"cli", "desktop", "other", "unvalidated_desktop"} {
				ctx := SetClaudeCodeClient(context.Background(), client == "cli" || client == "desktop")
				ctx = SetClaudeDesktopClient(ctx, client == "desktop" || client == "unvalidated_desktop")
				g := &Group{ID: 1, Platform: PlatformAnthropic, Status: StatusActive, Hydrated: true, ClaudeCodeOnly: only, AllowClaudeDesktop: allowDesktop}
				expected := !only || client == "cli" || (client == "desktop" && allowDesktop)
				require.Equal(t, expected, g.AllowsClaudeClient(ctx), "only=%v allowDesktop=%v client=%s", only, allowDesktop, client)
				s := &GatewayService{groupRepo: &mockGroupRepoForGateway{groups: map[int64]*Group{1: g}}}
				selected, _, err := s.resolveGatewayGroup(ctx, &g.ID)
				if expected {
					require.NoError(t, err)
					require.Equal(t, g.ID, selected.ID)
				} else {
					require.ErrorIs(t, err, ErrClaudeCodeOnly)
				}
			}
		}
	}
}

func TestClaudeDesktopRestrictionFallbackDoesNotGrantOriginalGroup(t *testing.T) {
	fallback := int64(2)
	g := &Group{ID: 1, Platform: PlatformAnthropic, Hydrated: true, ClaudeCodeOnly: true, FallbackGroupID: &fallback}
	other := &Group{ID: fallback, Platform: PlatformAnthropic, Hydrated: true}
	s := &GatewayService{groupRepo: &mockGroupRepoForGateway{groups: map[int64]*Group{1: g, 2: other}}}
	ctx := SetClaudeDesktopClient(SetClaudeCodeClient(context.Background(), true), true)
	selected, _, err := s.resolveGatewayGroup(ctx, &g.ID)
	require.NoError(t, err)
	require.Equal(t, fallback, selected.ID)
	g.AllowClaudeDesktop = true
	selected, _, err = s.resolveGatewayGroup(ctx, &g.ID)
	require.NoError(t, err)
	require.Equal(t, g.ID, selected.ID)
}

func TestClaudeDesktopAuthSnapshotRoundtripAndLegacyDefault(t *testing.T) {
	id := int64(7)
	key := &APIKey{ID: 8, Key: "local-test-only", GroupID: &id, User: &User{ID: 1}, Group: &Group{ID: id, Platform: PlatformAnthropic, Hydrated: true, ClaudeCodeOnly: true, AllowClaudeDesktop: true}}
	s := &APIKeyService{}
	snapshot := s.snapshotFromAPIKey(context.Background(), key)
	data, err := json.Marshal(&APIKeyAuthCacheEntry{Snapshot: snapshot})
	require.NoError(t, err)
	var cached APIKeyAuthCacheEntry
	require.NoError(t, json.Unmarshal(data, &cached))
	materialized, used, err := s.applyAuthCacheEntry(key.Key, &cached)
	require.NoError(t, err)
	require.True(t, used)
	require.True(t, materialized.Group.AllowClaudeDesktop)
	// An old snapshot cannot accidentally opt a group into Desktop access.
	var legacy APIKeyAuthGroupSnapshot
	require.NoError(t, json.Unmarshal([]byte(`{"claude_code_only":true}`), &legacy))
	require.False(t, legacy.AllowClaudeDesktop)
}
