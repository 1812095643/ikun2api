package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/requesttiming"
	"github.com/MACOS-DO/sub4api/internal/server/middleware"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type traceRecorderFixture struct {
	enabled bool
	records []service.ConnectionTrace
}

func (r *traceRecorderFixture) ConnectionDiagnosticsEnabled(context.Context) bool { return r.enabled }
func (r *traceRecorderFixture) RecordConnectionTrace(v service.ConnectionTrace) {
	r.records = append(r.records, v)
}

func TestConnectionDiagnosticsCapturesWithoutPayloads(t *testing.T) {
	for _, tc := range []struct {
		name   string
		chunks []string
		status int
		state  string
	}{
		{"json", nil, 200, "completed"},
		{"claude_terminal", []string{"event: message_", "stop\r\ndata: {}\r\n\r\n"}, 200, "completed"},
		{"gpt_terminal", []string{"data: test\n\nevent: response.completed\ndata: {}\n\n"}, 200, "completed"},
		{"gpt_data_terminal", []string{"data: {\"type\":\"response.completed\",\"response\":{}}\n\n"}, 200, "completed"},
		{"claude_data_terminal", []string{"data: {\"type\": \"message_stop\"}\r\n\r\n"}, 200, "completed"},
		{"terminal_frame_cut_off", []string{"event: response.completed\ndata: {\"response\":"}, 200, "unknown"},
		{"terminal_word_in_text", []string{"data: {\"type\":\"response.output_text.delta\",\"delta\":\"response.completed\"}\n\n"}, 200, "unknown"},
		{"missing_terminal", []string{"data: partial\n\n"}, 200, "unknown"},
		{"failed_stream", []string{"event: response.failed\ndata: {}\n\n"}, 200, "failed"},
		{"status_failure", nil, 502, "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := &traceRecorderFixture{enabled: true}
			router := gin.New()
			router.Use(ConnectionDiagnosticsMiddleware(recorder))
			router.POST("/v1/messages", func(c *gin.Context) {
				body, err := io.ReadAll(c.Request.Body)
				require.NoError(t, err)
				require.Contains(t, string(body), "private-prompt")
				if tc.chunks == nil {
					c.JSON(tc.status, gin.H{"value": "private-response"})
					return
				}
				c.Header("Content-Type", "text/event-stream")
				for _, chunk := range tc.chunks {
					_, _ = c.Writer.WriteString(chunk)
					c.Writer.Flush()
				}
			})
			req := httptest.NewRequest(http.MethodPost, "/v1/messages?token=private-query", strings.NewReader("private-prompt"))
			req.Header.Set("Authorization", "Bearer private-key")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			require.Len(t, recorder.records, 1)
			row := recorder.records[0]
			require.Equal(t, tc.state, row.State)
			require.Equal(t, int64(len("private-prompt")), row.RequestBytes)
			require.NotEmpty(t, row.TraceID)
			raw, err := json.Marshal(row)
			require.NoError(t, err)
			for _, secret := range []string{"private-prompt", "private-response", "private-key", "private-query"} {
				require.NotContains(t, string(raw), secret)
			}
		})
	}
}

func TestConnectionDiagnosticsDisabledDoesNotWrapBody(t *testing.T) {
	recorder := &traceRecorderFixture{}
	router := gin.New()
	router.Use(ConnectionDiagnosticsMiddleware(recorder))
	router.POST("/test", func(c *gin.Context) { require.Nil(t, requesttiming.From(c.Request.Context())); c.Status(204) })
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/test", nil))
	require.Empty(t, recorder.records)
}

func TestConnectionDiagnosticsIngressRejectionsRemainAggregated(t *testing.T) {
	recorder := &traceRecorderFixture{enabled: true}
	router := gin.New()
	router.Use(ConnectionDiagnosticsMiddleware(recorder))
	router.POST("/test", func(c *gin.Context) {
		middleware.MarkIngressRejected(c, middleware.IngressRejectInvalidAPIKey)
		c.Status(401)
	})
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/test", nil))
	require.Empty(t, recorder.records)
}

func BenchmarkConnectionDiagnosticsSSEObserver(b *testing.B) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	collector := requesttiming.New(time.Now(), 0)
	c.Request = c.Request.WithContext(requesttiming.With(c.Request.Context(), collector))
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	w := &diagnosticWriter{ResponseWriter: c.Writer, collector: collector, ctx: c}
	chunk := []byte("data: {\"delta\":\"" + strings.Repeat("x", 32768) + "\"}\n\n")
	w.observe(chunk)
	b.ReportAllocs()
	b.SetBytes(int64(len(chunk)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.observe(chunk)
	}
}
