package handler

import (
	"context"
	"errors"
	"github.com/MACOS-DO/sub4api/internal/pkg/ctxkey"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
)

func smartWSKey() *service.APIKey {
	id := int64(1)
	return &service.APIKey{ID: 99, UserID: 7, GroupID: &id, RouteGroupIDs: []int64{1, 2, 3}, FundingSource: "universal_subscription", User: &service.User{ID: 7, Status: service.StatusActive}}
}
func smartWSHydrator(_ context.Context, key *service.APIKey, id int64) (*service.APIKey, error) {
	clone := *key
	clone.GroupID = &id
	clone.Group = &service.Group{ID: id, Status: service.StatusActive, Platform: service.PlatformOpenAI, RateMultiplier: float64(id)}
	if id == 1 {
		clone.Group.Platform = service.PlatformAnthropic
	}
	return &clone, nil
}
func smartWSPreparer(ctx context.Context, key *service.APIKey) (context.Context, string, error) {
	return ctx, "gpt-test", nil
}
func TestSmartRoutingWebSocketUsesFirstFrameAndFinalGroupIdentity(t *testing.T) {
	var tried []int64
	released := 0
	key, held, ctx, err := selectSmartWSRoute(context.Background(), smartWSKey(), "gpt-test", []byte(`{"model":"gpt-test"}`), smartWSHydrator, smartWSPreparer,
		func(ctx context.Context, key *service.APIKey, model string) (*service.AccountSelectionResult, service.OpenAIAccountScheduleDecision, error) {
			tried = append(tried, *key.GroupID)
			require.Equal(t, key.Group, ctx.Value(ctxkey.Group))
			require.Equal(t, "gpt-test", model)
			if *key.GroupID == 2 {
				return nil, service.OpenAIAccountScheduleDecision{}, service.ErrNoAvailableAccounts
			}
			return &service.AccountSelectionResult{Account: &service.Account{ID: 30}, ReleaseFunc: func() { released++ }}, service.OpenAIAccountScheduleDecision{}, nil
		})
	require.NoError(t, err)
	require.Equal(t, []int64{2, 3}, tried)
	require.Equal(t, int64(3), *key.GroupID)
	require.Equal(t, 3.0, key.Group.RateMultiplier)
	require.Equal(t, "universal_subscription", key.FundingSource)
	require.Equal(t, key.Group, ctx.Value(ctxkey.Group))
	held.release()
	held.release()
	require.Equal(t, 1, released)
}
func TestSmartRoutingWebSocketSkipsRevokedGroupWithoutSelection(t *testing.T) {
	calls := 0
	_, held, _, err := selectSmartWSRoute(context.Background(), smartWSKey(), "gpt-test", []byte(`{"model":"gpt-test"}`),
		func(ctx context.Context, key *service.APIKey, id int64) (*service.APIKey, error) {
			candidate, err := smartWSHydrator(ctx, key, id)
			candidate.Group.IsExclusive = true
			return candidate, err
		},
		smartWSPreparer, func(context.Context, *service.APIKey, string) (*service.AccountSelectionResult, service.OpenAIAccountScheduleDecision, error) {
			calls++
			return nil, service.OpenAIAccountScheduleDecision{}, nil
		})
	require.ErrorIs(t, err, service.ErrNoAvailableAccounts)
	require.Nil(t, held)
	require.Zero(t, calls)
}
func TestSmartRoutingWebSocketDoesNotAdvanceOnInfrastructureFailure(t *testing.T) {
	calls := 0
	fatal := errors.New("scheduler unavailable")
	_, _, _, err := selectSmartWSRoute(context.Background(), smartWSKey(), "gpt-test", []byte(`{"model":"gpt-test"}`), smartWSHydrator, smartWSPreparer,
		func(context.Context, *service.APIKey, string) (*service.AccountSelectionResult, service.OpenAIAccountScheduleDecision, error) {
			calls++
			return nil, service.OpenAIAccountScheduleDecision{}, fatal
		})
	require.ErrorIs(t, err, fatal)
	require.Equal(t, 1, calls)
}
