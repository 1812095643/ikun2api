package service

import (
	"encoding/json"
	"fmt"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"sync/atomic"
	"testing"
)

func TestOpenAIWSImageFirstSkipsSearchInitializer(t *testing.T) {
	for _, prewarm := range []bool{false, true} {
		for _, lite := range []bool{false, true} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("prewarm_%v/lite_%v/stream_%v", prewarm, lite, stream), func(t *testing.T) {
					f := newOpenAIWSSearchFixture(t, true, func(conn *coderws.Conn, _ int64, raw []byte) {
						if openAIWSHostedToolsDeclared(raw)&openAIWSHostedImageGeneration == 0 {
							searchFixtureEmit(conn, `{"type":"error","status":400,"error":{"type":"invalid_request_error","message":"Hosted tool 'image_generation' requires authorization and metering that are not supported by rustponsesapi."}}`)
							return
						}
						if gjson.GetBytes(raw, "generate").Type == gjson.False {
							searchFixtureEmit(conn, `{"type":"response.completed","response":{"id":"resp_image_prewarm","status":"completed","output":[]}}`)
							return
						}
						searchFixtureEmit(conn, `{"type":"response.completed","response":{"id":"resp_image","model":"gpt-5.5","status":"completed","output":[{"type":"image_generation_call","id":"ig_original","status":"completed","result":"fixture-image-bytes"}],"usage":{"input_tokens":2,"output_tokens":1}}}`)
					})
					f.cfg.Gateway.OpenAIWS.PrewarmGenerateEnabled = prewarm
					tools := `"tools":[{"type":"image_generation","quality":"high","output_format":"png"}],"input":[{"role":"user","content":"draw original picture"}]`
					if lite {
						tools = `"input":[{"type":"additional_tools","role":"developer","tools":[{"type":"image_generation","quality":"high","output_format":"png"}]},{"role":"user","content":"draw original picture"}]`
					}
					result, err, rec := f.forward(t, fmt.Sprintf(`{"model":"gpt-5.5","stream":%v,%s}`, stream, tools))
					require.NoError(t, err)
					require.Equal(t, "resp_image", result.ResponseID)
					require.Contains(t, rec.Body.String(), "fixture-image-bytes")
					frames := f.frames()
					require.Len(t, frames, 1, "tool requests keep the existing prewarm skip rule")
					require.False(t, openAIWSWebSearchDeclared(frames[0].body))
					require.Contains(t, string(frames[0].body), "high")
					require.Contains(t, string(frames[0].body), "png")
				})
			}
		}
	}
}
func TestOpenAIWSImageActivationReusesCompatibleConnection(t *testing.T) {
	var n atomic.Int64
	f := newOpenAIWSSearchFixture(t, true, func(conn *coderws.Conn, _ int64, raw []byte) {
		if gjson.GetBytes(raw, "generate").Type == gjson.False {
			searchFixtureEmit(conn, `{"type":"response.completed","response":{"id":"resp_init","status":"completed","output":[]}}`)
			return
		}
		searchFixtureEmit(conn, fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_%d","status":"completed","output":[]}}`, n.Add(1)))
	})
	_, err, _ := f.forward(t, `{"model":"gpt-5.5","input":"first normal request"}`)
	require.NoError(t, err)
	_, err, _ = f.forward(t, `{"model":"gpt-5.5","input":"full picture request","tools":[{"type":"web_search"},{"type":"image_generation","quality":"high"}]}`)
	require.NoError(t, err)
	_, err, _ = f.forward(t, `{"model":"gpt-5.5","previous_response_id":"resp_2","input":"next picture","tools":[{"type":"image_generation","quality":"high"}]}`)
	require.NoError(t, err)
	frames := f.frames()
	require.Len(t, frames, 4)
	require.NotEqual(t, frames[1].conn, frames[2].conn)
	require.Equal(t, frames[2].conn, frames[3].conn)
	require.Equal(t, "resp_2", gjson.GetBytes(frames[3].body, "previous_response_id").String())
}

func TestOpenAIWSImageActivationHTTPPreservesCompleteHistory(t *testing.T) {
	var turn atomic.Int64
	f := newOpenAIWSSearchFixture(t, true, func(conn *coderws.Conn, _ int64, raw []byte) {
		if gjson.GetBytes(raw, "generate").Type == gjson.False {
			searchFixtureEmit(conn, `{"type":"response.completed","response":{"id":"resp_init","status":"completed","output":[]}}`)
			return
		}
		if turn.Add(1) == 1 {
			searchFixtureEmit(conn, `{"type":"response.completed","response":{"id":"resp_before_image","status":"completed","output":[{"type":"message","id":"msg_real","role":"assistant","content":[{"type":"output_text","text":"real earlier answer"}]},{"type":"function_call","id":"fc_read","call_id":"call_read","name":"read","arguments":"{}"}]}}`)
			return
		}
		require.False(t, gjson.GetBytes(raw, "previous_response_id").Exists())
		for _, text := range []string{"original question", "real earlier answer", "actual result"} {
			require.Contains(t, string(raw), text)
		}
		require.Equal(t, "high", gjson.GetBytes(raw, "tools.0.quality").String())
		searchFixtureEmit(conn, `{"type":"response.completed","response":{"id":"resp_after_image","status":"completed","output":[{"type":"image_generation_call","id":"ig_result","status":"completed","result":"fixture-png"}]}}`)
	})
	_, err, _ := f.forward(t, `{"model":"gpt-5.5","input":[{"role":"user","content":"original question"}],"tools":[{"type":"function","name":"read","parameters":{"type":"object"}}]}`)
	require.NoError(t, err)
	result, err, rec := f.forward(t, `{"model":"gpt-5.5","previous_response_id":"resp_before_image","input":[{"type":"function_call_output","call_id":"call_read","output":"actual result"}],"tools":[{"type":"image_generation","quality":"high"}]}`)
	require.NoError(t, err)
	require.Equal(t, "resp_after_image", result.ResponseID)
	require.Contains(t, rec.Body.String(), "fixture-png")
	frames := f.frames()
	require.Len(t, frames, 3)
	require.NotEqual(t, frames[1].conn, frames[2].conn)
}
func TestOpenAIWSHostedReplayScopeAndWindowIsolation(t *testing.T) {
	f := newOpenAIWSSearchFixture(t, true, func(*coderws.Conn, int64, []byte) {})
	payload := map[string]any{"input": []any{map[string]any{"role": "user", "content": "private thread"}}, "client_metadata": map[string]any{"x-codex-window-id": "window-a"}}
	f.svc.saveOpenAIWSHostedReplay(f.account, "key1:thread1", "resp_one", openAIWSPayloadCodexWindowID(payloadAsJSONBytes(payload)), []json.RawMessage{json.RawMessage(`{"role":"user","content":"private thread"}`)})
	for _, tc := range []struct {
		name, scope, anchor, window string
		account                     int64
		complete                    bool
	}{
		{"same", "key1:thread1", "resp_one", "window-a", 707, true},
		{"other-key", "key2:thread1", "resp_one", "window-a", 707, false},
		{"other-thread", "key1:thread2", "resp_one", "window-a", 707, false},
		{"other-account", "key1:thread1", "resp_one", "window-a", 708, false},
		{"other-anchor", "key1:thread1", "resp_other", "window-a", 707, false},
		{"other-window", "key1:thread1", "resp_one", "window-b", 707, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := *f.account
			account.ID = tc.account
			p := map[string]any{"previous_response_id": tc.anchor, "input": []any{}, "client_metadata": map[string]any{"x-codex-window-id": tc.window}}
			input, complete := f.svc.prepareOpenAIWSHostedReplay(&account, tc.scope, p, tc.window)
			require.Equal(t, tc.complete, complete)
			if !complete {
				require.Empty(t, input)
			}
		})
	}
}
