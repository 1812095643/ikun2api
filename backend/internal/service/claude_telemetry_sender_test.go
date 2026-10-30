package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/tlsfingerprint"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
)

func TestClaudeTelemetrySerialSenderCancellation(t *testing.T) {
	sender := claudeTelemetrySerialSender{slot: make(chan struct{}, 1)}
	release, err := sender.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := sender.acquire(ctx); err != context.Canceled {
		t.Fatal("取消的排队任务不应取得发送槽")
	}
	release()
	deadline := sender.nextAt
	ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := sender.acquire(ctx); err != context.DeadlineExceeded {
		t.Fatal("冷却等待必须响应取消")
	}
	if len(sender.slot) != 0 || !sender.nextAt.Equal(deadline) {
		t.Fatal("取消等待不能泄漏发送槽或缩短冷却时间")
	}
}

type telemetrySerialObservation struct {
	accountID int64
	body      []byte
	started   time.Time
}

type telemetrySerialTransport struct {
	HTTPUpstream
	entered chan telemetrySerialObservation
	proceed chan struct{}
	active  atomic.Int64
	maximum atomic.Int64
}

func (s *telemetrySerialTransport) DoWithTLS(req *http.Request, _ string, accountID int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	active := s.active.Add(1)
	defer s.active.Add(-1)
	for previous := s.maximum.Load(); active > previous; previous = s.maximum.Load() {
		if s.maximum.CompareAndSwap(previous, active) {
			break
		}
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	s.entered <- telemetrySerialObservation{accountID: accountID, body: body, started: time.Now()}
	select {
	case <-s.proceed:
	case <-req.Context().Done():
		return nil, req.Context().Err()
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"accepted_count":%d}`, gjson.GetBytes(body, "events.#").Int())))}, nil
}

func TestClaudeTelemetryTimerAndIngressShareSingleSender(t *testing.T) {
	t.Setenv("SUB6_CLAUDE_TELEMETRY_ENABLED", "true")
	transport := &telemetrySerialTransport{entered: make(chan telemetrySerialObservation, 3), proceed: make(chan struct{})}
	gateway := &GatewayService{httpUpstream: transport}
	ctx, cancel := context.WithCancel(context.Background())
	scheduler := &ClaudeTelemetryScheduler{gateway: gateway, keyRepo: &telemetrySchedulerKeyRepo{key: telemetrySchedulerKey()}, ctx: ctx, cancel: cancel, done: make(chan struct{}), wake: make(chan struct{}, 1)}
	close(scheduler.done)
	gateway.claudeTelemetryScheduler = scheduler
	t.Cleanup(scheduler.Stop)
	now := time.Now()
	expected := make(map[int64]string)
	for index := 1; index <= 3; index++ {
		accountUUID := uuid.NewString()
		wire := []byte(strings.ReplaceAll(string(telemetryTestWire()), telemetryTestAccount, accountUUID))
		req, _ := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", nil)
		body, name, err := buildClaudeTelemetryBody(req, wire, &Account{}, &ForwardResult{Usage: ClaudeUsage{InputTokens: index * 13, OutputTokens: index * 7}}, nil, now, http.StatusOK, 1)
		if err != nil {
			t.Fatal(err)
		}
		delivery := claudeTelemetryDelivery{body: body, eventName: name, apiKeyID: 17, userID: 23, accountID: int64(index), authorization: fmt.Sprintf("Bearer local-only-%d", index), proxyURL: telemetryTestProxy}
		expected[int64(index)] = accountUUID
		session := uuid.NewString()
		if index < 3 {
			gateway.claudeTelemetry.remember(17, session, delivery, now.Add(-claudeTelemetryInterval))
			continue
		}
		if scheduler.flush(now) != 2 {
			t.Fatal("两个账号的定时摘要应分别进入有界等待队列")
		}
		gateway.claudeTelemetry.remember(17, session, delivery, now)
		batch := &ClaudeTelemetryBatch{Count: 1, triggers: []claudeTelemetryTrigger{{sessionID: uuid.MustParse(session), eventID: uuid.New(), eventName: name}}}
		if gateway.RelayClaudeTelemetry(17, batch) != 1 || gateway.RelayClaudeTelemetry(17, batch) != 0 {
			t.Fatal("客户端触发应排队一次，不能重复发送")
		}
	}
	seen := make(map[int64]bool)
	eventIDs := make(map[string]bool)
	var previousFinished time.Time
	for index := 0; index < 3; index++ {
		var received telemetrySerialObservation
		select {
		case received = <-transport.entered:
		case <-time.After(claudeTelemetryMinSendGap + 3*time.Second):
			t.Fatal("等待中的账号未依次发送")
		}
		if !previousFinished.IsZero() && received.started.Sub(previousFinished) < claudeTelemetryMinSendGap {
			t.Fatal("不同账号之间没有保留完整发送间隔")
		}
		data := gjson.GetBytes(received.body, "events.0.event_data")
		metadata, err := base64.StdEncoding.DecodeString(data.Get("additional_metadata").String())
		eventID := data.Get("event_id").String()
		if err != nil || seen[received.accountID] || eventIDs[eventID] || !claudeTelemetryUUID(eventID) || data.Get("auth.account_uuid").String() != expected[received.accountID] || gjson.GetBytes(metadata, "outputTokens").Int() != received.accountID*7 {
			t.Fatal("每个账号必须保留自己真实的身份、事件和用量，不能共用模板内容")
		}
		seen[received.accountID], eventIDs[eventID] = true, true
		select {
		case <-transport.entered:
			t.Fatal("上一批尚未完成时出现了第二个并发发送")
		case <-time.After(50 * time.Millisecond):
		}
		previousFinished = time.Now()
		transport.proceed <- struct{}{}
	}
	scheduler.Stop()
	if transport.maximum.Load() != 1 || len(seen) != 3 || len(claudeTelemetryPendingSlots) != 0 {
		t.Fatal("单路发送、账号完整性或退出清理不符合预期")
	}
	evidence, _ := json.Marshal(map[string]any{"max_concurrent": transport.maximum.Load(), "accounts": len(seen), "distinct_events": len(eventIDs), "min_gap_ms": claudeTelemetryMinSendGap.Milliseconds()})
	t.Log(string(evidence))
}

func TestClaudeTelemetryStopCancelsWaitingSenders(t *testing.T) {
	t.Setenv("SUB6_CLAUDE_TELEMETRY_ENABLED", "true")
	transport := &telemetrySerialTransport{entered: make(chan telemetrySerialObservation, 3), proceed: make(chan struct{})}
	gateway := &GatewayService{httpUpstream: transport}
	ctx, cancel := context.WithCancel(context.Background())
	scheduler := &ClaudeTelemetryScheduler{gateway: gateway, keyRepo: &telemetrySchedulerKeyRepo{key: telemetrySchedulerKey()}, ctx: ctx, cancel: cancel, done: make(chan struct{})}
	close(scheduler.done)
	gateway.claudeTelemetryScheduler = scheduler
	t.Cleanup(scheduler.Stop)
	for index := 0; index < 3; index++ {
		delivery := telemetryBatchDelivery(t, true)
		delivery.groupID = 0
		if !gateway.dispatchClaudeTelemetry(delivery) {
			t.Fatal("未能加入有界发送队列")
		}
	}
	select {
	case <-transport.entered:
	case <-time.After(claudeTelemetryMinSendGap + 3*time.Second):
		t.Fatal("首个发送未启动")
	}
	done := make(chan struct{})
	go func() { scheduler.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("退出未取消排队和在途发送")
	}
	if len(transport.entered) != 0 || len(claudeTelemetryPendingSlots) != 0 || len(claudeTelemetrySender.slot) != 0 {
		t.Fatal("退出后仍有额外发送或未释放的槽位")
	}
}
