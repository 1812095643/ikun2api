package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/tlsfingerprint"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
)

func telemetryBatchDelivery(t *testing.T, success bool) claudeTelemetryDelivery {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", nil)
	var result *ForwardResult
	status := http.StatusTooManyRequests
	if success {
		result = &ForwardResult{Usage: ClaudeUsage{InputTokens: 31, OutputTokens: 7}}
		status = http.StatusOK
	}
	body, name, err := buildClaudeTelemetryBody(req, telemetryTestWire(), &Account{}, result, nil, time.Now(), status, 1)
	if err != nil {
		t.Fatal(err)
	}
	return claudeTelemetryDelivery{body: body, eventName: name, apiKeyID: 17, userID: 23, groupID: 1, accountID: 2, authorization: "Bearer local-only", userAgent: "claude-cli/2.1.301", proxyURL: telemetryTestProxy, concurrency: 3}
}

func TestClaudeTelemetryBatchPreservesEventsAndIsolatesOrigins(t *testing.T) {
	first, second := telemetryBatchDelivery(t, true), telemetryBatchDelivery(t, false)
	for _, variant := range []string{"same", "key", "user", "group", "account", "token", "proxy", "agent", "concurrency", "profile", "account_uuid", "device"} {
		t.Run(variant, func(t *testing.T) {
			other := second
			switch variant {
			case "key":
				other.apiKeyID++
			case "user":
				other.userID++
			case "group":
				other.groupID++
			case "account":
				other.accountID++
			case "token":
				other.authorization = "Bearer rotated-local-only"
			case "proxy":
				other.proxyURL = "socks5://different-proxy:443"
			case "agent":
				other.userAgent = "claude-cli/2.1.302"
			case "concurrency":
				other.concurrency++
			case "profile":
				other.profile = &tlsfingerprint.Profile{Name: "other"}
			case "account_uuid":
				other.body = []byte(strings.ReplaceAll(string(other.body), telemetryTestAccount, telemetryClientSession))
			case "device":
				other.body = []byte(strings.ReplaceAll(string(other.body), strings.Repeat("a", 64), strings.Repeat("b", 64)))
			}
			batches := combineClaudeTelemetryDeliveries([]claudeTelemetryDelivery{first, other})
			if variant != "same" {
				if len(batches) != 2 {
					t.Fatal("不同身份或出口不能合并")
				}
				return
			}
			if len(batches) != 1 || batches[0].eventName != "batch" {
				t.Fatal("相同身份的成功和失败摘要应合并发送")
			}
			payload, err := decodeClaudeTelemetryPayload(batches[0].body)
			if err != nil || len(payload.Events) != 2 {
				t.Fatal("批次事件数量不正确")
			}
			for i, event := range payload.Events {
				original := []claudeTelemetryDelivery{first, second}[i]
				if gjson.GetBytes(event, "event_data.event_id").String() != gjson.GetBytes(original.body, "events.0.event_data.event_id").String() || gjson.GetBytes(event, "event_data.event_name").String() != original.eventName {
					t.Fatal("合并不得改变事件 ID、结果或时间戳")
				}
				metadata, _ := base64.StdEncoding.DecodeString(gjson.GetBytes(event, "event_data.additional_metadata").String())
				if strings.Contains(string(event)+string(metadata), "never-upload") {
					t.Fatal("批量发送泄漏了原始请求内容")
				}
			}
		})
	}
}

func TestClaudeTelemetryBatchSplitsAtCountAndByteLimits(t *testing.T) {
	first := telemetryBatchDelivery(t, true)
	items := make([]claudeTelemetryDelivery, 201)
	for i := range items {
		items[i] = first
	}
	batches := combineClaudeTelemetryDeliveries(items)
	if len(batches) != 2 || gjson.GetBytes(batches[0].body, "events.#").Int() != 200 || gjson.GetBytes(batches[1].body, "events.#").Int() != 1 {
		t.Fatal("单批超过官方默认的 200 条上限")
	}
	payload, _ := decodeClaudeTelemetryPayload(first.body)
	var event map[string]any
	if err := json.Unmarshal(payload.Events[0], &event); err != nil {
		t.Fatal(err)
	}
	event["boundary_test"] = strings.Repeat("x", MaxClaudeTelemetryBodyBytes/2)
	payload.Events[0], _ = json.Marshal(event)
	first.body, _ = json.Marshal(payload)
	if len(first.body) > MaxClaudeTelemetryBodyBytes {
		t.Fatal("测试单条事件应在大小限制内")
	}
	batches = combineClaudeTelemetryDeliveries([]claudeTelemetryDelivery{first, first})
	if len(batches) != 2 {
		t.Fatal("合并后超出 256 KiB 必须分批")
	}
	for _, batch := range batches {
		if len(batch.body) > MaxClaudeTelemetryBodyBytes {
			t.Fatal("单批大小超限")
		}
	}
}

func TestClaudeTelemetryDueBatchLeavesOtherOriginsQueued(t *testing.T) {
	var buffer claudeTelemetryBuffer
	now := time.Now()
	for i := 0; i < 5; i++ {
		buffer.remember(17, uuid.NewString(), telemetryBatchDelivery(t, true), now)
	}
	future := telemetryBatchDelivery(t, true)
	buffer.remember(17, uuid.NewString(), future, now.Add(time.Second))
	other := telemetryBatchDelivery(t, true)
	other.apiKeyID = 18
	buffer.remember(18, uuid.NewString(), other, now)
	batches := buffer.claimDue(now.Add(claudeTelemetryInterval), 1)
	if len(batches) != 1 {
		t.Fatal("只应占用一个发送槽位")
	}
	got := int(gjson.GetBytes(batches[0].body, "events.#").Int())
	if got != 1 && got != 5 {
		t.Fatal("相同身份的到期事件未合并，或混入未到期事件")
	}
	if len(buffer.observed) != 7-got {
		t.Fatal("未领取的其他身份或未到期摘要不应丢失")
	}
	remaining := buffer.claimDue(now.Add(claudeTelemetryInterval+time.Second), 16)
	for _, batch := range remaining {
		got += int(gjson.GetBytes(batch.body, "events.#").Int())
	}
	if got != 7 || len(buffer.observed) != 0 {
		t.Fatal("分批领取丢失或重复了摘要")
	}
}

func TestClaudeTelemetryBatchAndIngressRaceDoesNotDuplicate(t *testing.T) {
	var buffer claudeTelemetryBuffer
	now := time.Now()
	const count = 20
	triggers := make([]claudeTelemetryTrigger, count)
	for i := range triggers {
		session := uuid.New()
		triggers[i] = claudeTelemetryTrigger{sessionID: session, eventID: uuid.New(), eventName: "tengu_api_success"}
		buffer.remember(17, session.String(), telemetryBatchDelivery(t, true), now)
	}
	var claimed atomic.Int64
	var wg sync.WaitGroup
	for _, trigger := range triggers {
		wg.Add(1)
		go func(trigger claudeTelemetryTrigger) {
			defer wg.Done()
			if _, ok := buffer.claim(17, trigger, now.Add(claudeTelemetryInterval)); ok {
				claimed.Add(1)
			}
		}(trigger)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for _, batch := range buffer.claimDue(now.Add(claudeTelemetryInterval), 16) {
			claimed.Add(gjson.GetBytes(batch.body, "events.#").Int())
		}
	}()
	wg.Wait()
	if claimed.Load() != count || len(buffer.observed) != 0 {
		t.Fatal("定时批次和客户端事件并发导致重复或遗漏")
	}
}

func TestClaudeTelemetryBatchRequiresExactAcknowledgement(t *testing.T) {
	batch := combineClaudeTelemetryDeliveries([]claudeTelemetryDelivery{telemetryBatchDelivery(t, true), telemetryBatchDelivery(t, false)})[0]
	for _, test := range []struct {
		name, response string
		accepted       int
		ok             bool
	}{
		{"complete", `{"accepted_count":2}`, 2, true},
		{"partial", `{"accepted_count":1}`, 1, false},
		{"discarded", `{"accepted_count":0}`, 0, false},
		{"missing", `{}`, 0, false},
		{"too_many", `{"accepted_count":3}`, 0, false},
		{"negative", `{"accepted_count":-1}`, 0, false},
		{"duplicate", `{"accepted_count":0,"accepted_count":2}`, 0, false},
		{"trailing", `{"accepted_count":2}{}`, 0, false},
		{"oversized", `{"accepted_count":2,"extra":"` + strings.Repeat("x", 16<<10) + `"}`, 0, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			transport := &localClaudeTelemetryTransport{status: 200, response: test.response, check: func(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) {
				body, _ := io.ReadAll(req.Body)
				if gjson.GetBytes(body, "events.#").Int() != 2 {
					t.Fatal("没有通过一个 HTTP 请求发送整批事件")
				}
			}}
			gateway := &GatewayService{httpUpstream: transport}
			_, accepted, err := gateway.sendClaudeTelemetry(context.Background(), batch)
			if accepted != test.accepted || (err == nil) != test.ok || transport.calls != 1 {
				t.Fatal("整批回执处理不正确，或未经授权重发")
			}
		})
	}
}

func TestClaudeTelemetryBatchRechecksEveryEventIdentity(t *testing.T) {
	first, second := telemetryBatchDelivery(t, true), telemetryBatchDelivery(t, false)
	payload, _ := decodeClaudeTelemetryPayload(first.body)
	other, _ := decodeClaudeTelemetryPayload(second.body)
	other.Events[0] = []byte(strings.ReplaceAll(string(other.Events[0]), telemetryTestAccount, telemetryClientSession))
	payload.Events = append(payload.Events, other.Events[0])
	first.body, _ = json.Marshal(payload)
	transport := &localClaudeTelemetryTransport{status: 200, response: `{"accepted_count":2}`}
	gateway := &GatewayService{httpUpstream: transport}
	if _, _, err := gateway.sendClaudeTelemetry(context.Background(), first); err == nil || transport.calls != 0 {
		t.Fatal("不能只检查首条事件身份")
	}
}

func TestClaudeTelemetryIngressReportsEventCountAfterBatching(t *testing.T) {
	t.Setenv("SUB6_CLAUDE_TELEMETRY_ENABLED", "true")
	sent := make(chan int, 1)
	transport := &localClaudeTelemetryTransport{status: 200, response: `{"accepted_count":2}`, check: func(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) {
		body, _ := io.ReadAll(req.Body)
		sent <- int(gjson.GetBytes(body, "events.#").Int())
	}}
	gateway := &GatewayService{httpUpstream: transport}
	var events []string
	for _, success := range []bool{true, false} {
		delivery := telemetryBatchDelivery(t, success)
		gateway.claudeTelemetry.remember(17, telemetryClientSession, delivery, time.Now())
		events = append(events, fmt.Sprintf(`{"event_type":"ClaudeCodeInternalEvent","event_data":{"event_name":%q,"session_id":%q,"event_id":%q}}`, delivery.eventName, telemetryClientSession, uuid.NewString()))
	}
	batch, err := ParseClaudeTelemetryBatch([]byte(`{"events":[` + strings.Join(events, ",") + `]}`))
	if err != nil {
		t.Fatal(err)
	}
	if gateway.RelayClaudeTelemetry(17, batch) != 2 {
		t.Fatal("queued_count 应计事件数，不能误报为 HTTP 批次数")
	}
	select {
	case count := <-sent:
		if count != 2 {
			t.Fatal("客户端触发未合并整批")
		}
	case <-time.After(claudeTelemetryMinSendGap + 2*time.Second):
		t.Fatal("未发送批次")
	}
	if gateway.RelayClaudeTelemetry(17, batch) != 0 {
		t.Fatal("客户端重放导致整批重复发送")
	}
}
