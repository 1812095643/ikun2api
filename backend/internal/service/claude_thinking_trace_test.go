//go:build unit

package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func thinkingTraceFixture(text, signature string) []byte {
	body, _ := json.Marshal(map[string]any{"messages": []any{map[string]any{"role": "assistant", "content": []any{map[string]string{"type": "thinking", "thinking": text, "signature": signature}}}}})
	return body
}

func TestClaudeThinkingTraceClassifiesRoundtripWithoutContentRetention(t *testing.T) {
	svc := &GatewayService{}
	account := newAnthropicOAuthAccountForPartialUsageTest()
	c, _ := newThinkingRecoveryContext(nil, "trace-session", 7)
	trace := svc.beginClaudeThinkingTrace(c, account, "claude-opus-5-5", "metadata-session", nil)
	require.NotNil(t, trace)
	original := gjson.Parse(`{"type":"thinking","thinking":"private original","signature":"signed-value"}`)
	trace.observeJSON([]byte(`{"content":[` + original.Raw + `]}`))
	trace.commit()
	require.Nil(t, trace.observed)
	require.Equal(t, 2, svc.claudeThinkingObserved.order.Len(), "only two fixed-size digests")

	fresh := svc.beginClaudeThinkingTrace(c, account, "claude-opus-5-5", "metadata-session", thinkingTraceFixture("private original", "signed-value"))
	require.Equal(t, "matches_previous_upstream_response", fresh.origin(original))
	changed := gjson.Parse(`{"type":"thinking","thinking":"changed text","signature":"signed-value"}`)
	require.Equal(t, "changed_during_request_forwarding", fresh.origin(changed))
	before := svc.beginClaudeThinkingTrace(c, account, "claude-opus-5-5", "metadata-session", thinkingTraceFixture("changed text", "signed-value"))
	require.Equal(t, "changed_before_request_forwarding", before.origin(changed))

	other, _ := newThinkingRecoveryContext(nil, "other-session", 7)
	isolated := svc.beginClaudeThinkingTrace(other, account, "claude-opus-5-5", "metadata-session", thinkingTraceFixture("changed text", "signed-value"))
	require.Equal(t, "not_observed_in_this_scope", isolated.origin(changed))
	unknown := gjson.Parse(`{"type":"thinking","thinking":"hello","signature":"never-seen"}`)
	require.Equal(t, "not_observed_in_this_scope", fresh.origin(unknown))
	require.Empty(t, svc.claudeThinkingObserved.known(trace.scope, time.Now().Add(claudeThinkingRecoveryTTL)))
}

func TestClaudeThinkingTraceSSEUsesCompleteSignatureValue(t *testing.T) {
	svc := &GatewayService{}
	account := newAnthropicOAuthAccountForPartialUsageTest()
	c, _ := newThinkingRecoveryContext(nil, "trace-session", 7)
	trace := svc.beginClaudeThinkingTrace(c, account, "claude-opus-5-5", "metadata-session", nil)
	for _, event := range []string{
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"hello "}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"世界"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"intermediate"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"complete"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"thinking","thinking":"incomplete","signature":""}}`,
	} {
		trace.observeEvent([]byte(event))
	}
	trace.commit()
	require.Equal(t, 2, svc.claudeThinkingObserved.order.Len())
	final := svc.beginClaudeThinkingTrace(c, account, "claude-opus-5-5", "metadata-session", thinkingTraceFixture("hello 世界", "complete"))
	require.Equal(t, "matches_previous_upstream_response", final.origin(gjson.Parse(`{"type":"thinking","thinking":"hello 世界","signature":"complete"}`)))
	require.Equal(t, "not_observed_in_this_scope", final.origin(gjson.Parse(`{"type":"thinking","thinking":"hello 世界","signature":"intermediatecomplete"}`)))
}

func TestClaudeThinkingTraceBoundariesAndMissingContext(t *testing.T) {
	svc := &GatewayService{}
	account := newAnthropicOAuthAccountForPartialUsageTest()
	c, _ := newThinkingRecoveryContext(nil, "trace-session", 7)
	trace := svc.beginClaudeThinkingTrace(c, account, "claude-opus-5-5", "metadata-session", nil)
	require.NotNil(t, trace)
	account.Type = AccountTypeAPIKey
	require.Nil(t, svc.beginClaudeThinkingTrace(c, account, "claude-opus-5-5", "metadata-session", nil))
	require.Nil(t, claudeThinkingTraceFromContext(c), "failover must not inherit a previous trace")
	require.Nil(t, svc.beginClaudeThinkingTrace(nil, account, "claude-opus-5-5", "", nil))
	var missing *claudeThinkingTrace
	missing.observeJSON(nil)
	missing.observeEvent(nil)
	missing.commit()
	require.Equal(t, "unavailable", missing.origin(gjson.Result{}))
}
