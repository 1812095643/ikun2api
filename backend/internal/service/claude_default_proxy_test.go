//go:build unit

package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/oauth"
)

func TestLocalClaudeDefaultProxyScope(t *testing.T) {
	t.Setenv(claudeDefaultProxyEnv, "2")
	explicit := int64(9)
	for _, test := range []struct {
		name, platform, accountType string
		selected                    *int64
		want                        int64
	}{
		{"claude_oauth", PlatformAnthropic, AccountTypeOAuth, nil, 2},
		{"claude_setup", PlatformAnthropic, AccountTypeSetupToken, nil, 2},
		{"claude_api", PlatformAnthropic, AccountTypeAPIKey, nil, 0},
		{"openai_oauth", PlatformOpenAI, AccountTypeOAuth, nil, 0},
		{"explicit_proxy", PlatformAnthropic, AccountTypeOAuth, &explicit, 9},
	} {
		t.Run(test.name, func(t *testing.T) {
			account, err := buildAccountForCreate(&CreateAccountInput{Platform: test.platform, Type: test.accountType, ProxyID: test.selected}, map[string]any{})
			if err != nil {
				t.Fatal(err)
			}
			got := int64(0)
			if account.ProxyID != nil {
				got = *account.ProxyID
			}
			if got != test.want {
				t.Fatalf("默认代理范围不正确：got=%d want=%d", got, test.want)
			}
		})
	}
}

func TestLocalClaudeOAuthUsesSameDefaultProxy(t *testing.T) {
	t.Setenv(claudeDefaultProxyEnv, "2")
	proxy := &Proxy{ID: 2, Protocol: "socks5", Host: "proxy.example.com", Port: 443, Status: StatusActive}
	repo := &mockProxyRepoForOAuth{getByIDFunc: func(_ context.Context, id int64) (*Proxy, error) {
		if id != proxy.ID {
			t.Fatalf("使用了非预期代理：%d", id)
		}
		return proxy, nil
	}}
	assertProxy := func(got string) {
		if got != proxy.URL() {
			t.Fatalf("Claude 授权链路代理不一致：%q", got)
		}
	}
	client := &mockClaudeOAuthClient{
		getOrgUUIDFunc: func(_ context.Context, _, proxyURL string) (string, error) { assertProxy(proxyURL); return "org", nil },
		getAuthCodeFunc: func(_ context.Context, _, _, _, _, _, proxyURL string) (string, error) {
			assertProxy(proxyURL)
			return "code", nil
		},
		exchangeCodeFunc: func(_ context.Context, _, _, _, proxyURL string, _ bool) (*oauth.TokenResponse, error) {
			assertProxy(proxyURL)
			return &oauth.TokenResponse{AccessToken: "local-only"}, nil
		},
		refreshTokenFunc: func(_ context.Context, _, proxyURL string) (*oauth.TokenResponse, error) {
			assertProxy(proxyURL)
			return &oauth.TokenResponse{AccessToken: "local-only"}, nil
		},
	}
	svc := NewOAuthService(repo, client)
	defer svc.Stop()
	ctx := context.Background()
	generated, err := svc.GenerateAuthURL(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ExchangeCode(ctx, &ExchangeCodeInput{SessionID: generated.SessionID, Code: "local-code"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CookieAuth(ctx, &CookieAuthInput{SessionKey: "local-cookie"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RefreshAccountToken(ctx, &Account{Credentials: map[string]any{"refresh_token": "local-refresh"}}); err != nil {
		t.Fatal(err)
	}
}

func TestLocalClaudeProxyUnavailableNeverFallsBack(t *testing.T) {
	t.Setenv(claudeDefaultProxyEnv, "2")
	past := time.Now().Add(-time.Hour)
	for _, test := range []struct {
		name  string
		proxy *Proxy
		err   error
	}{
		{"lookup_error", nil, fmt.Errorf("local lookup failure")},
		{"missing", nil, nil},
		{"inactive", &Proxy{Status: "inactive"}, nil},
		{"expired", &Proxy{Status: StatusActive, ExpiresAt: &past}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := &mockProxyRepoForOAuth{getByIDFunc: func(context.Context, int64) (*Proxy, error) { return test.proxy, test.err }}
			if url, err := resolveClaudeOAuthProxyURL(context.Background(), repo, nil); err == nil || url != "" {
				t.Fatal("代理不可用时必须停止，不能直连")
			}
		})
	}
	t.Setenv(claudeDefaultProxyEnv, "invalid")
	if _, err := claudeAccountProxyForCreate(PlatformAnthropic, AccountTypeOAuth, nil); err == nil {
		t.Fatal("必须拒绝无效默认代理配置")
	}
	if _, err := claudeAccountProxyForCreate(PlatformOpenAI, AccountTypeOAuth, nil); err != nil {
		t.Fatal("Claude 默认配置不能影响 GPT")
	}
}
