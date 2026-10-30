package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type openAIWSHostedTools uint8

const (
	openAIWSHostedWebSearch openAIWSHostedTools = 1 << iota
	openAIWSHostedImageGeneration
)

func openAIWSHostedToolKind(kind string) openAIWSHostedTools {
	kind = strings.TrimSpace(kind)
	if strings.HasPrefix(kind, "web_search") {
		return openAIWSHostedWebSearch
	}
	if kind == "image_generation" {
		return openAIWSHostedImageGeneration
	}
	return 0
}

func (tools openAIWSHostedTools) label() string {
	switch tools {
	case openAIWSHostedWebSearch:
		return "网页搜索"
	case openAIWSHostedImageGeneration:
		return "图片生成"
	default:
		return "网页搜索和图片生成"
	}
}

func (tools openAIWSHostedTools) errorPrefix() string {
	switch tools {
	case openAIWSHostedWebSearch:
		return "web_search"
	case openAIWSHostedImageGeneration:
		return "image_generation"
	default:
		return "hosted_tool"
	}
}

func openAIWSHostedToolsDeclared(payload []byte) openAIWSHostedTools {
	var declared openAIWSHostedTools
	collect := func(raw gjson.Result) {
		for _, tool := range raw.Array() {
			declared |= openAIWSHostedToolKind(tool.Get("type").String())
		}
	}
	collect(gjson.GetBytes(payload, "tools"))
	for _, item := range gjson.GetBytes(payload, "input").Array() {
		if item.Get("type").String() == "additional_tools" {
			collect(item.Get("tools"))
		}
	}
	return declared
}

func openAIWSHostedToolsDeclaredMap(payload map[string]any) openAIWSHostedTools {
	var declared openAIWSHostedTools
	collect := func(raw any) {
		tools, _ := raw.([]any)
		for _, rawTool := range tools {
			tool, _ := rawTool.(map[string]any)
			kind, _ := tool["type"].(string)
			declared |= openAIWSHostedToolKind(kind)
		}
	}
	collect(payload["tools"])
	input, _ := payload["input"].([]any)
	for _, rawItem := range input {
		item, _ := rawItem.(map[string]any)
		if item["type"] == "additional_tools" {
			collect(item["tools"])
		}
	}
	return declared
}

// Called under writeMu after a successful write. A handshake alone does not
// initialize capabilities; later creates never change the initial capability.
func (c *openAIWSConn) recordInitialHostedTools(value any) {
	var body []byte
	switch payload := value.(type) {
	case map[string]any:
		if payload["type"] != "response.create" {
			return
		}
		c.responseCreateSent = true
		c.initialHostedTools = openAIWSHostedToolsDeclaredMap(payload)
		return
	case json.RawMessage:
		body = payload
	case []byte:
		body = payload
	default:
		return
	}
	if gjson.GetBytes(body, "type").String() != "response.create" {
		return
	}
	c.responseCreateSent = true
	c.initialHostedTools = openAIWSHostedToolsDeclared(body)
}

func (l *openAIWSConnLease) missingHostedTools(required openAIWSHostedTools) openAIWSHostedTools {
	conn, err := l.activeConn()
	if err != nil {
		return 0
	}
	conn.writeMu.Lock()
	defer conn.writeMu.Unlock()
	if !conn.responseCreateSent {
		return 0
	}
	return required &^ conn.initialHostedTools
}

func (l *openAIWSConnLease) initializedWithoutWebSearch() bool {
	return l.missingHostedTools(openAIWSHostedWebSearch) != 0
}

func (l *openAIWSConnLease) responseCreateInitialized() bool {
	conn, err := l.activeConn()
	if err != nil {
		return true
	}
	conn.writeMu.Lock()
	defer conn.writeMu.Unlock()
	return conn.responseCreateSent
}

func openAIWSWebSearchDeclared(payload []byte) bool {
	return openAIWSHostedToolsDeclared(payload)&openAIWSHostedWebSearch != 0
}
func openAIWSWebSearchDeclaredMap(payload map[string]any) bool {
	return openAIWSHostedToolsDeclaredMap(payload)&openAIWSHostedWebSearch != 0
}

// The synthetic frame declares capability without giving the actual business
// request a new tool. It carries no prompt/history or continuation anchor.
func openAIWSSearchInitializationPayload(payload map[string]any, lite bool) map[string]any {
	initial := map[string]any{
		"type": "response.create", "model": payload["model"],
		"generate": false, "stream": true, "store": false,
		"input": []any{}, "instructions": "", "tool_choice": "none",
		"parallel_tool_calls": false,
	}
	if metadata, ok := payload["client_metadata"]; ok {
		initial["client_metadata"] = metadata
	}
	search := map[string]any{"type": "web_search"}
	if lite {
		initial["input"] = []any{map[string]any{"type": "additional_tools", "role": "developer", "tools": []any{search}}}
		initial["reasoning"] = map[string]any{"context": "all_turns"}
	} else {
		initial["tools"] = []any{search}
	}
	return initial
}

const openAIWSSearchInitTimeout = 5 * time.Second

// Initialization failures carry the original status and retry hints. They never
// pretend to be a successful business response or a business continuation ID.
type openAIWSSearchUpstreamError struct {
	StatusCode int
	Body       []byte
	Event      []byte
	Headers    http.Header
	Message    string
}

func (e *openAIWSSearchUpstreamError) Error() string { return e.Message }

func newOpenAIWSSearchUpstreamError(event []byte, handshake http.Header) *openAIWSSearchUpstreamError {
	failure := gjson.GetBytes(event, "response.error")
	if !failure.IsObject() {
		failure = gjson.GetBytes(event, "error")
	}
	status := 0
	for _, path := range []string{"status", "status_code", "response.error.status_code", "response.error.status", "error.status_code", "error.status"} {
		candidate := int(gjson.GetBytes(event, path).Int())
		if candidate >= 400 && candidate <= 599 {
			status = candidate
			break
		}
	}
	if status == 0 {
		status = openAIWSErrorHTTPStatusFromRaw(failure.Get("code").String(), failure.Get("type").String())
	}
	if status < 400 || status > 599 {
		status = http.StatusBadGateway
	}
	message := failure.Get("message").String()
	if message == "" {
		message = "OpenAI websocket search initialization failed"
	}
	if !failure.IsObject() {
		raw, _ := json.Marshal(map[string]any{"type": "upstream_error", "message": message})
		failure = gjson.ParseBytes(raw)
	}
	body := []byte(`{"error":` + failure.Raw + `}`)
	headers := cloneHeader(handshake)
	if headers == nil {
		headers = make(http.Header)
	}
	for _, path := range []string{"headers", "response.error.headers", "error.headers"} {
		gjson.GetBytes(event, path).ForEach(func(key, value gjson.Result) bool {
			name := http.CanonicalHeaderKey(key.String())
			if name == "Retry-After" || name == "X-Request-Id" || name == "Retry-After-Ms" {
				headers.Set(name, value.String())
			}
			return true
		})
	}
	return &openAIWSSearchUpstreamError{StatusCode: status, Body: body, Event: append([]byte(nil), event...), Headers: headers, Message: message}
}

func openAIWSHostedHistoryUnavailable() error {
	return newOpenAIWSSearchUpstreamError([]byte(`{"type":"error","status":409,"error":{"type":"invalid_request_error","code":"hosted_tool_history_unavailable","message":"Hosted tool activation requires complete conversation history; resend the complete input without previous_response_id."}}`), nil)
}

func (s *OpenAIGatewayService) initializeOpenAIWSSearch(ctx context.Context, lease *openAIWSConnLease, payload map[string]any, account *Account) (fallback bool, err error) {
	if s == nil || s.cfg == nil || !s.cfg.Gateway.OpenAIWS.SearchInitEnabled || account == nil || !account.IsOpenAIOAuthLike() || lease.responseCreateInitialized() || openAIWSHostedToolsDeclaredMap(payload) != 0 {
		return false, nil
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	lite := false
	input, _ := payload["input"].([]any)
	for _, raw := range input {
		item, _ := raw.(map[string]any)
		if item["type"] == "additional_tools" {
			lite = true
			break
		}
	}
	initial := openAIWSSearchInitializationPayload(payload, lite)
	initCtx, cancel := context.WithTimeout(ctx, openAIWSSearchInitTimeout)
	defer cancel()
	err = s.performOpenAIWSGeneratePrewarmPayload(initCtx, lease, initial, initial, account, nil, 0, true)
	if err == nil {
		return false, nil
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if errors.Is(initCtx.Err(), context.DeadlineExceeded) {
		return true, err
	}
	var failure *openAIWSSearchUpstreamError
	if errors.As(err, &failure) && failure.StatusCode == http.StatusBadRequest {
		code := gjson.GetBytes(failure.Body, "error.code").String()
		kind := gjson.GetBytes(failure.Body, "error.type").String()
		param := gjson.GetBytes(failure.Body, "error.param").String()
		if code == "unsupported_parameter" || kind == "invalid_request_error" {
			return true, err
		}
		for _, name := range []string{"tools", "input", "generate", "instructions", "tool_choice", "parallel_tool_calls", "reasoning"} {
			if param == name || strings.HasPrefix(param, name+".") || strings.HasPrefix(param, name+"[") {
				return true, err
			}
		}
	}
	return false, fmt.Errorf("initialize websocket search: %w", err)
}

// Hides optional reader-loop capabilities while initialization owns the socket.
type openAIWSSearchInitConn struct{ openAIWSClientConn }

// A bounded, process-local copy of complete OAuth turns. No credentials or
// initializer responses are stored. Scope includes the original API key/thread;
// unknown anchors and context-window changes cannot read another chain.
type openAIWSHostedWindowContextKey struct{}

type openAIWSHostedReplay struct {
	responseID string
	windowID   string
	input      []json.RawMessage
	bytes      int
	saved      time.Time
}

func (s *OpenAIGatewayService) prepareOpenAIWSHostedReplay(account *Account, scope string, payload map[string]any, window string) ([]json.RawMessage, bool) {
	if account == nil || !account.IsOpenAIOAuthLike() || scope == "" {
		return nil, false
	}
	raw := payloadAsJSONBytes(payload)
	if len(raw) > 8<<20 {
		return nil, false
	}
	previous := openAIWSPayloadString(payload, "previous_response_id")
	var prior []json.RawMessage
	complete := previous == ""
	if previous != "" {
		key := fmt.Sprintf("%d:%s", account.ID, scope)
		s.openaiWSHostedReplayMu.Lock()
		cached, exists := s.openaiWSHostedReplay[key]
		s.openaiWSHostedReplayMu.Unlock()
		if exists && cached.responseID == previous && time.Since(cached.saved) < time.Hour && (window == "" || cached.windowID == "" || window == cached.windowID) {
			prior, complete = cached.input, true
		}
	}
	if !complete {
		return nil, false
	}
	// The native HTTP transformer normalizes call_* IDs to fc_* (and the
	// equivalent custom-tool prefixes). Match the same known alias before the
	// existing replay sanitizer discards unpaired historical calls.
	if len(prior) > 0 {
		wanted := make(map[string]struct{})
		for _, item := range gjson.GetBytes(raw, "input").Array() {
			if isCodexToolCallOutputItemType(item.Get("type").String()) {
				wanted[item.Get("call_id").String()] = struct{}{}
			}
		}
		for i, item := range prior {
			kind := gjson.GetBytes(item, "type").String()
			if !isCodexToolCallContextItemType(kind) {
				continue
			}
			id := gjson.GetBytes(item, "call_id").String()
			if _, exists := wanted[id]; exists {
				continue
			}
			normalized := normalizeCodexCallIDForItemType(kind, id)
			if normalized == id || id == "" {
				continue
			}
			if _, exists := wanted[normalized]; !exists {
				continue
			}
			if priorID := gjson.GetBytes(item, "id").String(); priorID != "" && priorID != normalized {
				continue
			}
			updated, err := sjson.SetBytes(item, "call_id", normalized)
			if err == nil {
				copyOfPrior := append([]json.RawMessage(nil), prior...)
				copyOfPrior[i] = updated
				prior = copyOfPrior
			}
		}
	}
	input, exists, err := buildOpenAIWSReplayInputSequence(prior, prior != nil, raw, previous != "")
	return input, err == nil && exists
}

func (s *OpenAIGatewayService) saveOpenAIWSHostedReplay(account *Account, scope, responseID, window string, input []json.RawMessage) {
	if account == nil || !account.IsOpenAIOAuthLike() || scope == "" || responseID == "" {
		return
	}
	size := 0
	for _, item := range input {
		size += len(item)
		if size > 8<<20 {
			return
		}
	}
	entry := openAIWSHostedReplay{responseID: responseID, windowID: window, input: input, bytes: size, saved: time.Now()}
	key := fmt.Sprintf("%d:%s", account.ID, scope)
	s.openaiWSHostedReplayMu.Lock()
	defer s.openaiWSHostedReplayMu.Unlock()
	if s.openaiWSHostedReplay == nil {
		s.openaiWSHostedReplay = make(map[string]openAIWSHostedReplay)
	}
	delete(s.openaiWSHostedReplay, key)
	total := 0
	for k, old := range s.openaiWSHostedReplay {
		if time.Since(old.saved) >= time.Hour {
			delete(s.openaiWSHostedReplay, k)
		} else {
			total += old.bytes
		}
	}
	for len(s.openaiWSHostedReplay) >= 128 || total+size > 64<<20 {
		oldestKey := ""
		oldestTime := time.Now()
		for k, old := range s.openaiWSHostedReplay {
			if old.saved.Before(oldestTime) {
				oldestKey, oldestTime = k, old.saved
			}
		}
		if oldestKey == "" {
			break
		}
		total -= s.openaiWSHostedReplay[oldestKey].bytes
		delete(s.openaiWSHostedReplay, oldestKey)
	}
	s.openaiWSHostedReplay[key] = entry
}
