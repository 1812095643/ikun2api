//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestProxyFailureGuardRefreshDoesNotOverrideProxyCooldown(t *testing.T) {
	repo := &tokenRefreshAccountRepo{}
	cfg := &config.Config{TokenRefresh: config.TokenRefreshConfig{MaxRetries: 3, RetryBackoffSeconds: 0}}
	svc := NewTokenRefreshService(repo, nil, nil, nil, nil, nil, nil, cfg, nil)
	account := &Account{ID: 7, Platform: PlatformAnthropic, Type: AccountTypeOAuth}
	refresher := &tokenRefresherStub{err: ErrProxyQuarantined}
	err := svc.refreshWithRetry(context.Background(), account, refresher, refresher, time.Hour)
	require.ErrorIs(t, err, ErrProxyQuarantined)
	require.Zero(t, repo.setTempUnschedCalls, "token retry policy must not replace the proxy gate")
	require.Zero(t, repo.setErrorCalls, "network failure must not permanently disable the account")
}
