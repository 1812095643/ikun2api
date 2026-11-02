//go:build unit

package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/config"
	"github.com/MACOS-DO/sub4api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const thinkingRecoveryFixture = `{"model":"claude-opus-5-5","max_tokens":128,"thinking":{"type":"adaptive"},"output_config":{"effort":"high"},"metadata":{"user_id":"{\"device_id\":\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\",\"account_uuid\":\"11111111-1111-4111-8111-111111111111\",\"session_id\":\"22222222-2222-4222-8222-222222222222\"}"},"messages":[{"role":"user","content":"Start"},{"role":"assistant","content":[{"type":"thinking","thinking":"private old reasoning","signature":"rejected-signature"},{"type":"tool_use","id":"toolu_1","name":"lookup","input":{"n":9007199254740993}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"tool answer"}]}]}`

type thinkingRecoveryUpstream struct {
	HTTPUpstream
	bodies     [][]byte
	failRepair bool
}

func (u *thinkingRecoveryUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	u.bodies = append(u.bodies, body)
	response := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}, "X-Request-Id": {fmt.Sprintf("request-%d", len(u.bodies))}}}
	if strings.Contains(string(body), "rejected-signature") || u.failRepair {
		response.StatusCode = 400
		response.Body = io.NopCloser(strings.NewReader("{\"type\":\"error\",\"error\":{\"type\":\"invalid_request_error\",\"message\":\"messages.1.content.0: Invalid `signature` in `thinking` block\"}}"))
		return response, nil
	}
	response.Body = io.NopCloser(strings.NewReader(`{"id":"msg_recovered","type":"message","role":"assistant","model":"claude-opus-5-5","content":[{"type":"thinking","thinking":"fresh reasoning","signature":"fresh-signature"},{"type":"text","text":"Done"}],"stop_reason":"end_turn","usage":{"input_tokens":11,"output_tokens":3}}`))
	if gjson.GetBytes(body, "stream").Bool() {
		response.Header.Set("Content-Type", "text/event-stream")
		response.Body = io.NopCloser(strings.NewReader("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_fresh\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-opus-5-5\",\"content\":[],\"usage\":{\"input_tokens\":11,\"output_tokens\":0}}}\n\n" +
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\",\"signature\":\"\"}}\n\n" +
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"fresh reasoning\"}}\n\n" +
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"previous-complete-signature\"}}\n\n" +
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"fresh-signature\"}}\n\n" +
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":3}}\n\n" +
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}
	return response, nil
}

func (u *thinkingRecoveryUpstream) DoWithTLS(req *http.Request, proxy string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, accountID, concurrency)
}

func newThinkingRecoveryContext(body []byte, session string, keyID int64) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(body)))
	c.Request.Header.Set("User-Agent", "claude-cli/2.1.309 (external, claude-desktop-3p, agent-sdk/0.3.309)")
	c.Request.Header.Set("X-Claude-Code-Session-Id", session)
	c.Set("api_key", &APIKey{ID: keyID, UserID: 4})
	return c, recorder
}

func TestForwardClaudeThinkingRecoveryRetainsNewThinking(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream_%t", stream), func(t *testing.T) {
			upstream := &thinkingRecoveryUpstream{}
			cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
			settings := &signatureBudgetSettings{}
			svc := &GatewayService{cfg: cfg, responseHeaderFilter: compileResponseHeaderFilter(cfg), httpUpstream: upstream, rateLimitService: &RateLimitService{}, deferredService: &DeferredService{}, settingService: NewSettingService(settings, cfg)}
			account := newAnthropicOAuthAccountForPartialUsageTest()
			body, err := sjson.Set(thinkingRecoveryFixture, "stream", stream)
			require.NoError(t, err)
			forward := func(raw, session string, keyID int64) (*ForwardResult, *httptest.ResponseRecorder, error) {
				c, recorder := newThinkingRecoveryContext([]byte(raw), session, keyID)
				parsed, parseErr := ParseGatewayRequest(NewRequestBodyRef([]byte(raw)), PlatformAnthropic)
				require.NoError(t, parseErr)
				result, forwardErr := svc.Forward(context.Background(), c, account, parsed)
				return result, recorder, forwardErr
			}
			result, recorder, err := forward(body, "session-a", 7)
			require.NoError(t, err)
			require.Equal(t, 3, result.Usage.OutputTokens)
			require.Len(t, upstream.bodies, 2)
			require.Contains(t, string(upstream.bodies[0]), "rejected-signature")
			require.NotContains(t, string(upstream.bodies[1]), "private old reasoning")
			require.Equal(t, "adaptive", gjson.GetBytes(upstream.bodies[1], "thinking.type").String())
			require.Equal(t, "9007199254740993", gjson.GetBytes(upstream.bodies[1], "messages.1.content.0.input.n").Raw)
			require.Contains(t, recorder.Body.String(), "fresh-signature")
			if stream {
				require.Contains(t, recorder.Body.String(), `"type":"signature_delta","signature":"fresh-signature"`)
				require.Contains(t, recorder.Body.String(), `"type":"signature_delta","signature":"previous-complete-signature"`)
				require.Equal(t, 2, strings.Count(recorder.Body.String(), `"type":"signature_delta"`))
				require.Less(t, strings.Index(recorder.Body.String(), "previous-complete-signature"), strings.Index(recorder.Body.String(), "fresh-signature"))
			}

			body, err = sjson.SetRaw(body, "messages.-1", `{"role":"assistant","content":[{"type":"thinking","thinking":"fresh reasoning","signature":"fresh-signature"},{"type":"text","text":"Done"}]}`)
			require.NoError(t, err)
			body, err = sjson.SetRaw(body, "messages.-1", `{"role":"user","content":"Continue"}`)
			require.NoError(t, err)
			_, _, err = forward(body, "session-a", 7)
			require.NoError(t, err)
			require.Len(t, upstream.bodies, 3, "later turn must not repeat the known 400")
			require.NotContains(t, string(upstream.bodies[2]), "rejected-signature")
			require.Contains(t, string(upstream.bodies[2]), "fresh-signature")
			require.Equal(t, "adaptive", gjson.GetBytes(upstream.bodies[2], "thinking.type").String())

			_, _, err = forward(body, "session-b", 7)
			require.NoError(t, err)
			require.Len(t, upstream.bodies, 5, "another session gets its own initial attempt")
			_, _, err = forward(body, "session-a", 8)
			require.NoError(t, err)
			require.Len(t, upstream.bodies, 7, "another API Key cannot inherit recovery state")

			settings.disabled = true
			_, _, err = forward(body, "session-a", 7)
			require.Error(t, err)
			require.Len(t, upstream.bodies, 8, "disabled rectifier bypasses remembered recovery")
			require.Contains(t, string(upstream.bodies[7]), "rejected-signature")
		})
	}
}

func TestForwardClaudeThinkingRecoveryDoesNotRememberFailedRepair(t *testing.T) {
	upstream := &thinkingRecoveryUpstream{failRepair: true}
	cfg := &config.Config{}
	svc := &GatewayService{cfg: cfg, httpUpstream: upstream, rateLimitService: &RateLimitService{}, settingService: NewSettingService(&signatureBudgetSettings{}, cfg)}
	account := newAnthropicOAuthAccountForPartialUsageTest()
	for range 2 {
		c, _ := newThinkingRecoveryContext([]byte(thinkingRecoveryFixture), "session", 7)
		parsed, err := ParseGatewayRequest(NewRequestBodyRef([]byte(thinkingRecoveryFixture)), PlatformAnthropic)
		require.NoError(t, err)
		_, err = svc.Forward(context.Background(), c, account, parsed)
		require.Error(t, err)
	}
	require.Len(t, upstream.bodies, 4)
	require.Contains(t, string(upstream.bodies[2]), "rejected-signature")
	require.Zero(t, svc.claudeThinkingRecovery.order.Len())
}

func TestClaudeThinkingRecoveryCacheBoundaries(t *testing.T) {
	var cache claudeThinkingRecoveryCache
	now := time.Unix(1000, 0)
	scope := sha256.Sum256([]byte("session-a"))
	other := sha256.Sum256([]byte("session-b"))
	block := sha256.Sum256([]byte("thinking"))
	cache.remember(scope, [][32]byte{block}, now)
	require.Contains(t, cache.known(scope, now), block)
	require.Empty(t, cache.known(other, now))
	require.Empty(t, cache.known(scope, now.Add(claudeThinkingRecoveryTTL)))
	require.Empty(t, cache.scopes)
	for i := range claudeThinkingRecoveryMaxEntries + 20 {
		digest := sha256.Sum256([]byte(fmt.Sprint(i)))
		cache.remember(scope, [][32]byte{digest}, now)
	}
	require.Equal(t, claudeThinkingRecoveryMaxEntries, cache.order.Len())
	require.Len(t, cache.scopes[scope], claudeThinkingRecoveryMaxEntries)
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			digest := sha256.Sum256([]byte(fmt.Sprintf("parallel-%d", i)))
			cache.remember(other, [][32]byte{digest}, now)
			cache.known(other, now)
		}(i)
	}
	wg.Wait()
	require.Equal(t, claudeThinkingRecoveryMaxEntries, cache.order.Len())
}

func TestClaudeThinkingRecoveryScopeAndDigest(t *testing.T) {
	c, _ := newThinkingRecoveryContext(nil, "session", 7)
	account := newAnthropicOAuthAccountForPartialUsageTest()
	base, ok := claudeThinkingRecoveryScope(c, account, "claude-opus-5-5", "metadata-session")
	require.True(t, ok)
	for _, change := range []func(){
		func() { c.Set("api_key", &APIKey{ID: 8, UserID: 4}) },
		func() { c.Set("api_key", &APIKey{ID: 7, UserID: 5}) },
		func() { group := int64(8); c.Set("api_key", &APIKey{ID: 7, UserID: 4, GroupID: &group}) },
		func() { c.Request.Header.Set("X-Claude-Code-Session-Id", "other") },
	} {
		c, _ = newThinkingRecoveryContext(nil, "session", 7)
		change()
		next, valid := claudeThinkingRecoveryScope(c, account, "claude-opus-5-5", "metadata-session")
		require.True(t, valid)
		require.NotEqual(t, base, next)
	}
	c, _ = newThinkingRecoveryContext(nil, "session", 7)
	next, _ := claudeThinkingRecoveryScope(c, account, "claude-sonnet-5-5", "metadata-session")
	require.NotEqual(t, base, next)
	next, _ = claudeThinkingRecoveryScope(c, account, "claude-opus-5-5", "other-metadata")
	require.NotEqual(t, base, next)
	account.ID++
	next, _ = claudeThinkingRecoveryScope(c, account, "claude-opus-5-5", "metadata-session")
	require.NotEqual(t, base, next)
	c.Request.Header.Del("X-Claude-Code-Session-Id")
	_, ok = claudeThinkingRecoveryScope(c, account, "claude-opus-5-5", "")
	require.False(t, ok)
	c.Set("api_key", nil)
	_, ok = claudeThinkingRecoveryScope(c, account, "claude-opus-5-5", "metadata-session")
	require.False(t, ok)
	a := gjson.Parse(`{"type":"thinking","thinking":"hello","signature":"abc"}`)
	b := gjson.Parse(`{ "signature": "abc", "thinking": "hello", "type": "thinking" }`)
	da, _ := claudeThinkingBlockDigest(a)
	db, _ := claudeThinkingBlockDigest(b)
	require.Equal(t, da, db)
	dc, _ := claudeThinkingBlockDigest(gjson.Parse(`{"type":"thinking","thinking":"changed","signature":"abc"}`))
	require.NotEqual(t, da, dc)
}

func TestStripClaudeThinkingHistoryMatchesOfficialBoundaries(t *testing.T) {
	body := []byte(`{"thinking":{"type":"adaptive"},"messages":[{"role":"user","content":[{"type":"text","text":"user content"}]},{"role":"assistant","content":[{"type":"thinking","thinking":"old","signature":"old"},{"type":"redacted_thinking","data":"encrypted-old"},{"type":"text","text":"  "}]},{"role":"assistant","content":[{"type":"thinking","thinking":"fresh","signature":"fresh"},{"type":"tool_use","id":"t1","name":"x","input":{"n":9007199254740993}}]}]}`)
	old, _ := claudeThinkingBlockDigest(gjson.GetBytes(body, "messages.1.content.0"))
	redacted, _ := claudeThinkingBlockDigest(gjson.GetBytes(body, "messages.1.content.1"))
	clean, removed, changed := stripClaudeThinkingHistory(body, map[[32]byte]struct{}{old: {}, redacted: {}})
	require.True(t, changed)
	require.Len(t, removed, 2)
	require.Equal(t, "[Thinking removed]", gjson.GetBytes(clean, "messages.1.content.0.text").String())
	require.Equal(t, gjson.GetBytes(body, "messages.2").Raw, gjson.GetBytes(clean, "messages.2").Raw)
	require.Equal(t, gjson.GetBytes(body, "messages.0").Raw, gjson.GetBytes(clean, "messages.0").Raw)
	require.Equal(t, "adaptive", gjson.GetBytes(clean, "thinking.type").String())
	all, _, changed := stripClaudeThinkingHistory(body, nil)
	require.True(t, changed)
	require.NotContains(t, string(all), `"signature"`)
	require.NotContains(t, string(all), "encrypted-old")
	require.Equal(t, "9007199254740993", gjson.GetBytes(all, "messages.2.content.0.input.n").Raw)
	unchanged, _, changed := stripClaudeThinkingHistory(body, map[[32]byte]struct{}{})
	require.False(t, changed)
	require.Equal(t, body, unchanged)
}

func TestClaudeThinkingRecoveryErrorGate(t *testing.T) {
	for _, tc := range []struct {
		message string
		want    bool
	}{
		{"messages.5.content.0: Invalid `signature` in `thinking` block", true},
		{"messages.23.content.0: Invalid signature in thinking block", true},
		{"invalid signature in tool_result", false},
		{"Invalid API key", false},
		{"thinking budget must be greater than 0", false},
	} {
		body, err := json.Marshal(map[string]any{"error": map[string]string{"message": tc.message}})
		require.NoError(t, err)
		require.Equal(t, tc.want, isClaudeInvalidThinkingSignature(body))
	}
}

func TestClaudeThinkingRecoveryProtocolScope(t *testing.T) {
	account := newAnthropicOAuthAccountForPartialUsageTest()
	require.True(t, supportsClaudeThinkingHistoryRecovery(account, "claude-opus-5-5"))
	require.False(t, supportsClaudeThinkingHistoryRecovery(account, "deepseek-v4"))
	require.False(t, supportsClaudeThinkingHistoryRecovery(account, "unknown-model"))
	account.Type = AccountTypeAPIKey
	require.False(t, supportsClaudeThinkingHistoryRecovery(account, "claude-opus-5-5"))
	require.False(t, supportsClaudeThinkingHistoryRecovery(nil, "claude-opus-5-5"))
}
