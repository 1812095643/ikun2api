package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MACOS-DO/sub4api/internal/server/middleware"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type usageFundingUserRepo struct {
	service.UserRepository
	calls int
}

func (r *usageFundingUserRepo) GetByID(_ context.Context, id int64) (*service.User, error) {
	r.calls++
	return &service.User{ID: id, Balance: 17.5}, nil
}

func (r *usageFundingUserRepo) GetUserAvatar(context.Context, int64) (*service.UserAvatar, error) {
	return nil, nil
}

func TestUsageFundingSourceIsAuthoritative(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, source, groupType string
		want                    int
	}{
		{name: "unset funding source uses wallet", groupType: service.SubscriptionTypeSubscription, want: http.StatusOK},
		{name: "explicit wallet ignores retired group", source: service.FundingSourceBalance, groupType: service.SubscriptionTypeSubscription, want: http.StatusOK},
		{name: "universal standard group never falls back", source: service.FundingSourceUniversal, groupType: service.SubscriptionTypeStandard, want: http.StatusServiceUnavailable},
		{name: "universal retired group never falls back", source: service.FundingSourceUniversal, groupType: service.SubscriptionTypeSubscription, want: http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/usage", nil)
			c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 1, UserID: 7, FundingSource: tc.source, Group: &service.Group{Name: "retired plan", SubscriptionType: tc.groupType}})
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
			// Stale group-subscription context must never select the funding source.
			c.Set(string(middleware.ContextKeySubscription), &service.UserSubscription{WeeklyUsageUSD: 100})
			repo := &usageFundingUserRepo{}
			handler := &GatewayHandler{userService: service.NewUserService(repo, nil, nil, nil)}
			handler.Usage(c)
			require.Equal(t, tc.want, recorder.Code, recorder.Body.String())
			var response map[string]any
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
			if tc.want == http.StatusOK {
				require.Equal(t, "unrestricted", response["mode"])
				require.Equal(t, "钱包余额", response["planName"])
				require.Equal(t, 17.5, response["balance"])
				require.Equal(t, 17.5, response["remaining"])
				require.NotContains(t, response, "subscription")
				require.Equal(t, 1, repo.calls)
			} else {
				require.Contains(t, recorder.Body.String(), "billing_unavailable")
				require.NotContains(t, response, "balance")
				require.Zero(t, repo.calls)
			}
		})
	}
}
