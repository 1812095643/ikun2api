package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/config"
)

func TestOpenAIWSPoolAffinitySeparatesLanesAndPreservesCredentialRefresh(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 4
	cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
	cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 4
	pool := newOpenAIWSConnPool(cfg)
	defer pool.Close()
	pool.setClientDialerForTest(&openAIWSCountingDialer{})
	account := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{"chatgpt_account_id": "account-a", "access_token": "old"}}
	request := openAIWSAcquireRequest{Account: account, WSURL: "wss://example.com/responses",
		Headers: http.Header{"Authorization": {"Bearer old"}, "Thread-Id": {"thread"}}, ExecutionScope: "thread:turn"}
	first, err := pool.Acquire(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	firstID := first.ConnID()
	first.Release()
	memory := request
	memory.ExecutionScope = "thread:memory"
	second, err := pool.Acquire(context.Background(), memory)
	if err != nil {
		t.Fatal(err)
	}
	if second.ConnID() == firstID {
		t.Fatal("后台通道借用了主线程的连接")
	}
	second.Release()
	refreshed := *account
	refreshed.Credentials = map[string]any{"chatgpt_account_id": "account-a", "access_token": "new"}
	request.Account = &refreshed
	request.Headers = http.Header{"Authorization": {"Bearer new"}, "Thread-Id": {"thread"}}
	third, err := pool.Acquire(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if third.ConnID() != firstID {
		t.Fatal("同一账号的授权刷新不应丢失原连接")
	}
	third.Release()
	changed := refreshed
	changed.Credentials = map[string]any{"chatgpt_account_id": "account-b", "access_token": "another"}
	request.Account = &changed
	fourth, err := pool.Acquire(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if fourth.ConnID() == firstID {
		t.Fatal("重新登录到另一上游账号后仍在借用旧连接")
	}
	fourth.Release()
	if sameOpenAIWSPrewarmTarget(request, memory) {
		t.Fatal("预连接目标忽略了执行通道或账号变化")
	}
}

func TestOpenAIWSPoolAffinityKeepsUsedConnectionBeforeSpeculativeIdle(t *testing.T) {
	for _, reason := range []string{"capacity", "max_idle"} {
		t.Run(reason, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 2
			cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
			cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 2
			pool := newOpenAIWSConnPool(cfg)
			defer pool.Close()
			pool.setClientDialerForTest(&openAIWSCountingDialer{})
			account := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
			request := openAIWSAcquireRequest{Account: account, WSURL: "wss://example.com/responses", Headers: http.Header{}, ExecutionScope: "thread-a"}
			used, err := pool.Acquire(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			if err := used.WriteJSON(map[string]any{"type": "response.create"}, time.Second); err != nil {
				t.Fatal(err)
			}
			usedID := used.ConnID()
			used.conn.lastUsedNano.Store(time.Now().Add(-time.Second).UnixNano())
			used.Release()
			spareRequest := request
			spareRequest.ForceNewConn = true
			spare, err := pool.Acquire(context.Background(), spareRequest)
			if err != nil {
				t.Fatal(err)
			}
			spareID := spare.ConnID()
			spare.Release()
			if reason == "capacity" {
				other := request
				other.ExecutionScope = "thread-b"
				lease, err := pool.Acquire(context.Background(), other)
				if err != nil {
					t.Fatal(err)
				}
				lease.Release()
			} else {
				cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
				ap := pool.getOrCreateAccountPool(account.ID)
				ap.mu.Lock()
				evicted := pool.cleanupAccountLocked(ap, time.Now(), 2)
				ap.mu.Unlock()
				closeOpenAIWSConns(evicted)
			}
			ap := pool.getOrCreateAccountPool(account.ID)
			ap.mu.Lock()
			_, keptUsed := ap.conns[usedID]
			_, keptSpare := ap.conns[spareID]
			ap.mu.Unlock()
			if !keptUsed || keptSpare {
				t.Fatalf("未发请求的预连接挤掉了已有上下文连接：used=%v spare=%v", keptUsed, keptSpare)
			}
		})
	}
}
