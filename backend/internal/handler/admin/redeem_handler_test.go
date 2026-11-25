package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type existingBalanceRedeemRepo struct {
	service.RedeemCodeRepository
	calls int
}

func (r *existingBalanceRedeemRepo) GetByCode(_ context.Context, code string) (*service.RedeemCode, error) {
	r.calls++
	userID := int64(1)
	return &service.RedeemCode{Code: code, Type: "balance", Value: 10, Status: service.StatusUsed, UsedBy: &userID}, nil
}

func newCreateAndRedeemHandler() *RedeemHandler {
	return &RedeemHandler{adminService: newStubAdminService(), redeemService: &service.RedeemService{}}
}

func TestCreateAndRedeem_BalanceCompatibility(t *testing.T) {
	for _, codeType := range []string{"", "balance"} {
		t.Run("type="+codeType, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			repo := &existingBalanceRedeemRepo{}
			h := &RedeemHandler{redeemService: service.NewRedeemService(repo, nil, nil, nil, nil, nil, nil, nil)}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			body := map[string]any{"code": "existing-balance", "value": 10.0, "user_id": 1}
			if codeType != "" {
				body["type"] = codeType
			}
			data, err := json.Marshal(body)
			require.NoError(t, err)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/redeem-codes/create-and-redeem", bytes.NewReader(data))
			c.Request.Header.Set("Content-Type", "application/json")
			h.CreateAndRedeem(c)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Equal(t, 1, repo.calls)
			var result struct {
				Data struct {
					RedeemCode struct {
						Type string `json:"type"`
					} `json:"redeem_code"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
			require.Equal(t, "balance", result.Data.RedeemCode.Type)
		})
	}
}

func TestCreateAndRedeem_LegacySubscriptionRetired(t *testing.T) {
	groupID := int64(5)
	for _, tc := range []struct {
		name    string
		groupID *int64
		days    int
	}{
		{name: "missing group", days: 30},
		{name: "zero days", groupID: &groupID},
		{name: "negative days", groupID: &groupID, days: -7},
		{name: "formerly valid", groupID: &groupID, days: 31},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			body, err := json.Marshal(map[string]any{"code": "retired-subscription", "type": "subscription", "value": 29.9, "user_id": 1, "group_id": tc.groupID, "validity_days": tc.days})
			require.NoError(t, err)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/redeem-codes/create-and-redeem", bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			// No panic recovery: rejection must precede all repository access.
			newCreateAndRedeemHandler().CreateAndRedeem(c)
			require.Equal(t, http.StatusForbidden, w.Code)
			var response struct {
				Reason string `json:"reason"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
			require.Equal(t, "LEGACY_SUBSCRIPTION_RETIRED", response.Reason)
		})
	}
}

func TestResolveRedeemCodeExpiresAt_FromDays(t *testing.T) {
	days := 3
	expiresAt, err := resolveRedeemCodeExpiresAt(nil, &days)
	require.NoError(t, err)
	require.NotNil(t, expiresAt)
	require.WithinDuration(t, time.Now().UTC().AddDate(0, 0, days), *expiresAt, 2*time.Second)
}

func TestResolveRedeemCodeExpiresAt_RejectsPastAbsoluteTime(t *testing.T) {
	past := time.Now().UTC().Add(-time.Minute)
	expiresAt, err := resolveRedeemCodeExpiresAt(&past, nil)
	require.Error(t, err)
	require.Nil(t, expiresAt)
}

func TestResolveRedeemCodeExpiresAt_RejectsNonPositiveDays(t *testing.T) {
	days := 0
	expiresAt, err := resolveRedeemCodeExpiresAt(nil, &days)
	require.Error(t, err)
	require.Nil(t, expiresAt)
}

func TestResolveRedeemCodeExpiresAt_RejectsConflictingInputs(t *testing.T) {
	future := time.Now().UTC().Add(time.Hour)
	days := 3
	expiresAt, err := resolveRedeemCodeExpiresAt(&future, &days)
	require.Error(t, err)
	require.Nil(t, expiresAt)
}
