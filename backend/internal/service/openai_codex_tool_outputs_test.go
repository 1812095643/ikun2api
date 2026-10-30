package service

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func toolOutputItems(t *testing.T, raw string) []json.RawMessage {
	t.Helper()
	var items []json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(raw), &items))
	return items
}

func TestOpenAICodexToolOutputsCompleteMissingPairs(t *testing.T) {
	for _, test := range []struct {
		name  string
		input string
		added int
	}{
		{"function", `[{"type":"function_call","call_id":"call_a","name":"read","arguments":"{}"},{"role":"user","content":"continue"}]`, 1},
		{"real_output", `[{"type":"function_call","call_id":"call_a"},{"role":"user","content":"later"},{"type":"function_call_output","call_id":"call_a","output":"actual result"}]`, 0},
		{"empty_real_output", `[{"type":"function_call","call_id":"call_a"},{"type":"function_call_output","call_id":"call_a","output":""}]`, 0},
		{"custom_and_shell", `[{"type":"custom_tool_call","call_id":"custom_a","input":"patch"},{"type":"local_shell_call","call_id":"shell_a","action":{"type":"exec"}}]`, 2},
		{"typed_pairing", `[{"type":"function_call","call_id":"same"},{"type":"custom_tool_call","call_id":"same"},{"type":"function_call_output","call_id":"same","output":"real"}]`, 1},
		{"no_duplicate_marker", `[{"type":"function_call","call_id":"same"},{"type":"function_call","call_id":"same"}]`, 1},
		{"server_and_unknown_tools", `[{"type":"web_search_call","call_id":"a"},{"type":"tool_search_call","call_id":"b","execution":"server"},{"type":"mcp_tool_call","call_id":"c"},{"type":"future_tool_call","call_id":"d"}]`, 0},
		{"invalid_ids", `[{"type":"function_call"},{"type":"function_call","call_id":" "},{"type":"function_call","call_id":123}]`, 0},
		{"existing_orphan_output", `[{"type":"function_call_output","call_id":"preserve","output":"real"}]`, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			items := toolOutputItems(t, test.input)
			original, err := json.Marshal(items)
			require.NoError(t, err)
			repaired, added := completeOpenAICodexToolOutputs(items, nil)
			require.Equal(t, test.added, added)
			require.Len(t, repaired, len(items)+added)
			after, err := json.Marshal(items)
			require.NoError(t, err)
			require.Equal(t, original, after, "不能修改原数组或原始调用正文")
			second, repeated := completeOpenAICodexToolOutputs(repaired, nil)
			require.Zero(t, repeated, "重复处理不能再次补写")
			require.Equal(t, repaired, second)
			if added == 0 {
				require.Equal(t, items, repaired)
			}
			if test.name == "function" {
				require.JSONEq(t, `{"type":"function_call_output","call_id":"call_a","output":"aborted"}`, string(repaired[1]))
				require.Equal(t, items[1], repaired[2])
			}
			if test.name == "custom_and_shell" {
				require.Equal(t, "custom_tool_call_output", gjson.GetBytes(repaired[1], "type").String())
				require.Equal(t, "function_call_output", gjson.GetBytes(repaired[3], "type").String())
			}
		})
	}
}

func TestOpenAICodexToolOutputsKnownContinuationKeepsRealResults(t *testing.T) {
	prior := toolOutputItems(t, `[{"role":"user","content":"read"},{"type":"function_call","call_id":"call_a"},{"type":"function_call","call_id":"call_b"}]`)
	current := toolOutputItems(t, `[{"type":"function_call_output","call_id":"call_a","output":"actual"},{"role":"user","content":"continue"}]`)
	repaired, added := completeOpenAICodexToolOutputs(current, prior)
	require.Equal(t, 1, added)
	require.JSONEq(t, `{"type":"function_call_output","call_id":"call_b","output":"aborted"}`, string(repaired[0]))
	require.Equal(t, current, repaired[1:])
	second, repeated := completeOpenAICodexToolOutputs(repaired, prior)
	require.Zero(t, repeated)
	require.Equal(t, repaired, second)
}

func TestOpenAICodexToolOutputsRepairRepeatedContextAtCallPosition(t *testing.T) {
	prior := toolOutputItems(t, `[{"type":"function_call","call_id":"call_a"}]`)
	current := toolOutputItems(t, `[{"role":"user","content":"history"},{"type":"function_call","call_id":"call_a"},{"role":"user","content":"next"}]`)
	repaired, added := completeOpenAICodexToolOutputs(current, prior)
	require.Equal(t, 1, added)
	require.Equal(t, current[:2], repaired[:2])
	require.Equal(t, "aborted", gjson.GetBytes(repaired[2], "output").String())
	require.Equal(t, current[2], repaired[3])
}

func TestOpenAICodexToolOutputsRequestPreservesNumbersAndOpaqueHistory(t *testing.T) {
	raw := []byte(`{"input":[{"type":"function_call","call_id":"call_a","arguments":"{}","extra":9007199254740993123}],"metadata":{"large":9007199254740993123}}`)
	updated, err := completeOpenAICodexRequestTools(raw)
	require.NoError(t, err)
	require.Equal(t, "9007199254740993123", gjson.GetBytes(updated, "input.0.extra").Raw)
	require.Equal(t, "9007199254740993123", gjson.GetBytes(updated, "metadata.large").Raw)
	require.Equal(t, "aborted", gjson.GetBytes(updated, "input.1.output").String())
	for _, body := range []string{
		`{"previous_response_id":"resp_unknown","input":[{"type":"function_call_output","call_id":"call_a","output":"actual"}]}`,
		`{"previous_response_id":"resp_unknown","input":[{"role":"user","content":"continue"}]}`,
		`{"previous_response_id":"resp_unknown"}`,
		`{"input":"ordinary text"}`,
		`{"model":"local-model"}`,
		`not-json`,
	} {
		updated, err := completeOpenAICodexRequestTools([]byte(body))
		require.NoError(t, err)
		require.True(t, bytes.Equal([]byte(body), updated), "未知锚点或非数组输入不能靠猜测补结果")
	}
}

func TestOpenAICodexToolOutputsUnknownAnchorRepairsVisibleCalls(t *testing.T) {
	for _, kind := range []string{"function_call", "custom_tool_call", "local_shell_call"} {
		t.Run(kind, func(t *testing.T) {
			raw := []byte(`{"previous_response_id":"resp_unknown","metadata":{"large":9007199254740993123},"input":[{"type":"` + kind + `","call_id":"call_missing","input":"patch"},{"type":"custom_tool_call","call_id":"call_real","input":"read"},{"type":"custom_tool_call_output","call_id":"call_real","output":""},{"role":"user","content":"continue"}]}`)
			updated, err := completeOpenAICodexRequestTools(raw)
			require.NoError(t, err)
			require.Equal(t, "resp_unknown", gjson.GetBytes(updated, "previous_response_id").String())
			require.Equal(t, "9007199254740993123", gjson.GetBytes(updated, "metadata.large").Raw)
			wantType := "function_call_output"
			if kind == "custom_tool_call" {
				wantType = "custom_tool_call_output"
			}
			require.Equal(t, wantType, gjson.GetBytes(updated, "input.1.type").String())
			require.Equal(t, "call_missing", gjson.GetBytes(updated, "input.1.call_id").String())
			require.Equal(t, "aborted", gjson.GetBytes(updated, "input.1.output").String())
			require.Equal(t, gjson.GetBytes(raw, "input.2").Raw, gjson.GetBytes(updated, "input.3").Raw)
			again, err := completeOpenAICodexRequestTools(updated)
			require.NoError(t, err)
			require.Equal(t, updated, again)

			var fields map[string]any
			require.NoError(t, decodeOpenAIJSONUseNumber(raw, &fields))
			changed, err := completeOpenAICodexRequestToolFields(fields)
			require.NoError(t, err)
			require.True(t, changed)
			encoded, err := marshalOpenAIUpstreamJSON(fields)
			require.NoError(t, err)
			require.JSONEq(t, string(updated), string(encoded))
			changed, err = completeOpenAICodexRequestToolFields(fields)
			require.NoError(t, err)
			require.False(t, changed)
		})
	}
}

func TestOpenAICodexToolOutputsMismatchedAnchorUsesOnlyVisibleHistory(t *testing.T) {
	prior := toolOutputItems(t, `[{"type":"custom_tool_call","call_id":"call_hidden"},{"type":"custom_tool_call_output","call_id":"call_visible","output":"other session"}]`)
	for _, expected := range []string{"", "resp_other"} {
		t.Run("anchor_"+expected, func(t *testing.T) {
			raw := []byte(`{"previous_response_id":"resp_unknown","input":[{"type":"custom_tool_call","call_id":"call_visible","input":"patch"},{"type":"custom_tool_call_output","call_id":"call_actual","output":"actual"}]}`)
			updated, changed, err := completeOpenAICodexContinuationTools(raw, prior, expected)
			require.NoError(t, err)
			require.True(t, changed)
			require.Equal(t, "resp_unknown", gjson.GetBytes(updated, "previous_response_id").String())
			require.Equal(t, "call_visible", gjson.GetBytes(updated, "input.1.call_id").String())
			require.Equal(t, "aborted", gjson.GetBytes(updated, "input.1.output").String())
			require.Equal(t, "actual", gjson.GetBytes(updated, "input.2.output").String())
			require.NotContains(t, string(updated), "call_hidden")
			require.NotContains(t, string(updated), "other session")
		})
	}
}

func TestOpenAICodexToolOutputsContinuationAnchorGuard(t *testing.T) {
	prior := toolOutputItems(t, `[{"type":"function_call","call_id":"call_a"},{"type":"function_call","call_id":"call_b"}]`)
	for _, expected := range []string{"resp_prior", "", "resp_other"} {
		t.Run("anchor_"+expected, func(t *testing.T) {
			raw := []byte(`{"previous_response_id":"resp_prior","input":[{"type":"function_call_output","call_id":"call_a","output":"actual"}]}`)
			updated, changed, err := completeOpenAICodexContinuationTools(raw, prior, expected)
			require.NoError(t, err)
			if expected != "resp_prior" {
				require.False(t, changed)
				require.Equal(t, raw, updated)
				return
			}
			require.True(t, changed)
			require.Equal(t, "call_b", gjson.GetBytes(updated, "input.0.call_id").String())
			require.Equal(t, "aborted", gjson.GetBytes(updated, "input.0.output").String())
			require.Equal(t, "actual", gjson.GetBytes(updated, "input.1.output").String())
			second, again, err := completeOpenAICodexContinuationTools(updated, prior, expected)
			require.NoError(t, err)
			require.False(t, again)
			require.Equal(t, updated, second)
		})
	}
}
