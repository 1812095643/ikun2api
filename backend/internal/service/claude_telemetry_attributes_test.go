package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/tlsfingerprint"
	"github.com/tidwall/gjson"
)

func assertClaudeTelemetryAttributes(t *testing.T, body []byte) {
	t.Helper()
	for _, event := range gjson.GetBytes(body, "events").Array() {
		encoded := event.Get("event_data.additional_metadata").String()
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			t.Fatal(err)
		}
		var metadata map[string]any
		if err := json.Unmarshal(decoded, &metadata); err != nil {
			t.Fatal(err)
		}
		if metadata["anthropic.plan"] != "pro" || metadata["anthropic.provider"] != "firstParty" || metadata["anthropic.custom_base_url"] != false || metadata["provider"] != "firstParty" {
			t.Fatal("服务端属性、旧 provider 或布尔值类型不正确")
		}
		for _, key := range []string{"anthropic.seat_type", "program_use_case_id", "base_url", "user.email", "vcs.repository.url.full", "idp_groups"} {
			if _, exists := metadata[key]; exists {
				t.Fatalf("未选择的属性 %s 不应进入摘要", key)
			}
		}
		if event.Get("event_data.client_type").String() != "ikun2api_gateway" || metadata["source"] != "ikun2api_gateway" {
			t.Fatal("新增属性不能隐去服务端重建来源")
		}
		if strings.Contains(string(body)+string(decoded), "private-canary") {
			t.Fatal("原始客户端资料进入了重建摘要")
		}
	}
}

func TestClaudeTelemetryAttributesCannotBeOverriddenByClient(t *testing.T) {
	for _, name := range []string{"success", "error"} {
		t.Run(name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", nil)
			req.Header.Set("anthropic.provider", "https://private-canary.invalid")
			req.Header.Set("anthropic.custom_base_url", "true")
			req.Header.Set("x-claude-code-wiggly-dove", "private-canary")
			wire := []byte(strings.Replace(string(telemetryTestWire()), `"messages":`, `"anthropic.plan":"enterprise","anthropic.provider":"gateway","anthropic.custom_base_url":true,"anthropic.seat_type":"premium","program_use_case_id":"private-canary","system":"private-canary","messages":`, 1))
			account := &Account{Extra: map[string]any{"anthropic.plan": "enterprise", "anthropic.seat_type": "premium", "git_remote": "private-canary"}}
			var result *ForwardResult
			var forwardErr error
			status := http.StatusOK
			if name == "success" {
				result = &ForwardResult{Usage: ClaudeUsage{InputTokens: 17, OutputTokens: 5}}
			} else {
				status, forwardErr = http.StatusBadRequest, fmt.Errorf("private-canary")
			}
			body, event, err := buildClaudeTelemetryBody(req, wire, account, result, forwardErr, time.Now(), status, 1)
			if err != nil || event != "tengu_api_"+name {
				t.Fatalf("生成摘要失败：%s %v", event, err)
			}
			assertClaudeTelemetryAttributes(t, body)
		})
	}
}

func TestClaudeTelemetryAttributesRejectNonOfficialUpstream(t *testing.T) {
	for name, address := range map[string]string{
		"custom":         "https://private-canary.invalid/v1/messages",
		"http":           "http://api.anthropic.com/v1/messages",
		"suffix":         "https://api.anthropic.com.private-canary.invalid/v1/messages",
		"userinfo":       "https://private-canary@api.anthropic.com/v1/messages",
		"count_tokens":   "https://api.anthropic.com/v1/messages/count_tokens",
		"fragment":       "https://api.anthropic.com/v1/messages#private-canary",
		"get":            "https://api.anthropic.com/v1/messages",
		"custom_account": "https://api.anthropic.com/v1/messages",
		"nil":            "https://api.anthropic.com/v1/messages",
	} {
		t.Run(name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, address, nil)
			if err != nil {
				t.Fatal(err)
			}
			account := &Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth}
			switch name {
			case "get":
				req.Method = http.MethodGet
			case "nil":
				req = nil
			case "custom_account":
				account.Extra = map[string]any{"custom_base_url_enabled": true}
			}
			body, _, err := buildClaudeTelemetryBody(req, telemetryTestWire(), account, &ForwardResult{}, nil, time.Now(), 200, 1)
			if err == nil || len(body) != 0 {
				t.Fatal("非官方出站请求不能声明 firstParty/false")
			}
		})
	}
}

func TestClaudeTelemetryAttributesReachBatchedTransport(t *testing.T) {
	t.Setenv("SUB6_CLAUDE_TELEMETRY_ENABLED", "true")
	batch := combineClaudeTelemetryDeliveries([]claudeTelemetryDelivery{telemetryBatchDelivery(t, true), telemetryBatchDelivery(t, false)})[0]
	transport := &localClaudeTelemetryTransport{status: 200, response: `{"accepted_count":2}`, check: func(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatal(err)
		}
		if req.URL.String() != claudeTelemetryEndpoint || gjson.GetBytes(body, "events.#").Int() != 2 {
			t.Fatal("摘要未通过原有第一方批次入口发送")
		}
		assertClaudeTelemetryAttributes(t, body)
	}}
	gateway := &GatewayService{httpUpstream: transport}
	if _, accepted, err := gateway.sendClaudeTelemetry(context.Background(), batch); err != nil || accepted != 2 || transport.calls != 1 {
		t.Fatalf("批次传输失败：accepted=%d calls=%d err=%v", accepted, transport.calls, err)
	}
}

func TestClaudeTelemetryRejectsDiagnosticsAtEgress(t *testing.T) {
	t.Setenv("SUB6_CLAUDE_TELEMETRY_ENABLED", "true")
	first := telemetryBatchDelivery(t, true)
	for _, name := range []string{"tengu_agent_proxy_open_tunnels", "tengu_resume_hook_stopped_batch", "tengu_vscode_start_second_try", "tengu_mcp_gmail_file_reference_upload", "tengu_mcp_drive_download_saved_as_text", "tengu_tools_used_header_size", "tengu_repl_mode_opt_in", "tengu_repl_script_end", "tengu_repl_inner_executing", "tengu_repl_script_call_without_classifier", "unknown_future_diagnostic"} {
		t.Run(name, func(t *testing.T) {
			bad := first
			bad.body = []byte(strings.Replace(string(first.body), "tengu_api_success", name, 1))
			payload, _ := decodeClaudeTelemetryPayload(first.body)
			var extra claudeTelemetryPayload
			if err := json.Unmarshal(bad.body, &extra); err != nil {
				t.Fatal(err)
			}
			payload.Events = append(payload.Events, extra.Events...)
			mixed := first
			mixed.body, _ = json.Marshal(payload)
			for _, delivery := range []claudeTelemetryDelivery{bad, mixed} {
				transport := &localClaudeTelemetryTransport{status: 200, response: `{"accepted_count":2}`}
				gateway := &GatewayService{httpUpstream: transport}
				if _, _, err := gateway.sendClaudeTelemetry(context.Background(), delivery); err == nil || transport.calls != 0 {
					t.Fatal("诊断事件或混合批次绕过出站检查")
				}
			}
			batch := first
			if appendClaudeTelemetryBatch(&batch, bad) {
				t.Fatal("诊断事件不能并入正常摘要批次")
			}
		})
	}
}

func TestClaudeTelemetryRejectsAmbiguousEgressEventNames(t *testing.T) {
	t.Setenv("SUB6_CLAUDE_TELEMETRY_ENABLED", "true")
	for _, raw := range []string{
		`{"events":[{"event_type":"ClaudeCodeInternalEvent","event_data":{"event_name":"tengu_api_success","event_name":"tengu_agent_proxy_open_tunnels"}}]}`,
		`{"events":[{"event_type":"ClaudeCodeInternalEvent","event_type":"OtherEvent","event_data":{"event_name":"tengu_api_success"}}]}`,
		`{"events":[{"event_type":"ClaudeCodeInternalEvent","event_data":{"event_name":"tengu_api_success"},"event_data":{"event_name":"tengu_resume_hook_stopped_batch"}}]}`,
		`{"events":[],"events":[{"event_type":"ClaudeCodeInternalEvent","event_data":{"event_name":"tengu_api_success"}}]}`,
	} {
		delivery := telemetryBatchDelivery(t, true)
		delivery.body = []byte(raw)
		transport := &localClaudeTelemetryTransport{status: 200, response: `{"accepted_count":1}`}
		gateway := &GatewayService{httpUpstream: transport}
		if _, _, err := gateway.sendClaudeTelemetry(context.Background(), delivery); err == nil || transport.calls != 0 {
			t.Fatal("重复字段不能绕过出站事件检查")
		}
	}
}
