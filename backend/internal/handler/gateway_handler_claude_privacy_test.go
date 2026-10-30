package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MACOS-DO/sub4api/internal/config"
	middleware "github.com/MACOS-DO/sub4api/internal/server/middleware"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGatewayClaudePrivacyRejectsBeforeSchedulingBillingAndAudit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for name, call := range map[string]func(*GatewayHandler, *gin.Context){"messages": (*GatewayHandler).Messages, "count_tokens": (*GatewayHandler).CountTokens} {
		for variant, safeguards := range map[string]string{
			"array_paths":   `[{"type":"dangerous_tool_use","classifier_context":{"rule_roots":{"project":"PRIVATE_PROJECT"}}}]`,
			"object_paths":  `{"enabled":true,"classifier_context":{"trusted_directories":{"primary":{"path":"PRIVATE_PROJECT"}}}}`,
			"future_fields": `[{"type":"dangerous_tool_use","classifier_context":{"future_context":"PRIVATE_PROJECT"}}]`,
			"wrong_type":    `"PRIVATE_PROJECT"`,
		} {
			t.Run(name+"/"+variant, func(t *testing.T) {
				body := `{"model":"claude-sonnet-4-6","max_tokens":64,"messages":[{"role":"user","content":"hello"}],"safeguards":` + safeguards + `}`
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+name, strings.NewReader(body))
				c.Request.Header.Set("Content-Type", "application/json")
				c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 3, UserID: 5})
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 5, Concurrency: 1})
				// 所有下游依赖均未配置，触及选号、计费或审计会使本用例直接暴露问题。
				h := &GatewayHandler{cfg: &config.Config{Gateway: config.GatewayConfig{MaxBodySize: 1 << 20}}}
				require.NotPanics(t, func() { call(h, c) })
				require.Equal(t, http.StatusBadRequest, recorder.Code)
				require.Contains(t, recorder.Body.String(), "invalid_request_error")
				require.Contains(t, recorder.Body.String(), "本地逐次授权模式")
				require.NotContains(t, recorder.Body.String(), "PRIVATE_PROJECT")
			})
		}
	}
}

func TestGatewayClaudeAuxiliaryRejectsBeforeScheduling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for name, call := range map[string]func(*GatewayHandler, *gin.Context){"messages": (*GatewayHandler).Messages, "count_tokens": (*GatewayHandler).CountTokens} {
		for _, field := range []string{`"cache_control":["PRIVATE"]`, `"diagnostics":{"previous_message_id":{"path":"PRIVATE"}}`, `"system":[{"type":"text","text":"hello","cache_control":{"type":"ephemeral","ttl":{"path":"PRIVATE"}}}]`} {
			t.Run(name+field, func(t *testing.T) {
				body := `{"model":"claude-haiku-4-5","max_tokens":64,"messages":[{"role":"user","content":"hello"}],` + field + `}`
				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+name, strings.NewReader(body))
				c.Request.Header.Set("Content-Type", "application/json")
				c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 3, UserID: 5})
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 5, Concurrency: 1})
				h := &GatewayHandler{cfg: &config.Config{Gateway: config.GatewayConfig{MaxBodySize: 1 << 20}}}
				require.NotPanics(t, func() { call(h, c) })
				require.Equal(t, 400, w.Code)
				require.NotContains(t, w.Body.String(), "PRIVATE")
			})
		}
	}
}
