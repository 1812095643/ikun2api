package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/MACOS-DO/sub4api/internal/config"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const requestBodyUnicodeFixture = "中文🚀𠀀e\u0301\n字面量\\u4e2d <>&\u2028尾"

func requestBodyEncodingFixture(t *testing.T, anthropic, stream, escaped bool) []byte {
	t.Helper()
	text := requestBodyUnicodeFixture
	var payload map[string]any
	if anthropic {
		payload = map[string]any{
			"model": "claude-sonnet-4-6", "max_tokens": 256, "stream": stream,
			"system": []any{map[string]any{"type": "text", "text": text}},
			"tools":  []any{map[string]any{"name": "read_file", "description": text, "input_schema": map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string", "description": text}}}}},
			"messages": []any{
				map[string]any{"role": "user", "content": text},
				map[string]any{"role": "assistant", "content": []any{
					map[string]any{"type": "thinking", "thinking": text, "signature": "opaque-fixture-signature"},
					map[string]any{"type": "tool_use", "id": "toolu_fixture", "name": "read_file", "input": map[string]any{"path": text}},
				}},
				map[string]any{"role": "user", "content": []any{
					map[string]any{"type": "tool_result", "tool_use_id": "toolu_fixture", "content": text},
					map[string]any{"type": "text", "text": text},
				}},
			},
		}
	} else {
		arguments, err := json.Marshal(map[string]any{"path": text})
		require.NoError(t, err)
		payload = map[string]any{
			"model": "gpt-5.5", "stream": stream, "store": false, "instructions": text,
			"tools": []any{map[string]any{"type": "function", "name": "read_file", "description": text, "parameters": map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string", "description": text}}}}},
			"input": []any{
				map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": text}}},
				map[string]any{"type": "function_call", "call_id": "call_fixture", "name": "read_file", "arguments": string(arguments)},
				map[string]any{"type": "function_call_output", "call_id": "call_fixture", "output": text},
			},
		}
	}
	body, err := marshalOpenAIUpstreamJSON(payload)
	require.NoError(t, err)
	if escaped {
		body = escapeCodexNonASCII(body)
	}
	return body
}

func assertRequestBodyUnicodeFields(t *testing.T, original, got []byte, anthropic bool) {
	t.Helper()
	require.True(t, utf8.Valid(got), "wire JSON remains valid UTF-8")
	require.True(t, json.Valid(got))
	require.False(t, bytes.HasPrefix(got, []byte{0xff, 0xfe}), "no UTF-16LE BOM")
	require.False(t, bytes.HasPrefix(got, []byte{0xfe, 0xff}), "no UTF-16BE BOM")
	fields := []string{"tools", "input", "instructions"}
	if anthropic {
		fields = []string{"system", "messages", "tools"}
	}
	for _, field := range fields {
		require.JSONEq(t, gjson.GetBytes(original, field).Raw, gjson.GetBytes(got, field).Raw, field)
	}
	if anthropic {
		require.Equal(t, requestBodyUnicodeFixture, gjson.GetBytes(got, "messages.1.content.0.thinking").String())
		require.Equal(t, "opaque-fixture-signature", gjson.GetBytes(got, "messages.1.content.0.signature").String())
	}
}

func TestGatewayRequestBodyUnicodeWireEncoding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, lane := range []string{"claude_oauth", "claude_oauth_normalized", "claude_apikey_passthrough", "gpt_oauth", "gpt_oauth_passthrough", "gpt_apikey"} {
		for _, stream := range []bool{false, true} {
			for _, escaped := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/stream=%t/escaped=%t", lane, stream, escaped), func(t *testing.T) {
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					anthropic := strings.HasPrefix(lane, "claude_")
					original := requestBodyEncodingFixture(t, anthropic, stream, escaped)
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					path := "/v1/responses"
					if anthropic {
						path = "/v1/messages"
					}
					c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(original)).WithContext(ctx)
					cfg := &config.Config{}
					var req *http.Request
					var err error
					if anthropic {
						svc := &GatewayService{cfg: cfg}
						account := &Account{ID: 71, Platform: PlatformAnthropic, Type: AccountTypeOAuth}
						body := original
						if lane == "claude_apikey_passthrough" {
							account.Type = AccountTypeAPIKey
							req, _, err = svc.buildUpstreamRequestAnthropicAPIKeyPassthrough(ctx, c, account, body, "synthetic-token")
						} else {
							if lane == "claude_oauth_normalized" {
								body, _ = normalizeClaudeOAuthRequestBody(body, "claude-sonnet-4-6", claudeOAuthNormalizeOptions{})
							}
							req, _, err = svc.buildUpstreamRequest(ctx, c, account, body, "synthetic-token", "oauth", "claude-sonnet-4-6", stream, lane == "claude_oauth_normalized")
						}
					} else {
						svc := &OpenAIGatewayService{cfg: cfg}
						account := &Account{ID: 72, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"chatgpt_account_id": "synthetic-account"}}
						body := original
						if lane == "gpt_apikey" {
							account.Type = AccountTypeAPIKey
						} else {
							body, _, err = normalizeOpenAIPassthroughOAuthBody(body, false)
							require.NoError(t, err)
						}
						if lane == "gpt_oauth_passthrough" {
							req, err = svc.buildUpstreamRequestOpenAIPassthrough(ctx, c, account, body, "synthetic-token")
						} else {
							req, err = svc.buildUpstreamRequest(ctx, c, account, body, "synthetic-token", stream, "", false)
						}
					}
					require.NoError(t, err)
					require.NotNil(t, req)
					built, err := io.ReadAll(req.Body)
					require.NoError(t, err)
					require.NoError(t, req.Body.Close())
					assertRequestBodyUnicodeFields(t, original, built, anthropic)
					// Send the final built body only to loopback, using synthetic auth.
					// This verifies received bytes, not just in-memory JSON round trips.
					captured := make(chan []byte, 1)
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						wire, readErr := io.ReadAll(r.Body)
						if readErr != nil {
							w.WriteHeader(http.StatusBadRequest)
							return
						}
						captured <- wire
						w.Header().Set("Content-Type", "application/json")
						_, _ = w.Write([]byte(`{}`))
					}))
					defer server.Close()
					req.URL, err = url.Parse(server.URL + path)
					require.NoError(t, err)
					req.Host = ""
					req.Body = io.NopCloser(bytes.NewReader(built))
					req.ContentLength = int64(len(built))
					response, err := server.Client().Do(req)
					require.NoError(t, err)
					require.NoError(t, response.Body.Close())
					require.Equal(t, http.StatusOK, response.StatusCode)
					select {
					case wire := <-captured:
						require.Equal(t, built, wire)
						assertRequestBodyUnicodeFields(t, original, wire, anthropic)
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
				})
			}
		}
	}
}

func TestOpenAIWebSocketRequestBodyUnicodeWireEncoding(t *testing.T) {
	for _, escaped := range []bool{false, true} {
		t.Run(fmt.Sprintf("escaped=%t", escaped), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			original := requestBodyEncodingFixture(t, false, true, escaped)
			account := &Account{ID: 72, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
			normalized, _, err := normalizeOpenAIResponsesWebSocketCompatibilityBody(original, account, false)
			require.NoError(t, err)
			var decoded map[string]any
			require.NoError(t, json.Unmarshal(normalized, &decoded))
			svc := &OpenAIGatewayService{cfg: &config.Config{}}
			payload := svc.buildOpenAIWSCreatePayload(decoded, account)
			type frame struct {
				kind coderws.MessageType
				body []byte
				err  error
			}
			captured := make(chan frame, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, acceptErr := coderws.Accept(w, r, nil)
				if acceptErr != nil {
					captured <- frame{err: acceptErr}
					return
				}
				defer conn.CloseNow()
				kind, body, readErr := conn.Read(ctx)
				captured <- frame{kind: kind, body: body, err: readErr}
			}))
			defer server.Close()
			conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
			require.NoError(t, err)
			defer conn.CloseNow()
			client := &coderOpenAIWSClientConn{conn: conn}
			require.NoError(t, client.WriteJSON(ctx, payload))
			select {
			case got := <-captured:
				require.NoError(t, got.err)
				require.Equal(t, coderws.MessageText, got.kind)
				assertRequestBodyUnicodeFields(t, original, got.body, false)
				require.Equal(t, "response.create", gjson.GetBytes(got.body, "type").String())
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		})
	}
}

func TestGatewayRequestBodyUnicodeByteRanges(t *testing.T) {
	for _, anthropic := range []bool{true, false} {
		for _, escaped := range []bool{true, false} {
			t.Run(fmt.Sprintf("anthropic=%t/escaped=%t", anthropic, escaped), func(t *testing.T) {
				body := requestBodyEncodingFixture(t, anthropic, true, escaped)
				// Put multibyte text before every field to distinguish byte ranges
				// from character indexes. Raw JSON parser offsets must stay bytes.
				prefix, err := json.Marshal(requestBodyUnicodeFixture)
				require.NoError(t, err)
				body = append(append([]byte(`{"fixture_prefix":`), append(prefix, ',')...), body[1:]...)
				protocol := "responses"
				if anthropic {
					protocol = PlatformAnthropic
				}
				parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), protocol)
				require.NoError(t, err)
				checkRanges := func() {
					if anthropic {
						require.Equal(t, gjson.GetBytes(body, "system").Raw, string(parsed.SystemRaw()))
						require.Equal(t, gjson.GetBytes(body, "messages").Raw, string(parsed.MessagesRaw()))
					} else {
						require.Equal(t, gjson.GetBytes(body, "input").Raw, string(parsed.InputRaw()))
					}
				}
				checkRanges()
				replacement := "gpt-5.5-new-route"
				if anthropic {
					replacement = "claude-sonnet-4-6-new-route"
				}
				body = ReplaceModelInBody(body, replacement)
				require.NoError(t, parsed.ReplaceBody(body))
				checkRanges()
				require.Equal(t, replacement, parsed.Model)
			})
		}
	}
}
