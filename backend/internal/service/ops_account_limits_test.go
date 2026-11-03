//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type opsRateLimitFailureRepo struct {
	rateLimit429AccountRepoStub
	err error
}

func (r *opsRateLimitFailureRepo) SetRateLimited(ctx context.Context, id int64, until time.Time) error {
	if r.err != nil {
		return r.err
	}
	return r.rateLimit429AccountRepoStub.SetRateLimited(ctx, id, until)
}

func TestOpsAccountLimitReceiptRequiresPersistenceAndIsRequestScoped(t *testing.T) {
	repo := &opsRateLimitFailureRepo{}
	svc := NewRateLimitService(repo, nil, nil, nil, nil)
	ctx := WithOpsAccountLimitTracking(context.Background())
	otherRequest := WithOpsAccountLimitTracking(context.Background())
	account := &Account{ID: 6, Platform: PlatformAnthropic, Type: AccountTypeOAuth}
	svc.HandleUpstreamError(ctx, account, 429, http.Header{}, []byte(`{"error":{"type":"rate_limit_error","message":"Error"}}`))
	require.True(t, OpsAccountLimitHandled(ctx, 6))
	require.False(t, OpsAccountLimitHandled(ctx, 7))
	require.False(t, OpsAccountLimitHandled(otherRequest, 6))
	require.False(t, OpsAccountLimitHandled(nil, 6))
	repo.err = errors.New("database unavailable")
	svc.HandleUpstreamError(ctx, account, 429, http.Header{}, nil)
	require.False(t, OpsAccountLimitHandled(ctx, 6), "后一次持久化失败不得借用前一次的凭据")
}

func TestAnthropic429RetryAfterParsing(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	for _, tc := range []struct {
		name, raw string
		ok        bool
	}{
		{"seconds", "120", true},
		{"date", now.Add(2 * time.Minute).Format(http.TimeFormat), true},
		{"past", now.Add(-time.Minute).Format(http.TimeFormat), false},
		{"zero", "0", false}, {"negative", "-1", false},
		{"overflow", "18446744073709551615", false},
		{"too_long", "999999999", false}, {"invalid", "tomorrow", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Lowercase keys match the raw upstream transport representation.
			got, ok := anthropic429RetryAfter(http.Header{"retry-after": {tc.raw}}, now)
			require.Equal(t, tc.ok, ok)
			if ok {
				require.Equal(t, now.Add(2*time.Minute), got)
			}
		})
	}
}

func TestAnthropic429WindowResetWinsOverRetryAfter(t *testing.T) {
	repo := &anthropicWindowLimitRepo{}
	svc := NewRateLimitService(repo, nil, nil, nil, nil)
	ctx := WithOpsAccountLimitTracking(context.Background())
	reset := time.Now().Add(time.Hour).Truncate(time.Second)
	headers := http.Header{"Retry-After": {"120"}}
	headers.Set("anthropic-ratelimit-unified-5h-status", "rejected")
	headers.Set("anthropic-ratelimit-unified-5h-reset", strconv.FormatInt(reset.Unix(), 10))
	svc.HandleUpstreamError(ctx, &Account{ID: 6, Platform: PlatformAnthropic, Type: AccountTypeOAuth}, 429, headers, nil)
	require.Equal(t, reset, repo.lastRateLimitReset)
	require.True(t, OpsAccountLimitHandled(ctx, 6))
}

func TestOpsClientRestrictionIgnoreSettingPersists(t *testing.T) {
	repo := newRuntimeSettingRepoStub()
	svc := &OpsService{settingRepo: repo}
	cfg, err := svc.GetOpsAdvancedSettings(context.Background())
	require.NoError(t, err)
	require.False(t, cfg.IgnoreClientRestrictionErrors)
	for _, enabled := range []bool{true, false} {
		cfg.IgnoreClientRestrictionErrors = enabled
		updated, err := svc.UpdateOpsAdvancedSettings(context.Background(), cfg)
		require.NoError(t, err)
		require.Equal(t, enabled, updated.IgnoreClientRestrictionErrors)
		fresh := &OpsService{settingRepo: repo}
		fresh.initRuntimeSettings(context.Background())
		require.Equal(t, enabled, fresh.OpsAdvancedSettingsSnapshot().IgnoreClientRestrictionErrors)
	}
}
