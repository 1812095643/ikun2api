package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyAuthSnapshot_WeeklyResetPreferenceRoundtripAndUpgrade(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		groupID := int64(10)
		key := &APIKey{ID: 1, UserID: 2, GroupID: &groupID, Status: StatusActive, User: &User{ID: 2, Status: StatusActive}, Group: &Group{ID: groupID, Platform: PlatformAnthropic, Status: StatusActive, Hydrated: true, PreferSoonestWeeklyReset: enabled}}
		svc := &APIKeyService{}
		snapshot := svc.snapshotFromAPIKey(context.Background(), key)
		data, err := json.Marshal(&APIKeyAuthCacheEntry{Snapshot: snapshot})
		require.NoError(t, err)
		var cached APIKeyAuthCacheEntry
		require.NoError(t, json.Unmarshal(data, &cached))
		restored, used, err := svc.applyAuthCacheEntry("weekly-test", &cached)
		require.NoError(t, err)
		require.True(t, used)
		require.Equal(t, enabled, restored.Group.PreferSoonestWeeklyReset)
		cached.Snapshot.Version = 26
		restored, used, err = svc.applyAuthCacheEntry("weekly-test", &cached)
		require.NoError(t, err)
		require.False(t, used)
		require.Nil(t, restored, "old snapshots without this field must be reloaded")
	}
}
