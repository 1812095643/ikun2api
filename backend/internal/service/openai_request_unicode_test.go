package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/MACOS-DO/sub4api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestTruncateOpenAIAlphaSearchPromptJSONUTF16(t *testing.T) {
	const suffix = "\n...<truncated>"
	for _, tc := range []struct {
		name, text string
		limit      int
		want       string
	}{
		{name: "empty", text: "", limit: 1, want: ""},
		{name: "ascii_at_limit", text: "abc", limit: 3, want: "abc"},
		{name: "ascii_truncated", text: "abcd", limit: 3, want: "abc" + suffix},
		{name: "chinese_at_limit", text: "中文", limit: 2, want: "中文"},
		{name: "chinese_truncated", text: "中文", limit: 1, want: "中" + suffix},
		{name: "mixed", text: "A中B", limit: 2, want: "A中" + suffix},
		{name: "emoji_at_limit", text: "🚀", limit: 2, want: "🚀"},
		{name: "emoji_fits", text: "A🚀B", limit: 3, want: "A🚀" + suffix},
		{name: "emoji_would_split", text: "A🚀B", limit: 2, want: "A" + suffix},
		{name: "no_room_for_emoji", text: "🚀B", limit: 1, want: suffix},
		{name: "astral_cjk", text: "𠀀中", limit: 2, want: "𠀀" + suffix},
		{name: "combining_code_units", text: "e\u0301中", limit: 2, want: "e\u0301" + suffix},
		{name: "control_character", text: "中\x00文", limit: 2, want: "中\x00" + suffix},
		{name: "literal_escape", text: `\u4e2d\u6587`, limit: 6, want: `\u4e2d` + suffix},
		{name: "trim", text: " \t中文🚀\n", limit: 4, want: "中文🚀"},
		{name: "zero_limit_keeps_text", text: "中文🚀", limit: 0, want: "中文🚀"},
		{name: "negative_limit_keeps_text", text: "中文🚀", limit: -1, want: "中文🚀"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateOpenAIAlphaSearchPromptJSON(tc.text, tc.limit)
			require.Equal(t, tc.want, got)
			require.True(t, utf8.ValidString(got), "never split a Unicode character")
		})
	}
}

func TestForwardAlphaSearchPATFallbackPreservesUTF16Prompt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		field, prefix, tail, label string
		limit                      int
	}{
		{field: "commands", prefix: `{"search_query":[{"q":"`, tail: `"}]}`, label: "\nCommands JSON:\n", limit: 12000},
		{field: "settings", prefix: `{"note":"`, tail: `"}`, label: "\n\nSearch settings JSON:\n", limit: 4000},
		{field: "input", prefix: `[{"role":"user","content":"`, tail: `"}]`, label: "\n\nRecent conversation/input JSON:\n", limit: 8000},
	} {
		t.Run(tc.field, func(t *testing.T) {
			// Leave one UTF-16 code unit before a surrogate pair. The whole emoji
			// must be omitted instead of splitting its encoding at the limit.
			prefix := tc.prefix + strings.Repeat("中", tc.limit-len(tc.prefix)-1)
			body, err := json.Marshal(map[string]any{
				"model":  "gpt-5.5",
				tc.field: json.RawMessage(prefix + "🚀尾" + tc.tail),
			})
			require.NoError(t, err)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/alpha/search", bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(alphaSearchResponsesSSE("ok"))),
			}}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			account := &Account{ID: 43, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
				Credentials: map[string]any{
					"access_token": "at-test-token", "chatgpt_account_id": "fixture-account",
					"auth_mode": OpenAIAuthModePersonalAccessToken,
				},
			}
			result, err := svc.ForwardAlphaSearch(context.Background(), c, account, body)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, "/v1/responses", result.UpstreamEndpoint)
			require.True(t, json.Valid(upstream.lastBody))
			require.True(t, utf8.Valid(upstream.lastBody))
			require.Equal(t, "application/json", upstream.lastReq.Header.Get("Content-Type"))
			prompt := gjson.GetBytes(upstream.lastBody, "input.0.content.0.text").String()
			require.True(t, strings.HasSuffix(prompt, tc.label+prefix+"\n...<truncated>"))
			require.NotContains(t, prompt, "\ufffd")
			require.NotContains(t, prompt, "🚀")
		})
	}
}

func TestOpenAIResponsesRequestUnicodeRoundTrip(t *testing.T) {
	text := "中文🚀𠀀e\u0301\n字面量\\u4e2d <>&"
	input := []any{
		map[string]any{"type": "message", "role": "user", "content": []any{
			map[string]any{"type": "input_text", "text": text},
		}},
		map[string]any{"type": "function_call", "call_id": "call_fixture", "name": "read_file", "arguments": `{"path":"中文🚀.txt"}`},
		map[string]any{"type": "function_call_output", "call_id": "call_fixture", "output": text},
	}
	for _, asciiEscaped := range []bool{false, true} {
		name := "literal_unicode"
		if asciiEscaped {
			name = "escaped_surrogate_pairs"
		}
		t.Run(name, func(t *testing.T) {
			body, err := marshalOpenAIUpstreamJSON(map[string]any{
				"model": "gpt-5.5", "instructions": text, "input": input,
				"store": false, "stream": true,
			})
			require.NoError(t, err)
			if asciiEscaped {
				body = escapeCodexNonASCII(body)
			}
			for _, transport := range []string{"http", "websocket"} {
				t.Run(transport, func(t *testing.T) {
					var normalized []byte
					if transport == "http" {
						normalized, _, err = normalizeOpenAIPassthroughOAuthBody(body, false)
					} else {
						account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
						normalized, _, err = normalizeOpenAIResponsesWebSocketCompatibilityBody(body, account, false)
					}
					require.NoError(t, err)
					require.True(t, utf8.Valid(normalized))
					var got map[string]any
					require.NoError(t, json.Unmarshal(normalized, &got))
					require.Equal(t, text, got["instructions"])
					require.Equal(t, input, got["input"], "preserve user text, tool arguments and output")
				})
			}
		})
	}
}
