//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestUsageLog_GatewayTimingPreservesCacheAndLegacyRows(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)
	user := mustCreateUser(t, client, &service.User{Email: "gateway-timing-" + uuid.NewString() + "@example.com"})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-timing-" + uuid.NewString(), Name: "timing"})
	account := mustCreateAccount(t, client, &service.Account{Name: "timing-account", Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth})
	wait, pre, switches, first, upload, upstream := 2000, 2500, 1, 4300, 300, 1800
	row := &service.UsageLog{UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, RequestID: uuid.NewString(), Model: "claude-sonnet-4-6", InputTokens: 10, OutputTokens: 6, CacheCreation30mTokens: 37, CacheCreation12hTokens: 59, CreatedAt: time.Now(), GatewayWaitMs: &wait, PreForwardMs: &pre, AccountSwitchCount: &switches, ClientFirstTokenMs: &first, UploadMs: &upload, FirstTokenMs: &upstream}
	inserted, err := repo.Create(ctx, row)
	require.NoError(t, err)
	require.True(t, inserted)
	got, err := repo.GetByID(ctx, row.ID)
	require.NoError(t, err)
	require.Equal(t, 37, got.CacheCreation30mTokens)
	require.Equal(t, 59, got.CacheCreation12hTokens)
	require.Equal(t, &wait, got.GatewayWaitMs)
	require.Equal(t, &pre, got.PreForwardMs)
	require.Equal(t, &switches, got.AccountSwitchCount)
	require.Equal(t, &first, got.ClientFirstTokenMs)
	require.Equal(t, &upload, got.UploadMs)
	require.Equal(t, &upstream, got.FirstTokenMs, "upstream TTFT must remain available for tok/s")
	legacy := &service.UsageLog{UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, RequestID: uuid.NewString(), Model: "claude-sonnet-4-6", FirstTokenMs: &upstream, CreatedAt: time.Now()}
	_, err = repo.Create(ctx, legacy)
	require.NoError(t, err)
	old, err := repo.GetByID(ctx, legacy.ID)
	require.NoError(t, err)
	require.Nil(t, old.GatewayWaitMs)
	require.Nil(t, old.PreForwardMs)
	require.Nil(t, old.ClientFirstTokenMs)
	require.Equal(t, &upstream, old.FirstTokenMs)
}
