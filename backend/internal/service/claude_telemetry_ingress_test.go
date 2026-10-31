package service

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestClaudeTelemetryRejectsAmbiguousAndOversizedInput(t *testing.T) {
	for name, body := range map[string]string{
		"invalid":         `{"events":`,
		"missing":         `{}`,
		"wrong_type":      `{"events":{}}`,
		"duplicate_root":  `{"events":[],"ev\u0065nts":[]}`,
		"duplicate_event": `{"events":[{"event_type":"ClaudeCodeInternalEvent","event_type":"other","event_data":{}}]}`,
		"duplicate_data":  `{"events":[{"event_type":"ClaudeCodeInternalEvent","event_data":{"event_name":"tengu_api_success","event_\u006eame":"tengu_api_error"}}]}`,
		"oversized":       `{"events":[],"data":"` + strings.Repeat("x", MaxClaudeTelemetryBodyBytes) + `"}`,
		"too_many":        `{"events":[` + strings.Repeat(`{},`, 200) + `{}` + `]}`,
		"deeply_nested":   `{"events":[],"data":` + strings.Repeat("[", 11000) + "0" + strings.Repeat("]", 11000) + "}",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseClaudeTelemetryBatch([]byte(body)); err == nil {
				t.Fatal("不明确或超限的输入必须拒绝")
			}
		})
	}
}

func TestClaudeTelemetryOnlyRecognizesObservedEventKinds(t *testing.T) {
	for _, name := range []string{
		"tengu_api_success", "tengu_api_error", "tengu_init", "tengu_git_info",
		"tengu_safety_event", "safety_event", "tengu_remote_reported_cwd",
		"tengu_memory_low", "tengu_memory_drain", "tengu_tool_cgroup",
		"tengu_api_tools_used_header", "tengu_feature_ok", "unknown",
	} {
		batch := telemetryTestBatch(t, name)
		want := 0
		if name == "tengu_api_success" || name == "tengu_api_error" {
			want = 1
		}
		if batch.Count != 1 || len(batch.triggers) != want {
			t.Fatalf("事件 %s 准入不正确", name)
		}
	}
	body := fmt.Sprintf(`{"events":[{"event_type":"GrowthbookExperimentEvent","event_data":{"session_id":%q,"event_id":%q,"git_remote_url":"private"}}]}`, telemetryClientSession, telemetryTestEvent)
	batch, err := ParseClaudeTelemetryBatch([]byte(body))
	if err != nil || len(batch.triggers) != 0 {
		t.Fatal("灰度实验事件不得转发")
	}
}

func TestClaudeTelemetryClaimsAreScopedAndAtomic(t *testing.T) {
	var buffer claudeTelemetryBuffer
	now := time.Now()
	trigger := telemetryTestBatch(t, "tengu_api_success").triggers[0]
	delivery := claudeTelemetryDelivery{eventName: "tengu_api_success", accountID: 19}
	buffer.remember(17, telemetryClientSession, delivery, now)
	wrong := trigger
	wrong.sessionID = uuid.New()
	if _, ok := buffer.claim(18, trigger, now); ok {
		t.Fatal("跨 Key 领取")
	}
	if _, ok := buffer.claim(17, wrong, now); ok {
		t.Fatal("跨会话领取")
	}
	wrong = trigger
	wrong.eventName = "tengu_api_error"
	if _, ok := buffer.claim(17, wrong, now); ok {
		t.Fatal("改写请求结果")
	}
	var winners atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, ok := buffer.claim(17, trigger, now); ok {
				winners.Add(1)
			}
		}()
	}
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatal("并发触发重复领取真实观测")
	}
	buffer.remember(17, telemetryClientSession, delivery, now)
	if _, ok := buffer.claim(17, trigger, now); ok {
		t.Fatal("重放旧事件不能消费新观测")
	}
	trigger.eventID = uuid.New()
	if _, ok := buffer.claim(17, trigger, now.Add(claudeTelemetryRetention)); ok {
		t.Fatal("过期观测不能发送")
	}
}

func TestClaudeTelemetryBuffersRemainBounded(t *testing.T) {
	var buffer claudeTelemetryBuffer
	now := time.Now()
	delivery := claudeTelemetryDelivery{eventName: "tengu_api_success"}
	for i := 0; i < claudeTelemetryMaxEntries+50; i++ {
		buffer.remember(int64(i+1), telemetryClientSession, delivery, now)
	}
	if len(buffer.observed) != claudeTelemetryMaxEntries {
		t.Fatal("观测缓存超出边界")
	}
	trigger := telemetryTestBatch(t, "tengu_api_success").triggers[0]
	for i := 0; i < claudeTelemetryMaxEntries; i++ {
		trigger.eventID = uuid.New()
		if _, ok := buffer.claim(int64(i+1), trigger, now); !ok {
			t.Fatal("有效观测未领取")
		}
	}
	buffer.remember(17, telemetryClientSession, delivery, now)
	trigger.eventID = uuid.New()
	if _, ok := buffer.claim(17, trigger, now); ok {
		t.Fatal("去重缓存已满时必须停止发送")
	}
	buffer.prune(now.Add(claudeTelemetryRetention))
	if len(buffer.observed) != 0 || len(buffer.seen) != 0 {
		t.Fatal("过期记录未清理")
	}
}

func TestClaudeTelemetryDoesNotKeepUntrustedIdentity(t *testing.T) {
	t.Setenv("SUB6_CLAUDE_TELEMETRY_ENABLED", "true")
	for _, variant := range []string{"valid", "no_key", "no_proxy", "no_fingerprint", "wrong_fingerprint", "wrong_account", "passthrough_session", "custom_base", "apikey", "http", "other_host", "other_path"} {
		t.Run(variant, func(t *testing.T) {
			cache := &telemetryIdentityCache{fp: &Fingerprint{ClientID: strings.Repeat("a", 64), UserAgent: "claude-cli/2.1.301"}}
			gateway := &GatewayService{httpUpstream: &localClaudeTelemetryTransport{}, identityService: &IdentityService{cache: cache}}
			c := telemetryTestContext()
			account := &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Extra: map[string]any{"account_uuid": telemetryTestAccount}}
			req, _ := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", nil)
			setHeaderRaw(req.Header, "authorization", "Bearer offline-only")
			session := telemetryClientSession
			proxy := telemetryTestProxy
			switch variant {
			case "no_proxy":
				proxy = ""
			case "no_key":
				c.Set("api_key", nil)
			case "no_fingerprint":
				cache.fp = nil
			case "wrong_fingerprint":
				cache.fp.ClientID = strings.Repeat("b", 64)
			case "wrong_account":
				account.Extra["account_uuid"] = uuid.NewString()
			case "passthrough_session":
				session = telemetryTestSession
			case "custom_base":
				account.Extra["custom_base_url_enabled"] = true
			case "apikey":
				account.Type = AccountTypeAPIKey
			case "http":
				req.URL.Scheme = "http"
			case "other_host":
				req.URL.Host = "api.anthropic.com.invalid"
			case "other_path":
				req.URL.Path = "/v1/messages/count_tokens"
			}
			gateway.rememberClaudeTelemetry(c, session, req, telemetryTestWire(), account, &ForwardResult{}, nil, time.Now(), 200, 1, proxy, nil)
			want := 0
			if variant == "valid" {
				want = 1
			}
			if len(gateway.claudeTelemetry.observed) != want {
				t.Fatal("错误身份或出站范围进入了遥测缓存")
			}
		})
	}
}
