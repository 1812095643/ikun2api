package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
	"time"
)

func TestClaudeIdentityCacheRefreshPreservesIdentity(t *testing.T) {
	fp := defaultFingerprint()
	fp.ClientID = strings.Repeat("a", 64)
	fp.IdentityVersion = 1
	fp.UpdatedAt = time.Now().Add(-25 * time.Hour).Unix()
	cache := &stubIdentityCache{fingerprint: &fp}
	svc := newTestIdentityService(cache)
	got, err := svc.GetOrCreateFingerprint(context.Background(), 41, nil)
	require.NoError(t, err)
	require.Equal(t, fp.ClientID, got.ClientID)
	require.Equal(t, fp.ProfileID, got.ProfileID)
	require.Greater(t, got.UpdatedAt, fp.UpdatedAt)
	require.Equal(t, 1, cache.setCalls)
	again, err := svc.GetOrCreateFingerprint(context.Background(), 41, nil)
	require.NoError(t, err)
	require.Equal(t, got, again)
	require.Equal(t, 1, cache.setCalls)
}
