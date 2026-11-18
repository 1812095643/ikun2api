package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBridgePassIdentityAndExpiry(t *testing.T) {
	now := time.Unix(1800000000, 0)
	signer := NewBridgePassSigner("test site signing key")
	pass, until := signer.Issue(7, "test-api-key", now)
	require.Len(t, pass, 66)
	require.Equal(t, now.Add(BridgePassTTL), until)
	require.Equal(t, BridgePassValid, signer.Verify(pass, 7, "test-api-key", until.Add(-time.Second)))
	require.Equal(t, BridgePassExpired, signer.Verify(pass, 7, "test-api-key", until))
	require.Equal(t, BridgePassMissing, signer.Verify(pass, 8, "test-api-key", now))
	require.Equal(t, BridgePassMissing, signer.Verify(pass, 7, "rotated-key", now))
	require.Equal(t, BridgePassMissing, signer.Verify(pass+"x", 7, "test-api-key", now))
	require.Equal(t, BridgePassMissing, NewBridgePassSigner("other site").Verify(pass, 7, "test-api-key", now))
	require.Equal(t, BridgePassMissing, NewBridgePassSigner("").Verify(pass, 7, "test-api-key", now))
	changed := byte('A')
	if pass[30] == changed {
		changed = 'B'
	}
	require.Equal(t, BridgePassMissing, signer.Verify(pass[:30]+string(changed)+pass[31:], 7, "test-api-key", now))
}

func TestBridgeProtectedFallbackGroup(t *testing.T) {
	fallback := &Group{ID: 2, Platform: PlatformAnthropic, Status: StatusActive, Hydrated: true, BridgeRequired: true}
	group := &Group{ID: 1, Platform: PlatformAnthropic, Status: StatusActive, Hydrated: true, ClaudeCodeOnly: true, FallbackGroupID: &fallback.ID}
	svc := &GatewayService{groupRepo: bridgeTestGroups{groups: map[int64]*Group{1: group, 2: fallback}}}
	_, _, err := svc.resolveGatewayGroup(context.Background(), &group.ID)
	require.ErrorIs(t, err, ErrBridgeRequired)
	resolved, id, err := svc.resolveGatewayGroup(WithBridgePassState(context.Background(), BridgePassValid), &group.ID)
	require.NoError(t, err)
	require.Equal(t, fallback.ID, *id)
	require.Equal(t, fallback, resolved)
}

type bridgeTestGroups struct {
	GroupRepository
	groups map[int64]*Group
}

func (r bridgeTestGroups) GetByIDLite(_ context.Context, id int64) (*Group, error) {
	return r.groups[id], nil
}
