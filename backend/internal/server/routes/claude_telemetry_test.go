package routes

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MACOS-DO/sub4api/internal/handler"
	"github.com/MACOS-DO/sub4api/internal/server/middleware"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

func telemetryTestRouter() *gin.Engine {
	r := gin.New()
	auth := middleware.APIKeyAuthMiddleware(func(c *gin.Context) {
		if c.GetHeader("Authorization") != "Bearer local-test" {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Set("api_key", &service.APIKey{ID: 17})
		c.Next()
	})
	RegisterCommonRoutes(r)
	RegisterClaudeTelemetryRoutes(r, &handler.GatewayHandler{}, auth)
	return r
}

func TestClaudeTelemetryRoutesDropAllUntrustedPayloads(t *testing.T) {
	r := telemetryTestRouter()
	for _, path := range []string{"/api/event_logging/batch", "/api/event_logging/v2/batch", "/api/event_logging/future", "/v1/api/event_logging/v2/batch", "/api/claude_code/metrics", "/v1/api/claude_code/metrics", "/api/v2/rum", "/v1/api/v2/rum", "/api/v2/logs", "/v1/api/v2/logs", "/v1/logs", "/v1/metrics", "/v1/traces"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"git_remote_url":"private-canary","head_sha":"hash-canary","email":"email-canary"}`))
			req.Header.Set("x-claude-code-wiggly-dove", "private-canary")
			recorder := httptest.NewRecorder()
			r.ServeHTTP(recorder, req)
			if recorder.Code != http.StatusOK || gjson.Get(recorder.Body.String(), "accepted_count").Int() != 0 || strings.Contains(recorder.Body.String(), "canary") {
				t.Fatal("遥测未安全终止")
			}
		})
	}
}

func TestClaudeTelemetryRoutesRequireAuthenticationBeforeReconstruction(t *testing.T) {
	r := telemetryTestRouter()
	for _, authorization := range []string{"Bearer local-test", "Bearer invalid", ""} {
		req := httptest.NewRequest(http.MethodPost, "/api/event_logging/v2/batch", strings.NewReader(`{"events":[{"event_type":"unknown","event_data":{"git_remote_url":"private-canary"}}]}`))
		req.Header.Set("Authorization", authorization)
		recorder := httptest.NewRecorder()
		r.ServeHTTP(recorder, req)
		if authorization == "Bearer invalid" {
			if recorder.Code != http.StatusUnauthorized {
				t.Fatal("错误凭据进入重建入口")
			}
		} else if recorder.Code != http.StatusOK || gjson.Get(recorder.Body.String(), "queued_count").Int() != 0 {
			t.Fatal("没有真实观测时不能创建活动")
		}
	}
}

func TestClaudeTelemetryRoutesBoundCompressedAndRawBodies(t *testing.T) {
	r := telemetryTestRouter()
	for _, compressed := range []bool{false, true} {
		for _, oversized := range []bool{false, true} {
			body := []byte(`{"events":[]}`)
			if oversized {
				body = []byte(`{"events":[],"private":"` + strings.Repeat("x", service.MaxClaudeTelemetryBodyBytes+1) + `"}`)
			}
			if compressed {
				var out bytes.Buffer
				writer := gzip.NewWriter(&out)
				_, _ = writer.Write(body)
				_ = writer.Close()
				body = out.Bytes()
			}
			req := httptest.NewRequest(http.MethodPost, "/api/event_logging/v2/batch", bytes.NewReader(body))
			req.Header.Set("Authorization", "Bearer local-test")
			if compressed {
				req.Header.Set("Content-Encoding", "gzip")
			}
			recorder := httptest.NewRecorder()
			r.ServeHTTP(recorder, req)
			if oversized && recorder.Code != http.StatusBadRequest {
				t.Fatal("压缩前后均需限制大小")
			}
			if !oversized && recorder.Code != http.StatusOK {
				t.Fatal("正常载荷不能被误拒绝")
			}
		}
	}
}
