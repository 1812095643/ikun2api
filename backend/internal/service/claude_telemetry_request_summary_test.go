package service

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func TestClaudeTelemetryRequestSummaryUsesOnlyObservedCounts(t *testing.T) {
	wire, err := sjson.SetRawBytes(telemetryTestWire(), "tools", []byte(`[{"name":"PRIVATE_TOOL","ownership_metadata":{"server_url":"PRIVATE_URL"}},{"name":"other"}]`))
	require.NoError(t, err)
	wire, err = sjson.SetRawBytes(wire, "messages", []byte(`[
        {"role":"user","content":"PRIVATE_PROMPT with tool_use words"},
        {"role":"assistant","content":[{"type":"thinking","thinking":"PRIVATE_CODE","signature":"PRIVATE_SIGNATURE"},{"type":"tool_use","name":"PRIVATE_TOOL","input":{"code":"PRIVATE_CODE"}},{"type":"server_tool_use","name":"PRIVATE_SERVER_TOOL"}]},
        {"role":"user","content":[{"type":"tool_result","content":"PRIVATE_RESULT","is_error":true},{"type":"tool_result","content":[{"type":"tool_use","name":"BUSINESS_JSON"}]},{"type":"tool_addition","tool":{"definition":{"name":"PRIVATE_TOOL","ownership_metadata":{"server_url":"PRIVATE_URL"}}}},{"type":"text","text":"PRIVATE_TEXT"}]}
    ]`))
	require.NoError(t, err)
	wire, err = sjson.SetBytes(wire, "stream", true)
	require.NoError(t, err)
	wire, err = sjson.SetRawBytes(wire, "gatewayRequestSummary", []byte(`{"historyToolCalls":999999,"scriptCheck":"PRIVATE_CLAIM","optedIn":true}`))
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", nil)
	require.NoError(t, err)
	req.Header.Set("x-claude-code-sandy-plover", `[{"url":"PRIVATE_URL","directory_entry":"PRIVATE_ID"}]`)
	for _, tc := range []struct {
		name    string
		result  *ForwardResult
		err     error
		status  int
		outcome string
	}{
		{"success", &ForwardResult{}, nil, 200, "success"},
		{"rate_limit", nil, fmt.Errorf("PRIVATE_ERROR"), 429, "rate_limited"},
		{"failure", nil, fmt.Errorf("PRIVATE_ERROR"), 400, "upstream_error"},
		{"transport", nil, fmt.Errorf("PRIVATE_ERROR"), 0, "transport_error"},
		{"disconnect", &ForwardResult{ClientDisconnect: true}, nil, 200, "client_disconnect"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, _, err := buildClaudeTelemetryBody(req, wire, &Account{}, tc.result, tc.err, time.Now().Add(-1500*time.Millisecond), tc.status, 2)
			require.NoError(t, err)
			data, err := base64.StdEncoding.DecodeString(gjson.GetBytes(body, "events.0.event_data.additional_metadata").String())
			require.NoError(t, err)
			require.NotContains(t, string(data)+string(body), "PRIVATE")
			require.Equal(t, tc.outcome, gjson.GetBytes(data, "outcome").String())
			require.JSONEq(t, `{"toolDefinitions":2,"inlineToolDefinitions":1,"historyToolCalls":2,"historyToolResults":2,"historyToolErrors":1,"stream":true}`, gjson.GetBytes(data, "gatewayRequestSummary").Raw)
			require.GreaterOrEqual(t, gjson.GetBytes(data, "durationMs").Int(), int64(1500))
			for _, name := range []string{"optedIn", "scriptCheck", "permissionMode", "innerExecutedCount", "codeChars", "git_remote", "rh", "cc_prompt_id"} {
				require.False(t, gjson.GetBytes(data, name).Exists(), name)
			}
			require.False(t, gjson.GetBytes(data, "source").Exists())
			require.Equal(t, "cli", gjson.GetBytes(body, "events.0.event_data.client_type").String())
		})
	}
}

func TestClaudeTelemetryRequestSummaryDoesNotInferClientExecution(t *testing.T) {
	// A request history is a snapshot: repeated IDs and copies are not claimed
	// as newly executed tools, and a nested JSON result is never inspected.
	summary := buildClaudeTelemetryRequestSummary([]byte(`{"tools":{},"stream":"true","messages":[{"role":"user","content":[{"type":"tool_use","name":"not_an_assistant_call"}]},{"role":"assistant","content":[{"type":"tool_result","is_error":true}]},{"role":"user","content":[{"type":"tool_result","is_error":"true","content":[{"type":"tool_addition","tool":{"definition":{}}}]}]}]}`))
	require.Equal(t, claudeTelemetryRequestSummary{HistoryToolResults: 1}, summary)
	require.Equal(t, claudeTelemetryRequestSummary{}, buildClaudeTelemetryRequestSummary([]byte(`{"messages":"PRIVATE"}`)))
}

func TestClaudeTelemetryCodeModeEventsCannotClaimOrGenerateActivity(t *testing.T) {
	t.Setenv("SUB6_CLAUDE_TELEMETRY_ENABLED", "true")
	for _, name := range []string{"tengu_tools_used_header_size", "tengu_repl_mode_opt_in", "tengu_repl_mode_opt_in_dialog_accept", "tengu_repl_script_end", "tengu_repl_inner_executing", "tengu_repl_script_call_without_classifier", "tengu_cloud_move_handoff"} {
		t.Run(name, func(t *testing.T) {
			transport := &localClaudeTelemetryTransport{}
			gateway := &GatewayService{httpUpstream: transport}
			expected := telemetryBatchDelivery(t, true)
			gateway.claudeTelemetry.remember(17, telemetryClientSession, expected, time.Now())
			batch := telemetryTestBatch(t, name)
			require.Empty(t, batch.triggers)
			require.Zero(t, gateway.RelayClaudeTelemetry(17, batch))
			require.Zero(t, transport.calls)
			trigger := telemetryTestBatch(t, "tengu_api_success").triggers[0]
			delivery, ok := gateway.claudeTelemetry.claim(17, trigger, time.Now())
			require.True(t, ok)
			require.Equal(t, expected.body, delivery.body)
			require.False(t, strings.Contains(string(delivery.body), name))
		})
	}
}
