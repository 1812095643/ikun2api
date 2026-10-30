package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIOAuthImageSearchCompatibility(t *testing.T) {
	for _, test := range []struct {
		name    string
		body    string
		want    string
		changed bool
	}{
		{"mixed", `{"tools":[{"type":"web_search","search_content_types":["image","text","image"],"image_settings":{"max_results":3}}]}`, `{"tools":[{"type":"web_search","search_content_types":["text"]}]}`, true},
		{"image_only", `{"tools":[{"type":"web_search","search_content_types":["image"]}]}`, `{"tools":[{"type":"web_search","search_content_types":["text"]}]}`, true},
		{"versioned_image_only", `{"tools":[{"type":"web_search_2025_08_26","search_content_types":["image"],"image_settings":{"max_results":3},"search_context_size":"high"}]}`, `{"tools":[{"type":"web_search_2025_08_26","search_content_types":["text"],"search_context_size":"high"}]}`, true},
		{"lite_tools", `{"input":[{"type":"additional_tools","tools":[{"type":"web_search_2025_08_26","search_content_types":["image"],"image_settings":{}}]}]}`, `{"input":[{"type":"additional_tools","tools":[{"type":"web_search_2025_08_26","search_content_types":["text"]}]}]}`, true},
		{"preview", `{"tools":[{"type":"web_search_preview_2025_03_11","search_content_types":["text","image"]}]}`, `{"tools":[{"type":"web_search_preview_2025_03_11","search_content_types":["text"]}]}`, true},
		{"additional_tools", `{"input":[{"type":"message","role":"user","content":"image","additional_tools":[{"type":"web_search","search_content_types":["image","text"]}]}]}`, `{"input":[{"type":"message","role":"user","content":"image","additional_tools":[{"type":"web_search","search_content_types":["text"]}]}]}`, true},
		{"text", `{"tools":[{"type":"web_search","search_content_types":["text"]}]}`, ``, false},
		{"function", `{"tools":[{"type":"function","name":"web_search","search_content_types":["image"]}]}`, ``, false},
		{"image_generation", `{"tools":[{"type":"image_generation","search_content_types":["image"],"image_settings":{}}],"input":[{"type":"function_call_output","call_id":"call_a","output":{"tools":[{"type":"web_search","search_content_types":["image"]}]}}]}`, ``, false},
		{"unrecognized_value", `{"tools":[{"type":"web_search","search_content_types":["video",1]}]}`, ``, false},
		{"invalid_array", `{"tools":[{"type":"web_search","search_content_types":"image"}]}`, ``, false},
		{"ordinary", `{"input":"find an image"}`, ``, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			want := test.want
			if !test.changed {
				want = test.body
			}
			updated, changed, err := normalizeOpenAIOAuthResponsesCompatibilityBody([]byte(test.body))
			require.NoError(t, err)
			require.Equal(t, test.changed, changed)
			require.JSONEq(t, want, string(updated))
			second, changedAgain, err := normalizeOpenAIOAuthResponsesCompatibilityBody(updated)
			require.NoError(t, err)
			require.False(t, changedAgain)
			require.Equal(t, updated, second)
			var decoded map[string]any
			require.NoError(t, json.Unmarshal([]byte(test.body), &decoded))
			require.Equal(t, test.changed, normalizeOpenAIOAuthResponsesCompatibilityFields(decoded))
			encoded, err := json.Marshal(decoded)
			require.NoError(t, err)
			require.JSONEq(t, want, string(encoded))
		})
	}
}

func TestOpenAIOAuthImageSearchKeepsAPIKeyRequests(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","input":[],"tools":[{"type":"web_search","search_content_types":["image","text"]}]}`)
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		t.Run(accountType, func(t *testing.T) {
			account := &Account{ID: 1, Platform: PlatformOpenAI, Type: accountType}
			updated, _, err := normalizeOpenAIResponsesWebSocketCompatibilityBody(body, account, false)
			require.NoError(t, err)
			if accountType == AccountTypeOAuth {
				require.Equal(t, `["text"]`, gjson.GetBytes(updated, "tools.0.search_content_types").Raw)
			} else {
				require.Equal(t, `["image","text"]`, gjson.GetBytes(updated, "tools.0.search_content_types").Raw)
			}
		})
	}
}

func TestOpenAIOAuthImageSearchPreservesRawNumbersAndTypedLists(t *testing.T) {
	body := []byte(`{"metadata":{"number":9007199254740993123},"tools":[{"type":"web_search","search_content_types":["image","text"],"extra":9007199254740993123}]}`)
	updated, changed, err := normalizeOpenAIOAuthImageSearchBody(body)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "9007199254740993123", gjson.GetBytes(updated, "metadata.number").Raw)
	require.Equal(t, "9007199254740993123", gjson.GetBytes(updated, "tools.0.extra").Raw)
	tool := map[string]any{"type": "web_search", "search_content_types": []string{"image", "text"}}
	require.True(t, normalizeOpenAIOAuthImageSearchTool(tool))
	require.Equal(t, []string{"text"}, tool["search_content_types"])
}
