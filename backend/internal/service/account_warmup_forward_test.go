//go:build unit

package service

import (
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAccountWarmupForwardFencePreventsUnexpectedSend(t *testing.T) {
	upstream := &anthropicHTTPUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"hello"}],"usage":{"input_tokens":2,"output_tokens":1}}`))}}
	svc := newForwardPartialUsageServiceForTest(upstream)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	body := []byte(`{"model":"claude-sonnet-4-6","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}`)
	parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
	if err != nil {
		t.Fatal(err)
	}
	stop := errors.New("cancelled before sending")
	parsed.warmupBeforeSend = func() error { return stop }
	_, err = svc.Forward(context.Background(), c, newAnthropicOAuthAccountForPartialUsageTest(), parsed)
	if !errors.Is(err, errWarmupSendStopped) || upstream.lastReq != nil {
		t.Fatal("cancelled task reached upstream")
	}
}
func TestAccountWarmupForwardHasNoSignatureRepairRetry(t *testing.T) {
	upstream := &signatureBudgetUpstream{keepFailing: true}
	svc := newForwardPartialUsageServiceForTest(&anthropicHTTPUpstreamRecorder{})
	svc.httpUpstream = upstream
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	parsed, err := ParseGatewayRequest(NewRequestBodyRef([]byte(`{"model":"claude-sonnet-4-6","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}`)), PlatformAnthropic)
	if err != nil {
		t.Fatal(err)
	}
	sends := 0
	parsed.warmupBeforeSend = func() error { sends++; return nil }
	_, _ = svc.Forward(context.Background(), c, newAnthropicOAuthAccountForPartialUsageTest(), parsed)
	if sends != 1 || len(upstream.bodies) != 1 {
		t.Fatalf("bounded warmup made %d sends and %d wire requests", sends, len(upstream.bodies))
	}
}
