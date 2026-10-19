package service

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const claudeDefaultProxyEnv = "SUB6_CLAUDE_DEFAULT_PROXY_ID"

func claudeDefaultProxyID() (*int64, error) {
	value := strings.TrimSpace(os.Getenv(claudeDefaultProxyEnv))
	if value == "" {
		return nil, nil
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return nil, fmt.Errorf("Claude 默认代理配置必须为正整数")
	}
	return &id, nil
}

// IsClaudeDefaultProxy 仅向页面公开默认标记，不公开代理凭据。
func IsClaudeDefaultProxy(proxy *Proxy) bool {
	id, err := claudeDefaultProxyID()
	return err == nil && id != nil && proxy != nil && proxy.ID == *id && proxy.IsActive() && !proxy.IsExpired(time.Now())
}

func claudeAccountProxyForCreate(platform, accountType string, proxyID *int64) (*int64, error) {
	if proxyID != nil || platform != PlatformAnthropic || (accountType != AccountTypeOAuth && accountType != AccountTypeSetupToken) {
		return proxyID, nil
	}
	return claudeDefaultProxyID()
}

func resolveClaudeOAuthProxyURL(ctx context.Context, repo ProxyRepository, proxyID *int64) (string, error) {
	if proxyID == nil {
		var err error
		proxyID, err = claudeDefaultProxyID()
		if err != nil {
			return "", err
		}
	}
	if proxyID == nil {
		return "", nil
	}
	if repo == nil {
		return "", fmt.Errorf("无法读取 Claude 代理配置")
	}
	proxy, err := repo.GetByID(ctx, *proxyID)
	if err != nil || proxy == nil || !proxy.IsActive() || proxy.IsExpired(time.Now()) {
		// 代理不可用时停止授权，避免静默切回服务器直连出口。
		return "", fmt.Errorf("Claude 代理 %d 不可用，请检查代理状态", *proxyID)
	}
	return proxy.URL(), nil
}
