package handler

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestCodexContextMixedWithBothBootstraps(t *testing.T) {
	items := []any{map[string]any{"role": "user", "content": "继续"}}
	for _, name := range codexTextContextNamesForTest {
		items = append(items, pageContextItem(name, "context"))
	}
	items = append(items, map[string]any{"type": "function_call_output", "namespace": "codex_app", "name": "create_thread", "output": delegationEnvelope},
		map[string]any{"type": "function_call_output", "namespace": "codex_app", "name": "automation_update", "output": "<heartbeat><automation_id>test</automation_id></heartbeat>"})
	body, _ := normalizeCodexUntrustedContext(pageContextRequest(t, items...))
	body, _ = normalizeCodexAutomationBootstrap(body)
	body, _ = normalizeCodexDelegationBootstrap(body)
	for _, item := range gjson.GetBytes(body, "input").Array() {
		require.Equal(t, "user", item.Get("role").String())
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	require.True(t, (&OpenAIGatewayHandler{}).validateFunctionCallOutputRequest(c, body, zap.NewNop()))
}

func TestInvalidCodexContextReportsIndexWithoutOutputContent(t *testing.T) {
	for _, output := range []any{"private text", []any{map[string]any{"type": "encrypted_content", "encrypted_content": "private ciphertext"}}} {
		body := pageContextRequest(t, map[string]any{"role": "user", "content": "continue"}, pageContextItem("browser_info", "context"),
			map[string]any{"type": "function_call_output", "namespace": "agent", "name": "new_context_name", "output": output})
		body, _ = normalizeCodexUntrustedContext(body)
		core, logs := observer.New(zap.WarnLevel)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		require.False(t, (&OpenAIGatewayHandler{}).validateFunctionCallOutputRequest(c, body, zap.New(core)))
		require.Equal(t, 400, w.Code)
		require.Contains(t, w.Body.String(), "input[2]")
		require.NotContains(t, w.Body.String(), "only supported on Responses WebSocket")
		fields := logs.All()[0].ContextMap()
		require.EqualValues(t, 2, fields["input_index"])
		require.Equal(t, "agent", fields["namespace"])
		require.Equal(t, "new_context_name", fields["name"])
		encoded, err := json.Marshal(fields)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), "private text")
		require.NotContains(t, string(encoded), "private ciphertext")
		diagnostics := invalidResponsesToolOutputs(body, true)
		require.Len(t, diagnostics, 1)
		if _, ok := output.(string); ok {
			require.Equal(t, "string", diagnostics[0].OutputKind)
		} else {
			require.Equal(t, "encrypted", diagnostics[0].OutputKind)
		}
	}
}

func TestMissingToolReferenceReportsUnmatchedOutput(t *testing.T) {
	body := []byte(`{"model":"gpt-5","input":[{"type":"item_reference","id":"call_valid"},{"type":"function_call_output","call_id":"call_valid","output":"ok"},{"type":"function_call_output","call_id":"call_missing","output":"result"}]}`)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	require.False(t, (&OpenAIGatewayHandler{}).validateFunctionCallOutputRequest(c, body, zap.NewNop()))
	require.Contains(t, w.Body.String(), "input[2]")
	require.Contains(t, w.Body.String(), "valid previous_response_id")
}
