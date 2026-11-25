package service

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/MACOS-DO/sub4api/internal/monitoring/detectors"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/telemetry"
	"github.com/gin-gonic/gin"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestReviewTransportFailureIsDetectedAfterSuccessfulFailover(t *testing.T) {
	now := time.Now()
	hub := telemetry.New("ikun2api", "test", "test", 10, 100)
	ctx, tr := hub.Start(context.Background(), "r", now)
	tr.Attempt(7, "model", now)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(ctx)
	svc := &GatewayService{}
	_ = svc.handleUpstreamTransportError(ctx, c, &Account{ID: 7, Platform: "anthropic"}, &net.DNSError{Err: "no such host", Name: "example.invalid", IsNotFound: true}, OpsUpstreamErrorEvent{})
	tr.Attempt(8, "model", now)
	tr.Signal(telemetry.Signal{Output: "content", Supported: true, Done: true}, now)
	tr.Finish(200, false, now)
	findings := detectors.Evaluate(hub.Snapshot(0, now).Events, now, domain.DefaultConfig())
	for _, f := range findings {
		if f.Rule == "U03" {
			return
		}
	}
	t.Fatalf("DNS failed attempt vanished after successful failover; findings=%v", findings)
}

func TestMonitorTransportAcrossForwardersAndClientCancellation(t *testing.T) {
	for _, forwarder := range []string{"anthropic", "openai", "gemini"} {
		for _, kind := range []string{"dns", "timeout", "transport_unknown", "client_cancelled"} {
			t.Run(forwarder+"/"+kind, func(t *testing.T) {
				now := time.Now()
				h := telemetry.New("test", "fixture", "test", 4, 32)
				base, cancel := context.WithCancel(context.Background())
				defer cancel()
				ctx, tr := h.Start(base, "r", now)
				tr.Attempt(1, "model", now)
				var failure error = errors.New("opaque transport failure with private content")
				switch kind {
				case "dns":
					failure = &net.DNSError{Name: "private.example", Err: "not found", IsNotFound: true}
				case "timeout":
					failure = context.DeadlineExceeded
				case "client_cancelled":
					cancel()
					failure = context.Canceled
				}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest("POST", "/chat", nil).WithContext(ctx)
				a := &Account{ID: 1, Platform: forwarder}
				switch forwarder {
				case "anthropic":
					_ = (&GatewayService{}).handleUpstreamTransportError(ctx, c, a, failure, OpsUpstreamErrorEvent{})
				case "openai":
					_ = (&OpenAIGatewayService{}).handleOpenAIUpstreamTransportError(ctx, c, a, failure, false)
				case "gemini":
					_ = (&GeminiMessagesCompatService{}).handleUpstreamTransportError(ctx, c, a, failure)
				}
				tr.Attempt(2, "model", time.Now())
				tr.Signal(telemetry.Signal{Supported: true, Output: "content", Done: true}, time.Now())
				tr.Finish(200, kind == "client_cancelled", time.Now())
				snapshot := h.Snapshot(0, time.Now())
				found := false
				for _, f := range detectors.Evaluate(snapshot.Events, time.Now(), domain.DefaultConfig()) {
					if f.Rule == "U03" {
						found = true
					}
				}
				if found != (kind != "client_cancelled") {
					t.Fatal("transport classification lost or client cancellation misattributed", kind, found)
				}
				raw, _ := json.Marshal(snapshot)
				if strings.Contains(string(raw), "private") {
					t.Fatal("raw transport data exported")
				}
			})
		}
	}
}
