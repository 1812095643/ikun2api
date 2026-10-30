package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/tlsfingerprint"
	"github.com/tidwall/gjson"
)

type telemetrySchedulerKeyRepo struct {
	APIKeyRepository
	key *APIKey
	err error
}

func (r *telemetrySchedulerKeyRepo) GetByID(context.Context, int64) (*APIKey, error) {
	return r.key, r.err
}

func telemetrySchedulerKey() *APIKey {
	return &APIKey{ID: 17, UserID: 23, Status: StatusActive, User: &User{ID: 23, Status: StatusActive}}
}

func TestClaudeTelemetryTimerAndIngressClaimOnce(t *testing.T) {
	var buffer claudeTelemetryBuffer
	now := time.Now()
	trigger := telemetryTestBatch(t, "tengu_api_success").triggers[0]
	buffer.remember(17, telemetryClientSession, claudeTelemetryDelivery{eventName: trigger.eventName}, now)
	if len(buffer.claimDue(now.Add(claudeTelemetryInterval-time.Nanosecond), 16)) != 0 {
		t.Fatal("摘要未到期时不得定时发送")
	}
	var claimed atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			if index%2 == 0 {
				claimed.Add(int64(len(buffer.claimDue(now.Add(claudeTelemetryInterval), 16))))
			} else if _, ok := buffer.claim(17, trigger, now.Add(claudeTelemetryInterval)); ok {
				claimed.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if claimed.Load() != 1 {
		t.Fatal("客户端触发与定时任务竞争时重复发送了同一摘要")
	}
}

func TestClaudeTelemetryTimerKeepsNewestWithoutStarvation(t *testing.T) {
	var buffer claudeTelemetryBuffer
	now := time.Now()
	buffer.remember(17, telemetryClientSession, claudeTelemetryDelivery{eventName: "tengu_api_success", body: []byte("old")}, now)
	buffer.remember(17, telemetryClientSession, claudeTelemetryDelivery{eventName: "tengu_api_success", body: []byte("latest")}, now.Add(claudeTelemetryInterval-time.Second))
	if got := buffer.claimDue(now.Add(claudeTelemetryInterval), 0); len(got) != 0 {
		t.Fatal("发送槽位不足时不应领取摘要")
	}
	got := buffer.claimDue(now.Add(claudeTelemetryInterval), 1)
	if len(got) != 1 || string(got[0].body) != "latest" || len(buffer.claimDue(now.Add(time.Minute), 16)) != 0 {
		t.Fatal("持续请求必须按期发送最新摘要，空闲后不得重复发送")
	}
	buffer.remember(17, telemetryClientSession, claudeTelemetryDelivery{eventName: "tengu_api_success"}, now)
	if len(buffer.claimDue(now.Add(claudeTelemetryRetention), 16)) != 0 {
		t.Fatal("不能发送过期摘要")
	}
	for i := 0; i < 20; i++ {
		buffer.remember(int64(i+1), telemetryClientSession, claudeTelemetryDelivery{eventName: "tengu_api_success"}, now)
	}
	if len(buffer.claimDue(now.Add(claudeTelemetryInterval), 16)) != 16 || len(buffer.observed) != 4 {
		t.Fatal("单次定时领取必须受发送槽位限制，未领取摘要继续等待")
	}
}

type telemetrySchedulerTransport struct {
	HTTPUpstream
	sent  chan []byte
	block bool
}

func (s *telemetrySchedulerTransport) DoWithTLS(req *http.Request, proxy string, accountID int64, _ int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	if req.URL.String() != claudeTelemetryEndpoint || proxy != telemetryTestProxy || accountID != 1 || profile == nil || profile.Name != "scheduler-test" || !HTTPUpstreamRedirectsDisabled(req.Context()) {
		return nil, fmt.Errorf("unexpected telemetry route")
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	s.sent <- body
	if s.block {
		<-req.Context().Done()
		return nil, req.Context().Err()
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"accepted_count":1}`))}, nil
}

func TestClaudeTelemetrySchedulerSendsObservedSummaryAndStops(t *testing.T) {
	t.Setenv("SUB6_CLAUDE_TELEMETRY_ENABLED", "true")
	for _, block := range []bool{false, true} {
		t.Run(fmt.Sprint(block), func(t *testing.T) {
			transport := &telemetrySchedulerTransport{sent: make(chan []byte, 2), block: block}
			gateway := &GatewayService{httpUpstream: transport, identityService: &IdentityService{cache: &telemetryIdentityCache{fp: &Fingerprint{ClientID: strings.Repeat("a", 64)}}}}
			ctx, cancel := context.WithCancel(context.Background())
			scheduler := &ClaudeTelemetryScheduler{gateway: gateway, keyRepo: &telemetrySchedulerKeyRepo{key: telemetrySchedulerKey()}, ctx: ctx, cancel: cancel, done: make(chan struct{}), wake: make(chan struct{}, 1)}
			gateway.claudeTelemetryScheduler = scheduler
			go scheduler.run()
			t.Cleanup(scheduler.Stop)
			if scheduler.flush(time.Now()) != 0 {
				t.Fatal("无请求时不能制造遥测")
			}
			req, _ := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", nil)
			setHeaderRaw(req.Header, "authorization", "Bearer local-test-only")
			queuedAt := time.Now()
			gateway.rememberClaudeTelemetry(telemetryTestContext(), telemetryClientSession, req, telemetryTestWire(), &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Extra: map[string]any{"account_uuid": telemetryTestAccount}}, &ForwardResult{Usage: ClaudeUsage{InputTokens: 31, OutputTokens: 7}}, nil, queuedAt, 200, 1, telemetryTestProxy, &tlsfingerprint.Profile{Name: "scheduler-test"})
			select {
			case body := <-transport.sent:
				if time.Since(queuedAt) < claudeTelemetryInterval {
					t.Fatal("定时发送早于官方默认延迟")
				}
				data := gjson.GetBytes(body, "events.0.event_data")
				metadata, err := base64.StdEncoding.DecodeString(data.Get("additional_metadata").String())
				if err != nil || data.Get("client_type").String() != "ikun2api_gateway" || gjson.GetBytes(metadata, "outputTokens").Int() != 7 || strings.Contains(string(body)+string(metadata), "never-upload") {
					t.Fatal("定时发送必须保留真实用量与网关来源，并丢弃隐私正文")
				}
			case <-time.After(claudeTelemetryInterval + 2*time.Second):
				t.Fatal("没有客户端遥测触发时，定时任务未发送真实摘要")
			}
			if scheduler.flush(time.Now().Add(time.Minute)) != 0 || gateway.RelayClaudeTelemetry(17, telemetryTestBatch(t, "tengu_api_success")) != 0 {
				t.Fatal("定时发送后又重复领取了摘要")
			}
			stopped := make(chan struct{})
			go func() { scheduler.Stop(); close(stopped) }()
			select {
			case <-stopped:
			case <-time.After(2 * time.Second):
				t.Fatal("退出时未取消正在发送的遥测")
			}
			if _, ok := scheduler.beginDelivery(); ok {
				t.Fatal("退出后不得新增发送任务")
			}
			if len(gateway.claudeTelemetry.observed) != 0 || len(gateway.claudeTelemetry.seen) != 0 {
				t.Fatal("退出后未清理摘要和去重缓存")
			}
		})
	}
}

func TestClaudeTelemetryUsesOfficialTenSecondDelay(t *testing.T) {
	if claudeTelemetryInterval != 10*time.Second {
		t.Fatal("默认周期必须对齐本机官方 2.1.301 的 10000 毫秒")
	}
	var buffer claudeTelemetryBuffer
	now := time.Now()
	if _, pending := buffer.nextDelay(now); pending {
		t.Fatal("空队列不应启动定时器")
	}
	buffer.remember(17, telemetryClientSession, claudeTelemetryDelivery{eventName: "tengu_api_success"}, now)
	if delay, pending := buffer.nextDelay(now.Add(3 * time.Second)); !pending || delay != 7*time.Second {
		t.Fatal("必须从第一份摘要进入队列起计时，不能等待第二个固定轮询周期")
	}
	buffer.claimDue(now.Add(claudeTelemetryInterval), 16)
	if _, pending := buffer.nextDelay(now.Add(claudeTelemetryInterval)); pending {
		t.Fatal("发送完毕后应停止空闲定时器")
	}
}

func TestClaudeTelemetryScheduledOriginIsRevalidated(t *testing.T) {
	for _, variant := range []string{"valid", "quota_exhausted", "expired", "missing", "disabled", "wrong_owner", "wrong_user", "user_disabled", "user_deleted", "group_disabled", "group_changed", "group_denied", "repository_error"} {
		t.Run(variant, func(t *testing.T) {
			key := telemetrySchedulerKey()
			repo := &telemetrySchedulerKeyRepo{key: key}
			delivery := claudeTelemetryDelivery{apiKeyID: 17, userID: 23}
			groupID := int64(8)
			switch variant {
			case "quota_exhausted":
				key.Status = StatusAPIKeyQuotaExhausted
			case "expired":
				key.Status = StatusAPIKeyExpired
			case "missing":
				repo.key = nil
			case "disabled":
				key.Status = StatusAPIKeyDisabled
			case "wrong_owner":
				key.UserID++
			case "wrong_user":
				key.User.ID++
			case "user_disabled":
				key.User.Status = StatusDisabled
			case "user_deleted":
				now := time.Now()
				key.User.DeletedAt = &now
			case "group_disabled", "group_changed", "group_denied":
				key.GroupID = &groupID
				key.Group = &Group{ID: groupID, Status: StatusActive}
				delivery.groupID = groupID
				if variant == "group_disabled" {
					key.Group.Status = StatusDisabled
				}
				if variant == "group_changed" {
					delivery.groupID++
				}
				if variant == "group_denied" {
					key.Group.IsExclusive = true
				}
			case "repository_error":
				repo.err = fmt.Errorf("database unavailable")
			}
			scheduler := &ClaudeTelemetryScheduler{keyRepo: repo}
			allowed := variant == "valid" || variant == "quota_exhausted" || variant == "expired"
			if (scheduler.validateOrigin(context.Background(), delivery) == nil) != allowed {
				t.Fatal("等待期间的身份变化必须重新校验，遥测不重复计费")
			}
		})
	}
}

func TestClaudeTelemetryDisabledSchedulerClearsBufferedData(t *testing.T) {
	t.Setenv("SUB6_CLAUDE_TELEMETRY_ENABLED", "false")
	gateway := &GatewayService{httpUpstream: &localClaudeTelemetryTransport{}}
	scheduler := NewClaudeTelemetryScheduler(gateway, &telemetrySchedulerKeyRepo{key: telemetrySchedulerKey()})
	defer scheduler.Stop()
	gateway.claudeTelemetry.remember(17, telemetryClientSession, claudeTelemetryDelivery{eventName: "tengu_api_success"}, time.Now())
	if scheduler.flush(time.Now().Add(time.Minute)) != 0 || len(gateway.claudeTelemetry.observed) != 0 {
		t.Fatal("关闭开关后必须丢弃待发摘要")
	}
}

func TestClaudeTelemetryRefusesDirectEgress(t *testing.T) {
	transport := &localClaudeTelemetryTransport{}
	gateway := &GatewayService{httpUpstream: transport}
	if _, _, err := gateway.sendClaudeTelemetry(context.Background(), claudeTelemetryDelivery{}); err == nil || transport.calls != 0 {
		t.Fatal("代理缺失时不能从服务器公网地址直连")
	}
}

type telemetrySchedulerAccountRepo struct {
	AccountRepository
	account *Account
	err     error
}

func (r *telemetrySchedulerAccountRepo) GetByID(context.Context, int64) (*Account, error) {
	return r.account, r.err
}

func TestClaudeTelemetryRevalidatesAccountAndProxyBeforeSend(t *testing.T) {
	for _, variant := range []string{"valid", "account_missing", "account_disabled", "account_uuid_changed", "proxy_missing", "proxy_disabled", "proxy_expired", "proxy_changed", "no_proxy", "key_disabled", "lookup_error"} {
		t.Run(variant, func(t *testing.T) {
			proxyID := int64(2)
			account := &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Status: StatusActive, ProxyID: &proxyID, Proxy: &Proxy{ID: proxyID, Protocol: "socks5", Host: "local-proxy", Port: 443, Status: StatusActive}, Extra: map[string]any{"account_uuid": telemetryTestAccount}}
			repo := &telemetrySchedulerAccountRepo{account: account}
			keyRepo := &telemetrySchedulerKeyRepo{key: telemetrySchedulerKey()}
			transport := &localClaudeTelemetryTransport{status: 200, response: `{"accepted_count":1}`}
			gateway := &GatewayService{httpUpstream: transport, accountRepo: repo, claudeTelemetryScheduler: &ClaudeTelemetryScheduler{keyRepo: keyRepo}}
			delivery := claudeTelemetryDelivery{body: []byte(fmt.Sprintf(`{"events":[{"event_data":{"auth":{"account_uuid":%q}}}]}`, telemetryTestAccount)), proxyURL: telemetryTestProxy, accountID: 1, apiKeyID: 17, userID: 23}
			switch variant {
			case "account_missing":
				repo.account = nil
			case "account_disabled":
				account.Status = StatusDisabled
			case "account_uuid_changed":
				account.Extra["account_uuid"] = telemetryClientSession
			case "proxy_missing":
				account.Proxy = nil
			case "proxy_disabled":
				account.Proxy.Status = StatusDisabled
			case "proxy_expired":
				expired := time.Now().Add(-time.Minute)
				account.Proxy.ExpiresAt = &expired
			case "proxy_changed":
				account.Proxy.Host = "different-proxy"
			case "no_proxy":
				account.ProxyID = nil
				account.Proxy = nil
			case "key_disabled":
				keyRepo.key.Status = StatusAPIKeyDisabled
			case "lookup_error":
				repo.err = fmt.Errorf("repository unavailable")
			}
			_, _, err := gateway.sendClaudeTelemetry(context.Background(), delivery)
			if variant == "valid" {
				if err != nil || transport.calls != 1 {
					t.Fatal("有效身份和原代理应允许发送")
				}
			} else if err == nil || transport.calls != 0 {
				t.Fatal("等待期间的账号、Key 或代理变化必须在发送前阻断")
			}
		})
	}
}
