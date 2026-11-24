//go:build unit

package service

import (
	"context"
	"net/http"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/config"
	"github.com/MACOS-DO/sub4api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

type openAIRecoveryPauseRepo struct {
	*openAIRecoveryRepo
	pauseCalls int
}

func (r *openAIRecoveryPauseRepo) PauseOpenAIOAuthIfUnchanged(_ context.Context, expected *Account, message string, retry bool) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !reflect.DeepEqual(expected.Credentials, r.account.Credentials) || expected.Status != r.account.Status || expected.Schedulable != r.account.Schedulable || !reflect.DeepEqual(expected.ProxyID, r.account.ProxyID) {
		return false, nil
	}
	r.pauseCalls++
	r.account = snapshotOAuthRefreshAccount(r.account)
	r.account.Extra = map[string]any{OpenAIOAuthRecoveryExtraKey: map[string]any{
		"fingerprint": OpenAIOAuthRecoveryFingerprint(expected), "auto_retry": retry,
		"reason": message, "resume_schedulable": expected.Schedulable,
	}}
	r.account.Status, r.account.Schedulable, r.account.ErrorMessage = StatusError, false, message
	return true, nil
}

func (*openAIRecoveryPauseRepo) ApplyOpenAIOAuthReloginIfUnchanged(context.Context, *Account, map[string]any) (bool, error) {
	panic("本测试不写回登录结果")
}

func TestOpenAIOAuthRecoveryPermanentFailurePausesAndFailsOverBeforeForwarding(t *testing.T) {
	account, base, executor, _ := newOpenAIRecoveryFixture()
	repo := &openAIRecoveryPauseRepo{openAIRecoveryRepo: base}
	provider := NewOpenAITokenProvider(repo, nil, nil)
	provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, nil), executor)
	executor.refresh = func(context.Context, *Account) (map[string]any, error) {
		return nil, &openai.RefreshTokenError{StatusCode: http.StatusUnauthorized, Permanent: true}
	}
	_, err := provider.RefreshAfterUnauthorized(context.Background(), account, account.GetOpenAIAccessToken())
	require.Error(t, err)
	require.Equal(t, 1, repo.pauseCalls)
	require.Equal(t, StatusError, repo.account.Status)
	require.False(t, repo.account.Schedulable)
	require.True(t, OpenAIOAuthRecoveryPending(repo.account))
	gateway := &OpenAIGatewayService{openAITokenProvider: provider}
	_, _, err = gateway.GetAccessToken(context.Background(), account)
	var failover *UpstreamFailoverError
	require.ErrorAs(t, err, &failover)
	require.Equal(t, GatewayFailureStageAccountAuth, failover.Stage)
	require.True(t, failover.ShouldRetryNextAccount())
	require.False(t, failover.RetryableOnSameAccount)
	require.Equal(t, int32(1), executor.calls.Load())
}

func TestOpenAIOAuthRecoveryLateUnauthorizedDoesNotPauseReauthorizedAccount(t *testing.T) {
	account, base, executor, _ := newOpenAIRecoveryFixture()
	repo := &openAIRecoveryPauseRepo{openAIRecoveryRepo: base}
	provider := NewOpenAITokenProvider(repo, nil, nil)
	provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, nil), executor)
	credentials := shallowCopyMap(account.Credentials)
	credentials["access_token"] = "new-authorization"
	require.NoError(t, repo.UpdateCredentials(context.Background(), account.ID, credentials))
	provider.RecordUnauthorizedAfterRefresh(context.Background(), account, account.GetOpenAIAccessToken())
	require.Zero(t, repo.pauseCalls)
	require.True(t, repo.account.IsActive())
	require.True(t, repo.account.Schedulable)
	token, err := provider.GetAccessToken(context.Background(), repo.account)
	require.NoError(t, err)
	require.Equal(t, "new-authorization", token)
}

func TestOpenAIOAuthAuthFailureCounterExcludesUnrelatedErrors(t *testing.T) {
	account, _, _, _ := newOpenAIRecoveryFixture()
	api := NewOAuthRefreshAPI(nil, nil)
	for _, status := range []int{400, 429, 500, 502, 503, 529} {
		api.RecordOpenAIOAuthAuthFailure(account, status, []byte(`{"error":{"code":"token_invalidated"}}`))
	}
	api.RecordOpenAIOAuthAuthFailure(account, 403, []byte(`{"error":{"code":"cyber_policy","message":"401 unauthorized"}}`))
	require.Zero(t, api.OpenAIOAuthAuthFailureCount(account, 10*time.Minute))
	api.RecordOpenAIOAuthAuthFailure(account, 401, nil)
	api.RecordOpenAIOAuthAuthFailure(account, 403, []byte(`{"error":{"code":"token_expired"}}`))
	require.Equal(t, 2, api.OpenAIOAuthAuthFailureCount(account, 10*time.Minute))
	account.Credentials["access_token"] = "new-version"
	require.Zero(t, api.OpenAIOAuthAuthFailureCount(account, 10*time.Minute))
}

func TestOpenAIOAuthAuthFailureCounterWindowAndBound(t *testing.T) {
	account, _, _, _ := newOpenAIRecoveryFixture()
	api := NewOAuthRefreshAPI(nil, nil)
	var wg sync.WaitGroup
	for range 150 {
		wg.Add(1)
		go func() { defer wg.Done(); api.RecordOpenAIOAuthAuthFailure(account, 401, nil) }()
	}
	wg.Wait()
	require.Equal(t, 100, api.OpenAIOAuthAuthFailureCount(account, time.Hour))
	api.openAIAuthMu.Lock()
	state := api.openAIAuthFailures[account.ID]
	state.times = []time.Time{time.Now().Add(-11 * time.Minute), time.Now().Add(-2 * time.Minute)}
	api.openAIAuthFailures[account.ID] = state
	api.openAIAuthMu.Unlock()
	require.Equal(t, 1, api.OpenAIOAuthAuthFailureCount(account, 10*time.Minute))
	require.Equal(t, 2, api.OpenAIOAuthAuthFailureCount(account, time.Hour))
}

func TestOpenAIOAuthRecoveryPendingRespectsManualPauseAndTerminalResult(t *testing.T) {
	account, _, _, _ := newOpenAIRecoveryFixture()
	account.Status, account.Schedulable, account.ErrorMessage = StatusError, false, OpenAIOAuthRejectedMessage
	account.Extra = map[string]any{OpenAIOAuthRecoveryExtraKey: map[string]any{
		"fingerprint": OpenAIOAuthRecoveryFingerprint(account), "reason": account.ErrorMessage, "auto_retry": true,
	}}
	require.True(t, OpenAIOAuthRecoveryPending(account))
	state := account.Extra[OpenAIOAuthRecoveryExtraKey].(map[string]any)
	state["auto_retry"] = false
	require.False(t, OpenAIOAuthRecoveryPending(account))
	account.Extra[OpenAIOAuthRecoveryExtraKey] = nil
	require.False(t, OpenAIOAuthRecoveryPending(account))
	delete(account.Extra, OpenAIOAuthRecoveryExtraKey)
	require.True(t, OpenAIOAuthRecoveryPending(account))
	account.ErrorMessage = "network timeout on proxy 4012"
	require.False(t, OpenAIOAuthRecoveryPending(account))
}

func TestOpenAIOAuthBackgroundRefreshReusesDurablePauseAndInvalidatesCache(t *testing.T) {
	account, base, executor, _ := newOpenAIRecoveryFixture()
	repo := &openAIRecoveryPauseRepo{openAIRecoveryRepo: base}
	executor.refresh = func(context.Context, *Account) (map[string]any, error) {
		return nil, &openai.RefreshTokenError{StatusCode: 401, Permanent: true}
	}
	invalidator := &tokenCacheInvalidatorStub{}
	svc := NewTokenRefreshService(repo, nil, nil, nil, nil, invalidator, nil, &config.Config{TokenRefresh: config.TokenRefreshConfig{MaxRetries: 1}}, nil)
	svc.SetRefreshAPI(NewOAuthRefreshAPI(repo, nil))
	err := svc.refreshWithRetry(context.Background(), account, executor, executor, time.Hour)
	var permanent *accountPermanentRefreshError
	require.ErrorAs(t, err, &permanent)
	require.True(t, permanent.persistentlyBlocked)
	require.Equal(t, 1, repo.pauseCalls)
	require.Equal(t, 1, invalidator.calls)
}
