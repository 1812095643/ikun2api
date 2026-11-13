package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPreferQuotaHeadroomAccountsKeepsBelowThresholdForNewPlacement(t *testing.T) {
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	reset := now.Add(time.Hour)
	nearLimit := &Account{
		Platform:         PlatformAnthropic,
		Type:             AccountTypeOAuth,
		SessionWindowEnd: &reset,
		Extra:            map[string]any{"session_window_utilization": 0.90},
	}
	withHeadroom := &Account{
		Platform:         PlatformAnthropic,
		Type:             AccountTypeOAuth,
		SessionWindowEnd: &reset,
		Extra:            map[string]any{"session_window_utilization": 0.70},
	}
	accounts := []*Account{nearLimit, withHeadroom}

	got := preferQuotaHeadroomAccounts(accounts, 85, now)

	require.Equal(t, []*Account{withHeadroom}, got)
}

func TestPreferQuotaHeadroomAccountsKeepsAllWhenEveryAccountIsNearLimit(t *testing.T) {
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	reset := now.Add(time.Hour)
	accounts := []*Account{
		{Platform: PlatformAnthropic, Type: AccountTypeOAuth, SessionWindowEnd: &reset, Extra: map[string]any{"session_window_utilization": 0.90}},
		{Platform: PlatformAnthropic, Type: AccountTypeOAuth, SessionWindowEnd: &reset, Extra: map[string]any{"session_window_utilization": 0.95}},
	}

	got := preferQuotaHeadroomAccounts(accounts, 85, now)

	require.Equal(t, accounts, got, "soft threshold must not reject a group with no account below it")
}
