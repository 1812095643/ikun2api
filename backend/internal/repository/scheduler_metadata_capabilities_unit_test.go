//go:build unit

package repository

import (
	"context"
	"encoding/json"
	"maps"
	"testing"

	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSchedulerSnapshotKeepsOpenAIServiceTiersAndUpdates(t *testing.T) {
	for _, kind := range []string{service.AccountTypeOAuth, service.AccountTypeAPIKey} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			cache := newSchedulerCacheUnit(t)
			account := service.Account{ID: 701, Platform: service.PlatformOpenAI, Type: kind,
				Extra: map[string]any{service.OpenAIServiceTiersExtraKey: map[string]any{
					"fast_enabled": false, "ultrafast_enabled": true, "ultrafast_multiplier": 11.0,
				}},
			}
			bucket := service.SchedulerBucket{GroupID: 1, Platform: service.PlatformOpenAI, Mode: service.SchedulerModeSingle}
			token, err := cache.CaptureBucketWriteToken(ctx, bucket)
			require.NoError(t, err)
			require.NoError(t, cache.SetSnapshot(ctx, bucket, token, []service.Account{account}))
			snapshot, hit, err := cache.GetSnapshot(ctx, bucket)
			require.NoError(t, err)
			require.True(t, hit)
			require.False(t, snapshot[0].SupportsOpenAIServiceTier("priority"))
			require.True(t, snapshot[0].SupportsOpenAIServiceTier("ultrafast"))
			settings := snapshot[0].Extra[service.OpenAIServiceTiersExtraKey].(map[string]any)
			require.Equal(t, 11.0, settings["ultrafast_multiplier"])
			require.True(t, shouldEnqueueSchedulerOutboxForExtraUpdates(account.Extra))

			account.Extra[service.OpenAIServiceTiersExtraKey] = map[string]any{
				"fast_enabled": true, "ultrafast_enabled": false, "fast_multiplier": 3.0,
			}
			require.NoError(t, cache.SetAccount(ctx, &account))
			snapshot, hit, err = cache.GetSnapshot(ctx, bucket)
			require.NoError(t, err)
			require.True(t, hit)
			require.True(t, snapshot[0].SupportsOpenAIServiceTier("priority"))
			require.False(t, snapshot[0].SupportsOpenAIServiceTier("ultrafast"))
			settings = snapshot[0].Extra[service.OpenAIServiceTiersExtraKey].(map[string]any)
			require.Equal(t, 3.0, settings["fast_multiplier"])

			legacy := *snapshot[0]
			legacy.Extra = maps.Clone(snapshot[0].Extra)
			delete(legacy.Extra, service.OpenAIServiceTiersExtraKey)
			raw, err := json.Marshal(legacy)
			require.NoError(t, err)
			require.NoError(t, cache.rdb.Set(ctx, schedulerAccountMetaKey("701"), raw, 0).Err())
			cache.dropSnapshotEntry(bucket.String())
			_, hit, err = cache.GetSnapshot(ctx, bucket)
			require.NoError(t, err)
			require.False(t, hit, "缺少档位字段的旧投影必须回源")
		})
	}
}

func TestSchedulerMetadataServiceTierDefaultsAndMalformedSettings(t *testing.T) {
	defaults := buildSchedulerMetadataAccount(service.Account{Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey})
	require.Contains(t, defaults.Extra, service.OpenAIServiceTiersExtraKey)
	require.True(t, defaults.SupportsOpenAIServiceTier("ultrafast"))
	malformed := buildSchedulerMetadataAccount(service.Account{Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Extra: map[string]any{service.OpenAIServiceTiersExtraKey: nil}})
	require.Contains(t, malformed.Extra, service.OpenAIServiceTiersExtraKey)
	require.False(t, malformed.SupportsOpenAIServiceTier("ultrafast"))
}

// buildSchedulerMetadataAccount 是调度候选的唯一投影出口，openAIAccountSkipReason
// 按 SupportsOpenAIEndpointCapability 判 capability_mismatch。Seedance 分支直接读
// credentials.openai_capabilities 且不走「未配置即放行」，所以该键一旦被裁掉，
// Seedance 账号在选号阶段必然被剔除，而 /v1/models 走 DB 全量仍会列出模型。
func TestSchedulerMetadataKeepsOpenAICapabilities(t *testing.T) {
	account := service.Account{
		ID:       1,
		Platform: service.PlatformOpenAI,
		Type:     service.AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":             "sk-test",
			"base_url":            "https://relay.example.com/api/v3",
			"openai_capabilities": []string{"seedance"},
		},
	}

	projected := buildSchedulerMetadataAccount(account)

	require.Equal(t, []string{"seedance"}, projected.Credentials["openai_capabilities"])
	require.Equal(t, "https://relay.example.com/api/v3", projected.Credentials["base_url"])
	require.True(t, projected.SupportsOpenAIEndpointCapability(service.OpenAIEndpointCapabilitySeedance))
}

func TestSchedulerMetadataDropsUnlistedCredentials(t *testing.T) {
	account := service.Account{
		ID:       2,
		Platform: service.PlatformOpenAI,
		Type:     service.AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":             "sk-test",
			"base_url":            "https://relay.example.com/api/v3",
			"openai_capabilities": []string{"seedance"},
			"model_mapping":       map[string]string{"seedance-2-5": "doubao-seedance-2-5-260628"},
			"unlisted_secret":     "drop-me",
		},
	}

	projected := buildSchedulerMetadataAccount(account)

	require.Equal(t, map[string]string{"seedance-2-5": "doubao-seedance-2-5-260628"}, projected.Credentials["model_mapping"])
	require.NotContains(t, projected.Credentials, "unlisted_secret")
}
