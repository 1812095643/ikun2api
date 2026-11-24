package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type passthroughPartialErrorBody struct {
	reads  int
	closed bool
}

func (b *passthroughPartialErrorBody) Read(p []byte) (int, error) {
	b.reads++
	if b.reads > 1 {
		return 0, io.EOF
	}
	return copy(p, `{"type":"error","error":{"type":"invalid_request_error","message":"synthetic upstream detail"}}`), errors.New("synthetic upstream read failure")
}
func (b *passthroughPartialErrorBody) Close() error { b.closed = true; return nil }

func TestPassthrough400ReadFailureRetainsOriginalEvidence(t *testing.T) {
	body := &passthroughPartialErrorBody{}
	upstream := &anthropicHTTPUpstreamRecorder{resp: &http.Response{StatusCode: 400, Header: make(http.Header), Body: body}}
	cfg := &config.Config{}
	svc := &GatewayService{cfg: cfg, httpUpstream: upstream, responseHeaderFilter: compileResponseHeaderFilter(cfg)}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	_, err := svc.forwardAnthropicAPIKeyPassthrough(context.Background(), c, newAnthropicAPIKeyAccountForTest(), []byte(`{"model":"claude-opus-5-5","messages":[{"role":"user","content":"test"}]}`), "claude-opus-5-5", "claude-opus-5-5", false, time.Now())
	require.Error(t, err)
	require.Equal(t, 400, rec.Code)
	require.Contains(t, rec.Body.String(), "synthetic upstream detail")
	require.True(t, body.closed)
	require.Equal(t, 1, body.reads, "do not reread a partially consumed failed response")
}
