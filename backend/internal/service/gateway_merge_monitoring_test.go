//go:build unit

package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/monitoring/telemetry"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestMergedClaudeStreamFastPathPreservesMonitorAndSignature(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newMinimalGatewayService()
	account := newAnthropicOAuthAccountForPartialUsageTest()
	hub := telemetry.New("fixture", "merge", "test", 16, 128)
	router := gin.New()
	router.Use(hub.Middleware())
	frames := []string{
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"synthetic reasoning"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"fixture-signature"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"fixture answer"}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"message_stop"}`,
	}
	var stream strings.Builder
	for _, frame := range frames {
		kind := gjson.Get(frame, "type").String()
		require.False(t, sseEventNeedsJSONParse(kind, []byte(frame), false, false), "must exercise the upstream fast path")
		stream.WriteString("event: " + kind + "\ndata: " + frame + "\n\n")
	}
	router.POST("/v1/messages", func(c *gin.Context) {
		c.Set("api_key", &APIKey{ID: 7, UserID: 4})
		trace := svc.beginClaudeThinkingTrace(c, account, "claude-opus-5-5", "fixture-session", nil)
		require.NotNil(t, trace)
		telemetry.FromContext(c.Request.Context()).Attempt(account.ID, "claude-opus-5-5", time.Now())
		resp := &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(stream.String()))}
		_, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, account, time.Now(), "claude-opus-5-5", "claude-opus-5-5", false)
		require.NoError(t, err)
		block := gjson.Parse(`{"type":"thinking","thinking":"synthetic reasoning","signature":"fixture-signature"}`)
		follow := svc.beginClaudeThinkingTrace(c, account, "claude-opus-5-5", "fixture-session", thinkingTraceFixture("synthetic reasoning", "fixture-signature"))
		require.Equal(t, "matches_previous_upstream_response", follow.origin(block))
	})
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	req.Header.Set("X-Claude-Code-Session-Id", "fixture-session")
	router.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, stream.String(), recorder.Body.String(), "fast path must preserve SSE bytes")
	finished := 0
	for _, event := range hub.Snapshot(0, time.Now()).Events {
		if event.Kind == "request.finished" {
			finished++
			require.Equal(t, float64(1), event.N("has_output"))
			require.Equal(t, float64(1), event.N("terminal"))
			require.Equal(t, float64(1), event.N("content_output"))
		}
	}
	require.Equal(t, 1, finished)
}
