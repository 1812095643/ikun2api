//go:build unit

package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/config"
	"github.com/MACOS-DO/sub4api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func TestClaudeToolTextDisplayReversibleAndOpaqueFields(t *testing.T) {
	svc := &GatewayService{}
	account := newAnthropicOAuthAccountForPartialUsageTest()
	c, _ := newThinkingRecoveryContext(nil, "display-session", 7)
	svc.prepareClaudeToolText(c, account, "claude-opus-5-5", []byte(thinkingRecoveryFixture))
	rw := buildToolNameRewriteFromBody([]byte(`{"tools":[{"name":"sessions_list"}]}`))
	c.Set(toolNameRewriteKey, rw)
	original := []byte(`{"type":"message","content":[{"type":"thinking","thinking":"cc_sess_list","signature":"cc_sess_list"},{"type":"text","text":"用 cc_sess_list；原名 sessions_list 保持。"},{"type":"tool_use","name":"cc_sess_list","id":"cc_sess_list-id","input":{"value":"cc_sess_list"}}]}`)
	displayed := reverseToolNamesIfPresent(c, original)
	require.Equal(t, "用 sessions_list；原名 sessions_list 保持。", gjson.GetBytes(displayed, "content.1.text").String())
	require.Equal(t, "sessions_list", gjson.GetBytes(displayed, "content.2.name").String())
	for _, path := range []string{"content.0", "content.2.id", "content.2.input"} {
		require.Equal(t, gjson.GetBytes(original, path).Raw, gjson.GetBytes(displayed, path).Raw)
	}
	history := []byte(`{"messages":[{"role":"assistant","content":` + gjson.GetBytes(displayed, "content").Raw + `},{"role":"user","content":"用 sessions_list；原名 sessions_list 保持。"}]}`)
	roundtrip := claudeToolTextFromContext(c)
	restored := roundtrip.restoreHistory(history)
	require.Equal(t, gjson.GetBytes(original, "content.1.text").String(), gjson.GetBytes(restored, "messages.0.content.1.text").String())
	require.Equal(t, gjson.GetBytes(history, "messages.1").Raw, gjson.GetBytes(restored, "messages.1").Raw)
	require.Equal(t, gjson.GetBytes(history, "messages.0.content.0").Raw, gjson.GetBytes(restored, "messages.0.content.0").Raw)
	edited, _ := sjson.SetBytes(history, "messages.0.content.1.text", "用户修改了这段 sessions_list 内容")
	require.Equal(t, edited, roundtrip.restoreHistory(edited))
	// Restoration is positional: the pre-existing real name was not converted.
	require.Contains(t, gjson.GetBytes(restored, "messages.0.content.1.text").String(), "原名 sessions_list")
}

func TestClaudeToolTextStreamsAcrossProtocols(t *testing.T) {
	rw := buildToolNameRewriteFromBody([]byte(`{"tools":[{"name":"sessions_list"}]}`))
	for _, protocol := range []string{"anthropic", "responses", "chat"} {
		t.Run(protocol, func(t *testing.T) {
			svc := &GatewayService{}
			account := newAnthropicOAuthAccountForPartialUsageTest()
			c, _ := newThinkingRecoveryContext(nil, "stream-session", 7)
			svc.prepareClaudeToolText(c, account, "claude-opus-5-5", []byte(thinkingRecoveryFixture))
			c.Set(toolNameRewriteKey, rw)
			chunks := []string{"中文🙂 ", "cc_sess_list", "\nthen cc_sess_list, literal sessions_list"}
			var rendered strings.Builder
			for _, chunk := range chunks {
				var event any
				path := ""
				switch protocol {
				case "anthropic":
					event = map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": chunk}}
					path = "delta.text"
				case "responses":
					event = map[string]any{"type": "response.output_text.delta", "output_index": 0, "content_index": 0, "delta": chunk}
					path = "delta"
				case "chat":
					event = map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": chunk}}}}
					path = "choices.0.delta.content"
				}
				payload, _ := json.Marshal(event)
				sse := []byte("id: unchanged\r\ndata: " + string(payload) + "\r\n\r\n")
				output := string(reverseToolNamesIfPresent(c, sse))
				require.Contains(t, output, "id: unchanged\r\n")
				for _, line := range strings.Split(output, "\r\n") {
					if data, ok := extractAnthropicSSEDataLine(line); ok {
						rendered.WriteString(gjson.Get(data, path).String())
					}
				}
			}
			roundtrip := claudeToolTextFromContext(c)
			roundtrip.finishAll()
			require.NotContains(t, rendered.String(), "cc_sess_list")
			source, ok := roundtrip.cache.restore(roundtrip.scope, rendered.String(), time.Now())
			require.True(t, ok)
			require.Equal(t, strings.Join(chunks, ""), source)
		})
	}
}

func TestClaudeToolTextMultilineSSEAndTerminalJSON(t *testing.T) {
	svc := &GatewayService{}
	c, _ := newThinkingRecoveryContext(nil, "multiline", 7)
	svc.prepareClaudeToolText(c, newAnthropicOAuthAccountForPartialUsageTest(), "claude-opus-5-5", []byte(thinkingRecoveryFixture))
	rw := buildToolNameRewriteFromBody([]byte(`{"tools":[{"name":"sessions_list"}]}`))
	c.Set(toolNameRewriteKey, rw)
	sse := "event: content_block_delta\r\n: keep\r\ndata: {\"type\":\"content_block_delta\",\"index\":0,\r\ndata: \"delta\":{\"type\":\"text_delta\",\"text\":\"cc_sess_list\"}}\r\n\r\n"
	got := string(reverseToolNamesIfPresent(c, []byte(sse)))
	require.Equal(t, strings.Replace(sse, "cc_sess_list", "sessions_list", 1), got)
	reverseToolNamesIfPresent(c, []byte("data: {\"type\":\"content_block_stop\",\"index\":0}\n\n"))
	rt := claudeToolTextFromContext(c)
	original, ok := rt.cache.restore(rt.scope, "sessions_list", time.Now())
	require.True(t, ok)
	require.Equal(t, "cc_sess_list", original)
	for _, payload := range []string{
		`{"object":"response","output":[{"type":"message","content":[{"type":"output_text","text":"Call cc_sess_list"}]}]}`,
		`{"type":"response.completed","response":{"output":[{"type":"message","content":[{"type":"output_text","text":"Call cc_sess_list"}]}]}}`,
		`{"choices":[{"message":{"content":"Call cc_sess_list","reasoning_content":"cc_sess_list"}}]}`,
	} {
		output := string(reverseToolNamesIfPresent(c, []byte(payload)))
		require.Contains(t, output, "Call sessions_list")
		if strings.Contains(payload, "reasoning_content") {
			require.Contains(t, output, `"reasoning_content":"cc_sess_list"`)
		}
	}
}

func TestClaudeToolTextIsolationBoundsAndAmbiguity(t *testing.T) {
	svc := &GatewayService{}
	account := newAnthropicOAuthAccountForPartialUsageTest()
	c, _ := newThinkingRecoveryContext(nil, "session-a", 7)
	svc.prepareClaudeToolText(c, account, "claude-opus-5-5", []byte(thinkingRecoveryFixture))
	base := claudeToolTextFromContext(c)
	rw := buildToolNameRewriteFromBody([]byte(`{"tools":[{"name":"sessions_list"}]}`))
	display := base.render("cc_sess_list", rw, "", true)
	require.Equal(t, "sessions_list", display)
	for _, change := range []string{"key", "user", "account", "model", "session"} {
		other := *account
		cc, _ := newThinkingRecoveryContext(nil, "session-a", 7)
		model := "claude-opus-5-5"
		switch change {
		case "key":
			cc.Set("api_key", &APIKey{ID: 8, UserID: 4})
		case "user":
			cc.Set("api_key", &APIKey{ID: 7, UserID: 5})
		case "account":
			other.ID++
		case "model":
			model = "claude-sonnet-5-5"
		case "session":
			cc.Request.Header.Set("X-Claude-Code-Session-Id", "session-b")
		}
		svc.prepareClaudeToolText(cc, &other, model, []byte(thinkingRecoveryFixture))
		rt := claudeToolTextFromContext(cc)
		require.NotNil(t, rt)
		_, ok := rt.cache.restore(rt.scope, display, time.Now())
		require.False(t, ok, change)
	}
	// Natural real-name prose with identical rendered bytes invalidates ambiguity.
	base.render("sessions_list", rw, "", true)
	_, ok := base.cache.restore(base.scope, display, time.Now())
	require.False(t, ok)
	_, ok = base.cache.restore(base.scope, display, time.Now().Add(toolTextTTL))
	require.False(t, ok)
	for index := range toolTextMaxEntries + 10 {
		base.render(fmt.Sprintf("cc_sess_list %d", index), rw, "", true)
	}
	require.Equal(t, toolTextMaxEntries, base.cache.order.Len())
	require.LessOrEqual(t, base.cache.bytes, toolTextMaxCacheBytes)
	var wg sync.WaitGroup
	for index := range 16 {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			scope := sha256.Sum256([]byte(fmt.Sprint(index)))
			text := fmt.Sprintf("sessions_list %d", index)
			original := fmt.Sprintf("cc_sess_list %d", index)
			base.cache.remember(scope, sha256.Sum256([]byte(text)), sha256.Sum256([]byte(original)), []toolTextPatch{{0, 13, "sessions_list", "cc_sess_list"}}, time.Now())
			base.cache.restore(scope, text, time.Now())
		}(index)
	}
	wg.Wait()
	require.LessOrEqual(t, base.cache.order.Len(), toolTextMaxEntries)
}

func TestClaudeToolTextDisplayExactInverseUnicodeAndLimits(t *testing.T) {
	rw := &ToolNameRewrite{Reverse: map[string]string{"fake": "real", "fake_long": "fake", "alias": "real"}}
	var cache claudeToolTextCache
	scope := [32]byte{1}
	rt := &claudeToolTextRoundtrip{cache: &cache, scope: scope, streams: make(map[string]*toolTextStream)}
	for index := 0; index < 50; index++ {
		original := fmt.Sprintf("🙂%d fake_long 和 fake；literal real；alias\n", index)
		display := rt.render(original, rw, "", true)
		restored, ok := cache.restore(scope, display, time.Now())
		require.True(t, ok)
		require.Equal(t, original, restored)
	}
	original := strings.Repeat("fake ", toolTextMaxPatches+10)
	display := rt.render(original, rw, "limited", false)
	rt.finish("limited")
	restored, ok := cache.restore(scope, display, time.Now())
	require.True(t, ok)
	require.Equal(t, original, restored)
	// Very large tails stop new edits but preserve inversion of earlier edits.
	first := rt.render("fake", rw, "big", false)
	tail := strings.Repeat("x", toolTextMaxBytes+1)
	second := rt.render(tail, rw, "big", false)
	rt.finish("big")
	restored, ok = cache.restore(scope, first+second, time.Now())
	require.True(t, ok)
	require.Equal(t, "fake"+tail, restored)
}

type toolDisplayUpstream struct {
	HTTPUpstream
	calls          int
	stream         bool
	historyMatches bool
}

const toolDisplaySource = "I will call cc_sess_list; literal sessions_list is unchanged."

func (u *toolDisplayUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.calls++
	body, _ := io.ReadAll(req.Body)
	if u.calls == 2 {
		for _, m := range gjson.GetBytes(body, "messages").Array() {
			if m.Get("role").String() == "assistant" {
				for _, b := range m.Get("content").Array() {
					if b.Get("type").String() == "text" && b.Get("text").String() == toolDisplaySource {
						u.historyMatches = true
					}
				}
			}
		}
	}
	content := []any{map[string]any{"type": "thinking", "thinking": "cc_sess_list must stay signed", "signature": "opaque-signature"}, map[string]any{"type": "text", "text": toolDisplaySource}, map[string]any{"type": "tool_use", "name": "cc_sess_list", "id": "tool-display", "input": map[string]any{"value": "cc_sess_list"}}}
	response := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}, "X-Request-Id": {fmt.Sprint(u.calls)}}}
	if !u.stream {
		raw, _ := json.Marshal(map[string]any{"id": "msg_display", "type": "message", "role": "assistant", "model": "claude-opus-5-5", "content": content, "stop_reason": "tool_use", "usage": map[string]int{"input_tokens": 10, "output_tokens": 5}})
		response.Body = io.NopCloser(strings.NewReader(string(raw)))
		return response, nil
	}
	var sse strings.Builder
	emit := func(v any) {
		raw, _ := json.Marshal(v)
		fmt.Fprintf(&sse, "event: %s\ndata: %s\n\n", gjson.GetBytes(raw, "type").String(), raw)
	}
	emit(map[string]any{"type": "message_start", "message": map[string]any{"id": "msg_display", "type": "message", "role": "assistant", "model": "claude-opus-5-5", "content": []any{}, "usage": map[string]int{"input_tokens": 10, "output_tokens": 0}}})
	for index, block := range content {
		if index == 1 {
			emit(map[string]any{"type": "content_block_start", "index": index, "content_block": map[string]string{"type": "text", "text": ""}})
			emit(map[string]any{"type": "content_block_delta", "index": index, "delta": map[string]string{"type": "text_delta", "text": toolDisplaySource}})
		} else {
			emit(map[string]any{"type": "content_block_start", "index": index, "content_block": block})
		}
		emit(map[string]any{"type": "content_block_stop", "index": index})
	}
	emit(map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "tool_use"}, "usage": map[string]int{"output_tokens": 5}})
	emit(map[string]any{"type": "message_stop"})
	response.Header.Set("Content-Type", "text/event-stream")
	response.Body = io.NopCloser(strings.NewReader(sse.String()))
	return response, nil
}

func TestForwardClaudeToolTextCompatibilityTwoTurns(t *testing.T) {
	for _, protocol := range []string{"chat", "responses"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s_stream_%t", protocol, stream), func(t *testing.T) {
				upstream := &toolDisplayUpstream{stream: true}
				cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
				svc := &GatewayService{cfg: cfg, responseHeaderFilter: compileResponseHeaderFilter(cfg), httpUpstream: upstream, rateLimitService: &RateLimitService{}, deferredService: &DeferredService{}, settingService: NewSettingService(&signatureBudgetSettings{}, cfg), identityService: newTestIdentityService(&identityCacheStub{})}
				account := newAnthropicOAuthAccountForPartialUsageTest()
				account.Extra = map[string]any{"session_id_masking_enabled": true, "account_uuid": "11111111-1111-4111-8111-111111111111"}
				body := `{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"inspect sessions"}],"tools":[{"type":"function","function":{"name":"sessions_list","parameters":{"type":"object"}}}]}`
				if protocol == "responses" {
					body = `{"model":"claude-sonnet-4-6","input":[{"role":"user","content":"inspect sessions"}],"tools":[{"type":"function","name":"sessions_list","parameters":{"type":"object"}}]}`
				}
				body, _ = sjson.Set(body, "stream", stream)
				for turn := 0; turn < 2; turn++ {
					c, recorder := newThinkingRecoveryContext([]byte(body), "compat-display", 7)
					var err error
					if protocol == "chat" {
						_, err = svc.ForwardAsChatCompletions(context.Background(), c, account, []byte(body), nil)
					} else {
						_, err = svc.ForwardAsResponses(context.Background(), c, account, []byte(body), nil)
					}
					require.NoError(t, err)
					text := ""
					if !stream {
						if protocol == "chat" {
							text = gjson.GetBytes(recorder.Body.Bytes(), "choices.0.message.content").String()
						} else {
							for _, item := range gjson.GetBytes(recorder.Body.Bytes(), "output").Array() {
								if item.Get("type").String() == "message" {
									text += item.Get("content.0.text").String()
								}
							}
						}
					} else {
						for _, line := range strings.Split(recorder.Body.String(), "\n") {
							if data, ok := extractAnthropicSSEDataLine(line); ok {
								if protocol == "chat" {
									text += gjson.Get(data, "choices.0.delta.content").String()
								} else if gjson.Get(data, "type").String() == "response.output_text.delta" {
									text += gjson.Get(data, "delta").String()
								}
							}
						}
					}
					require.Equal(t, "I will call sessions_list; literal sessions_list is unchanged.", text)
					message, _ := json.Marshal(map[string]string{"role": "assistant", "content": text})
					path := "messages.-1"
					if protocol == "responses" {
						path = "input.-1"
					}
					body, _ = sjson.SetRaw(body, path, string(message))
					body, _ = sjson.SetRaw(body, path, `{"role":"user","content":"continue"}`)
				}
				require.Equal(t, 2, upstream.calls)
				require.True(t, upstream.historyMatches)
			})
		}
	}
}
func (u *toolDisplayUpstream) DoWithTLS(req *http.Request, p string, a int64, n int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, p, a, n)
}

func TestForwardClaudeToolTextDisplayTwoTurnsPreservesWireHistory(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			upstream := &toolDisplayUpstream{stream: stream}
			cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
			svc := &GatewayService{cfg: cfg, responseHeaderFilter: compileResponseHeaderFilter(cfg), httpUpstream: upstream, rateLimitService: &RateLimitService{}, deferredService: &DeferredService{}, settingService: NewSettingService(&signatureBudgetSettings{}, cfg), identityService: newTestIdentityService(&identityCacheStub{})}
			account := newAnthropicOAuthAccountForPartialUsageTest()
			account.Extra = map[string]any{"session_id_masking_enabled": true, "account_uuid": "11111111-1111-4111-8111-111111111111"}
			body := `{"model":"claude-opus-5-5","max_tokens":128,"thinking":{"type":"adaptive"},"tools":[{"name":"sessions_list","input_schema":{"type":"object"}}],"messages":[{"role":"user","content":"inspect sessions"}]}`
			body, _ = sjson.Set(body, "stream", stream)
			for turn := 0; turn < 2; turn++ {
				c, recorder := newThinkingRecoveryContext([]byte(body), "display-roundtrip", 7)
				c.Request.Header.Set("User-Agent", "third-party-client/1.0")
				parsed, err := ParseGatewayRequest(NewRequestBodyRef([]byte(body)), PlatformAnthropic)
				require.NoError(t, err)
				_, err = svc.Forward(context.Background(), c, account, parsed)
				require.NoError(t, err)
				var content []json.RawMessage
				if !stream {
					require.NoError(t, json.Unmarshal([]byte(gjson.GetBytes(recorder.Body.Bytes(), "content").Raw), &content))
				} else {
					for _, line := range strings.Split(recorder.Body.String(), "\n") {
						if data, ok := extractAnthropicSSEDataLine(line); ok {
							if gjson.Get(data, "type").String() == "content_block_start" {
								content = append(content, json.RawMessage(gjson.Get(data, "content_block").Raw))
							}
							if gjson.Get(data, "delta.type").String() == "text_delta" {
								idx := int(gjson.Get(data, "index").Int())
								value := gjson.GetBytes(content[idx], "text").String() + gjson.Get(data, "delta.text").String()
								updated, _ := sjson.SetBytes(content[idx], "text", value)
								content[idx] = updated
							}
						}
					}
				}
				require.Len(t, content, 3)
				require.Equal(t, "I will call sessions_list; literal sessions_list is unchanged.", gjson.GetBytes(content[1], "text").String())
				require.Equal(t, "sessions_list", gjson.GetBytes(content[2], "name").String())
				require.Equal(t, "cc_sess_list must stay signed", gjson.GetBytes(content[0], "thinking").String())
				require.Equal(t, "cc_sess_list", gjson.GetBytes(content[2], "input.value").String())
				raw, _ := json.Marshal(content)
				body, _ = sjson.SetRaw(body, "messages.-1", `{"role":"assistant","content":`+string(raw)+`}`)
				body, _ = sjson.SetRaw(body, "messages.-1", `{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-display","content":"done"}]}`)
			}
			require.Equal(t, 2, upstream.calls)
			require.True(t, upstream.historyMatches, "outbound history must exactly equal upstream's original prose")
		})
	}
}
