//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/config"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/telemetry"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const signatureErrorFixture = "{\"error\":{\"message\":\"messages.0.content.0: Invalid `signature` in `thinking` block\",\"type\":\"invalid_request_error\"},\"type\":\"error\"}"

func TestSignatureMonitorSessionIsolation(t *testing.T) {
	c, _ := newThinkingRecoveryContext(nil, "private-session", 7)
	id := signatureMonitorSession(c, nil)
	require.True(t, strings.HasPrefix(id, "session:"))
	require.NotContains(t, id, "private-session")
	require.Equal(t, id, signatureMonitorSession(c, nil))
	c.Set("api_key", &APIKey{ID: 8, UserID: 4})
	require.NotEqual(t, id, signatureMonitorSession(c, nil))
	c.Set("api_key", &APIKey{ID: 7, UserID: 5})
	require.NotEqual(t, id, signatureMonitorSession(c, nil))
	c.Request.Header.Del("X-Claude-Code-Session-Id")
	require.Empty(t, signatureMonitorSession(c, nil))
}

func TestSignatureMonitorPassthroughProvenanceAndNoSecretEmission(t *testing.T) {
	for _, tc := range []struct{ name, input, wire, want string }{
		{"unchanged", "private original", "private original", "signature_matches_previous"},
		{"client_or_pre_forward", "changed", "changed", "signature_changed_before_forward"},
		{"gateway_forward", "private original", "changed", "signature_changed_in_forward"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &GatewayService{}
			account := newAnthropicAPIKeyAccountForTest()
			c, _ := newThinkingRecoveryContext(nil, "private-session", 7)
			first := svc.beginClaudeThinkingTrace(c, account, "claude-opus-5-5", "", nil)
			require.NotNil(t, first)
			first.observeJSON([]byte(`{"content":[{"type":"thinking","thinking":"private original","signature":"secret-signature"}]}`))
			first.commit()
			input := thinkingTraceFixture(tc.input, "secret-signature")
			wire := thinkingTraceFixture(tc.wire, "secret-signature")
			originalInput := string(input)
			originalWire := string(wire)
			trace := svc.beginClaudeThinkingTrace(c, account, "claude-opus-5-5", "", input)
			hub := telemetry.New("test", "test", "test", 10, 100)
			ctx, tr := hub.Start(context.Background(), "r1", time.Now())
			tr.Update(domain.Observation{UserID: 4, KeyID: 7, AccountID: account.ID})
			c.Request = c.Request.WithContext(ctx)
			monitorClaudeSignatureRejection(c, account.ID, signatureMonitorSession(c, input), claudeThinkingBlockDigests(input), trace, wire, []byte(signatureErrorFixture))
			snap := hub.Snapshot(0, time.Now())
			require.Len(t, snap.Events, 2)
			event := snap.Events[1]
			require.NoError(t, event.Validate())
			require.Equal(t, float64(1), event.N(tc.want))
			require.Equal(t, "signature.rejected", event.Kind)
			b, _ := json.Marshal(snap)
			for _, forbidden := range []string{"private original", "secret-signature", "private-session"} {
				require.NotContains(t, string(b), forbidden)
			}
			require.Equal(t, originalInput, string(input), "diagnostics must not mutate input")
			require.Equal(t, originalWire, string(wire), "diagnostics must not mutate wire")
			require.Zero(t, svc.claudeThinkingRecovery.order.Len(), "diagnostics must not seed repair state")
		})
	}
}

func TestSignaturePassthroughForwardPreservesHistoryAndFailure(t *testing.T) {
	body := []byte(`{"model":"claude-opus-5-5","max_tokens":1024,"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"private original","signature":"secret-signature"}]}]}`)
	upstream := &anthropicHTTPUpstreamRecorder{resp: &http.Response{StatusCode: 400, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(signatureErrorFixture))}}
	cfg := &config.Config{}
	svc := &GatewayService{cfg: cfg, httpUpstream: upstream, responseHeaderFilter: compileResponseHeaderFilter(cfg)}
	account := newAnthropicAPIKeyAccountForTest()
	c, rec := newThinkingRecoveryContext(body, "private-session", 7)
	hub := telemetry.New("test", "test", "test", 10, 100)
	ctx, tr := hub.Start(context.Background(), "r1", time.Now())
	tr.Update(domain.Observation{UserID: 4, KeyID: 7, AccountID: account.ID})
	c.Request = c.Request.WithContext(ctx)
	_, err := svc.Forward(ctx, c, account, &ParsedRequest{Model: "claude-opus-5-5", Body: NewRequestBodyRef(body)})
	require.Error(t, err)
	require.Equal(t, 400, rec.Code)
	require.JSONEq(t, gjson.GetBytes(body, "messages").Raw, gjson.GetBytes(upstream.lastBody, "messages").Raw)
	count := 0
	for _, o := range hub.Snapshot(0, time.Now()).Events {
		if o.Kind == "signature.rejected" {
			count++
			require.Equal(t, float64(1), o.N("signature_forward_unchanged"))
			require.NotEmpty(t, o.TaskID)
		}
	}
	require.Equal(t, 1, count, "no signature retry or duplicate diagnostic")
}

func TestSignaturePassthroughResponseHandlersRecordRoundtrip(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "json", true: "sse"}[stream], func(t *testing.T) {
			block := `{"type":"thinking","thinking":"private original","signature":"secret-signature"}`
			response := `{"id":"msg_test","type":"message","role":"assistant","content":[` + block + `],"usage":{"input_tokens":1,"output_tokens":1},"stop_reason":"end_turn"}`
			if stream {
				response = "data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":" + block + "}\n\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\ndata: {\"type\":\"message_stop\"}\n\n"
			}
			up := &anthropicHTTPUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response))}}
			cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
			svc := &GatewayService{cfg: cfg, httpUpstream: up, responseHeaderFilter: compileResponseHeaderFilter(cfg), rateLimitService: &RateLimitService{}, deferredService: &DeferredService{}}
			account := newAnthropicAPIKeyAccountForTest()
			body := []byte(`{"model":"claude-opus-5-5","messages":[{"role":"user","content":"test"}]}`)
			c, _ := newThinkingRecoveryContext(body, "private-session", 7)
			_, err := svc.Forward(context.Background(), c, account, &ParsedRequest{Model: "claude-opus-5-5", Stream: stream, Body: NewRequestBodyRef(body)})
			require.NoError(t, err)
			follow := thinkingTraceFixture("private original", "secret-signature")
			trace := svc.beginClaudeThinkingTrace(c, account, "claude-opus-5-5", "", follow)
			require.Equal(t, "matches_previous_upstream_response", trace.origin(gjson.Parse(block)))
			account.ID++
			other := svc.beginClaudeThinkingTrace(c, account, "claude-opus-5-5", "", follow)
			require.Equal(t, "not_observed_in_this_scope", other.origin(gjson.Parse(block)), "never claim provenance across upstream accounts")
		})
	}
}
