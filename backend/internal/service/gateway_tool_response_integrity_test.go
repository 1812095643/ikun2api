//go:build unit

package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/MACOS-DO/sub4api/internal/config"
	"github.com/MACOS-DO/sub4api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func TestToolResponseIntegritySignedContentAndArguments(t *testing.T) {
	rw := buildToolNameRewriteFromBody([]byte(`{"tools":[{"name":"sessions_list"}]}`))
	input := []byte(`{"type":"message","id":"cc_sess_list-id","content":[{"type":"thinking","thinking":"Use cc_sess_list next","signature":"cc_sess_list-signed"},{"type":"redacted_thinking","data":"cc_sess_list-encrypted"},{"type":"text","text":"cc_sess_list in user code"},{"type":"tool_use","name":"cc_sess_list","id":"cc_sess_list-tool","input":{"name":"cc_sess_list","value":9007199254740993}}]}`)
	output := restoreToolNamesInBytes(input, rw)
	require.Equal(t, "sessions_list", gjson.GetBytes(output, "content.3.name").String())
	for _, path := range []string{"id", "content.0", "content.1", "content.2", "content.3.id", "content.3.input"} {
		require.Equal(t, gjson.GetBytes(input, path).Raw, gjson.GetBytes(output, path).Raw, path)
	}
}

func TestToolResponseIntegritySSE(t *testing.T) {
	rw := buildToolNameRewriteFromBody([]byte(`{"tools":[{"name":"sessions_list"}]}`))
	for _, event := range []string{
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"Use cc_sess_list next\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"cc_sess_list-signed\"}}\n\n",
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"cc_sess_list\"}}\r\n",
		"event: response.reasoning.delta\ndata: {\"type\":\"response.reasoning.delta\",\"delta\":\"cc_sess_list\"}\n\n",
		"data: [DONE]\n\n",
	} {
		require.Equal(t, event, string(restoreToolNamesInBytes([]byte(event), rw)))
	}
	for _, ending := range []string{"\n", "\r\n"} {
		event := strings.Join([]string{"id: cc_sess_list", ": cc_sess_list comment", "event: content_block_start", `data: {"type":"content_block_start","content_block":{"type":"tool_use","name":"cc_sess_list","id":"cc_sess_list-id","input":{}}}`, "", ""}, ending)
		want := strings.Replace(event, `"name":"cc_sess_list"`, `"name":"sessions_list"`, 1)
		require.Equal(t, want, string(restoreToolNamesInBytes([]byte(event), rw)))
	}
	// A complete SSE event can legally contain multiple data lines.
	multi := "event: content_block_start\ndata: {\"type\":\"content_block_start\",\ndata: \"content_block\":{\"type\":\"tool_use\",\"name\":\"cc_sess_list\",\"input\":{}}}\n\n"
	restored := string(restoreToolNamesInBytes([]byte(multi), rw))
	require.Contains(t, restored, `"name":"sessions_list"`)
	require.Contains(t, restored, "event: content_block_start")
}

func TestToolResponseIntegrityCompatibilityEnvelopes(t *testing.T) {
	rw := buildToolNameRewriteFromBody([]byte(`{"tools":[{"name":"sessions_list"}]}`))
	for _, tc := range []struct{ body, path string }{
		{`{"choices":[{"message":{"content":"cc_sess_list","reasoning_content":"cc_sess_list","tool_calls":[{"id":"cc_sess_list","type":"function","function":{"name":"cc_sess_list","arguments":"cc_sess_list"}}]}}]}`, "choices.0.message.tool_calls.0.function.name"},
		{`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"cc_sess_list","arguments":"cc_sess_list"}}]}}]}`, "choices.0.delta.tool_calls.0.function.name"},
		{`{"choices":[{"message":{"function_call":{"name":"cc_sess_list","arguments":"cc_sess_list"}}}]}`, "choices.0.message.function_call.name"},
		{`{"object":"response","output":[{"type":"reasoning","encrypted_content":"cc_sess_list"},{"type":"function_call","name":"cc_sess_list","arguments":"cc_sess_list","call_id":"cc_sess_list"}]}`, "output.1.name"},
		{`{"type":"response.output_item.added","item":{"type":"function_call","name":"cc_sess_list","arguments":"cc_sess_list","call_id":"cc_sess_list"}}`, "item.name"},
		{`{"type":"response.completed","response":{"output":[{"type":"function_call","name":"cc_sess_list","arguments":"cc_sess_list"}]}}`, "response.output.0.name"},
	} {
		output := restoreToolNamesInBytes([]byte(tc.body), rw)
		require.Equal(t, "sessions_list", gjson.GetBytes(output, tc.path).String(), tc.path)
		want, err := sjson.Set(tc.body, tc.path, "sessions_list")
		require.NoError(t, err)
		require.Equal(t, want, string(output))
	}
}

type signedToolRoundtripUpstream struct {
	HTTPUpstream
	calls, signatureErrors int
	bodies                 [][]byte
}

func syntheticThinkingSignature(text string) string {
	digest := sha256.Sum256([]byte(text))
	return hex.EncodeToString(digest[:])
}

func (u *signedToolRoundtripUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.calls++
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	u.bodies = append(u.bodies, body)
	response := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}, "X-Request-Id": {fmt.Sprint(u.calls)}}}
	for _, message := range gjson.GetBytes(body, "messages").Array() {
		for _, block := range message.Get("content").Array() {
			if block.Get("type").String() == "thinking" && block.Get("signature").String() != syntheticThinkingSignature(block.Get("thinking").String()) {
				u.signatureErrors++
				response.StatusCode = 400
				response.Body = io.NopCloser(strings.NewReader("{\"type\":\"error\",\"error\":{\"message\":\"messages.1.content.0: Invalid `signature` in `thinking` block\"}}"))
				return response, nil
			}
		}
	}
	thinking := "Use cc_sess_list to inspect the session"
	payload, _ := json.Marshal(map[string]any{"id": "msg_signed", "type": "message", "role": "assistant", "model": "claude-opus-5-5", "content": []any{
		map[string]any{"type": "thinking", "thinking": thinking, "signature": syntheticThinkingSignature(thinking)},
		map[string]any{"type": "tool_use", "id": "tool_roundtrip", "name": "cc_sess_list", "input": map[string]any{"marker": "cc_sess_list"}},
	}, "stop_reason": "tool_use", "usage": map[string]int{"input_tokens": 11, "output_tokens": 3}})
	response.Body = io.NopCloser(strings.NewReader(string(payload)))
	return response, nil
}

func (u *signedToolRoundtripUpstream) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, id, concurrency)
}

func TestForwardToolResponseIntegrityTwoTurnsWithoutSignatureRepair(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &signedToolRoundtripUpstream{}
	cfg := &config.Config{}
	svc := &GatewayService{cfg: cfg, responseHeaderFilter: compileResponseHeaderFilter(cfg), httpUpstream: upstream, rateLimitService: &RateLimitService{}, deferredService: &DeferredService{}, settingService: NewSettingService(&signatureBudgetSettings{}, cfg)}
	svc.identityService = newTestIdentityService(&identityCacheStub{})
	account := newAnthropicOAuthAccountForPartialUsageTest()
	account.Extra = map[string]any{"session_id_masking_enabled": true, "account_uuid": "11111111-1111-4111-8111-111111111111"}
	body := `{"model":"claude-opus-5-5","max_tokens":128,"thinking":{"type":"adaptive"},"system":[{"type":"text","text":"Git remote URL: PRIVACY_CANARY\nKeep user requirements."}],"tools":[{"name":"sessions_list","input_schema":{"type":"object"}}],"messages":[{"role":"user","content":"inspect sessions"}]}`
	for turn := 0; turn < 2; turn++ {
		c, recorder := newThinkingRecoveryContext([]byte(body), "tool-roundtrip", 7)
		c.Request.Header.Set("User-Agent", "third-party-tool-client/1.0")
		parsed, err := ParseGatewayRequest(NewRequestBodyRef([]byte(body)), PlatformAnthropic)
		require.NoError(t, err)
		_, err = svc.Forward(context.Background(), c, account, parsed)
		require.NoError(t, err)
		content := gjson.GetBytes(recorder.Body.Bytes(), "content")
		require.Equal(t, "sessions_list", content.Array()[1].Get("name").String())
		body, err = sjson.SetRaw(body, "messages.-1", `{"role":"assistant","content":`+content.Raw+`}`)
		require.NoError(t, err)
		body, err = sjson.SetRaw(body, "messages.-1", `{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool_roundtrip","content":"done"}]}`)
		require.NoError(t, err)
	}
	require.Equal(t, 2, upstream.calls, "two turns must need exactly two upstream requests")
	require.Zero(t, upstream.signatureErrors, "response rewriting must not corrupt signed thinking")
	var identity *ParsedUserID
	for _, wire := range upstream.bodies {
		require.NotContains(t, string(wire), "PRIVACY_CANARY")
		require.Equal(t, "cc_sess_list", gjson.GetBytes(wire, "tools.0.name").String())
		current := ParseMetadataUserID(gjson.GetBytes(wire, "metadata.user_id").String())
		require.NotNil(t, current)
		require.Equal(t, account.GetExtraString("account_uuid"), current.AccountUUID)
		if identity != nil {
			require.Equal(t, identity.SessionID, current.SessionID)
			require.Equal(t, identity.DeviceID, current.DeviceID)
		}
		identity = current
	}
}
