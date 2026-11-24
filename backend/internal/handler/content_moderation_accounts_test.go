package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MACOS-DO/sub4api/internal/securityaudit"
	middleware2 "github.com/MACOS-DO/sub4api/internal/server/middleware"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAccountAuditGateSkipsExemptButChecksFallbackAndNextWSTurn(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &service.ContentModerationConfig{Enabled: true, Mode: service.ContentModerationModePreBlock,
		AllGroups: true, ExemptAccountIDs: []int64{11}, KeywordBlockingMode: service.ContentModerationKeywordModeKeywordOnly,
		BlockedKeywords: []string{"blocked-content"}, BlockMessage: "audit denied", AutoBanEnabled: false, EmailOnHit: false}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	settings := &contentModerationHandlerSettingRepo{values: map[string]string{
		service.SettingKeyContentModerationConfig: string(raw), service.SettingKeyRiskControlEnabled: "true",
	}}
	moderation := service.NewContentModerationService(settings, &contentModerationHandlerTestRepo{}, nil, nil, nil, nil, nil, nil)
	coordinator := securityaudit.NewCoordinator(securityaudit.NewLegacyModerationAdapter(moderation), nil)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	subject := middleware2.AuthSubject{UserID: 7}
	key := &service.APIKey{ID: 9, UserID: 7}
	first := []byte(`{"input":"clean content"}`)
	d := runSecurityAudit(c, nil, coordinator, moderation, key, subject, service.ContentModerationProtocolOpenAIResponses, "gpt-test", first, "first_turn")
	require.True(t, d.AllowNextStage)
	require.NoError(t, service.CheckAccountContentModeration(c.Request.Context(), nil, &service.Account{ID: 11}))
	require.NoError(t, service.CheckAccountContentModeration(c.Request.Context(), nil, &service.Account{ID: 22}))

	next := []byte(`{"input":"blocked-content","account_id":11}`)
	d = runSecurityAudit(c, nil, coordinator, moderation, key, subject, service.ContentModerationProtocolOpenAIResponses, "gpt-test", next, "subsequent_turn")
	require.True(t, d.AllowNextStage)
	require.NoError(t, service.CheckAccountContentModeration(c.Request.Context(), nil, &service.Account{ID: 11}))
	require.Error(t, service.CheckAccountContentModeration(c.Request.Context(), nil, &service.Account{ID: 22}), "不能复用上一轮的通过结果或客户端伪造的账号 ID")
}

func TestAccountContentModerationErrorAfterKeepaliveUsesTerminalSSE(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Header("Content-Type", "text/event-stream")
	c.Writer.WriteHeaderNow()
	_, _ = c.Writer.Write([]byte(": ping\n\n"))
	writeAccountContentModerationError(c, service.ContentModerationProtocolOpenAIResponses, &service.ContentModerationDecision{Blocked: true, StatusCode: http.StatusForbidden, Message: "audit denied"})
	require.Equal(t, http.StatusOK, c.Writer.Status())
	require.NotEmpty(t, service.GetOpsStreamErrors(c))
}
