package admin

import (
	"encoding/json"
	"testing"

	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestPoolSettingsPartialSaveKeepsUnrelatedSettings(t *testing.T) {
	var payload map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(`{"pool_user_synthetic_count":5,"pool_user_synthetic_banned":1,"pool_user_synthetic_platform":"anthropic","pool_user_visible_group_id":0}`), &payload))
	omitted := omittedSettingKeys(payload)
	require.Contains(t, omitted, service.SettingKeySiteName)
	require.Contains(t, omitted, service.SettingKeySMTPHost)
	require.NotContains(t, omitted, service.SettingKeyPoolUserSyntheticCount)
	require.NotContains(t, omitted, service.SettingKeyPoolUserVisibleGroupID)
	require.Contains(t, omittedSettingKeys(map[string]json.RawMessage{"site_name": json.RawMessage(`"Custom"`)}), service.SettingKeyPoolUserSyntheticCount)
}
