package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAccountPoolEmailSourceAndMasking(t *testing.T) {
	snapshot := "snapshot123@example.test"
	for _, tc := range []struct {
		name     string
		account  *Account
		snapshot *string
		want     string
	}{
		{"primary email before custom name", &Account{Name: "Claude Pro 01", Extra: map[string]any{"email_address": "  abcdefghi@example.test  ", "email": "other123@example.test"}, Credentials: map[string]any{"email": "third123@example.test"}}, &snapshot, "abc***ghi@example.test"},
		{"extra email", &Account{Name: "Claude Pro 01", Extra: map[string]any{"email_address": " ", "email": "abcdefghi@example.test"}}, nil, "abc***ghi@example.test"},
		{"credential email", &Account{Name: "Claude Pro 01", Credentials: map[string]any{"email": "abcdefghi@example.test", "access_token": "DO_NOT_EXPOSE"}}, nil, "abc***ghi@example.test"},
		{"legacy email name", &Account{Name: "abcdefghi@example.test"}, nil, "abc***ghi@example.test"},
		{"deleted account email snapshot", nil, &snapshot, MaskAccountName(snapshot)},
		{"custom name without email", &Account{Name: "Claude Pro 01"}, nil, "—"},
		{"display address is not a bare email", &Account{Name: "Operator <abcdefghi@example.test>"}, nil, "—"},
		{"ignore auxiliary email", &Account{Name: "Internal label", Extra: map[string]any{"auxiliary_email": "private@example.test"}}, nil, "—"},
		{"invalid metadata can fall back", &Account{Name: "Internal label", Extra: map[string]any{"email_address": 42, "email": "invalid"}, Credentials: map[string]any{"email": "abcdefghi@example.test"}}, nil, "abc***ghi@example.test"},
		{"empty account", nil, nil, "—"},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.want, PoolAccountEmailLabel(tc.account, tc.snapshot)) })
	}
}
func TestAccountPoolEmailListMatchesUsageProjection(t *testing.T) {
	acc := &Account{ID: 9, Name: "Internal Claude account", Platform: "anthropic", Type: "oauth", Extra: map[string]any{"email_address": "abcdefghi@example.test"}}
	svc := NewAccountPoolService(poolTestAdmin{accounts: []*Account{acc}}, nil, nil, &poolTestUsageRepo{}, nil)
	rows, err := svc.ListAccountsForUser(context.Background(), 42, PoolAccountFilter{})
	require.NoError(t, err)
	require.Len(t, rows.Items, 1)
	require.Equal(t, "abc***ghi@example.test", rows.Items[0].Name)
	require.Equal(t, PoolAccountEmailLabel(acc, &acc.Name), rows.Items[0].Name)
	require.Equal(t, "Internal Claude account", acc.Name)
	for _, search := range []string{"Internal Claude", "def"} {
		filtered, err := svc.ListAccountsForUser(context.Background(), 42, PoolAccountFilter{Search: search})
		require.NoError(t, err)
		require.Empty(t, filtered.Items)
	}
}
