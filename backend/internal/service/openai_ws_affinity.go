package service

import "strings"

// 连接内上下文按账号凭据、线程及执行通道隔离，不能随指纹收敛开关一起关闭。
// 这里只补连接池的内部归属，不改变对外请求头或 response.create 的字段。
func normalizeOpenAIWSAcquireCompatibility(req openAIWSAcquireRequest) openAIWSHandshakeCompatibilityKey {
	key := normalizeOpenAIWSHandshakeCompatibility(req.Account, req.Headers)
	if scope := strings.TrimSpace(req.ExecutionScope); scope != "" {
		key.executionScope = scope
		key.credentialNamespace = codexAccountIdentityNamespace(req.Account)
	}
	return key
}

// 容量不足时先回收未发请求的备用连接，避免预连接挤掉已有上下文；同类仍按原 LRU 回收。
func openAIWSConnEvictionLess(left, right *openAIWSConn) bool {
	leftUsed, rightUsed := left.hasRequest.Load(), right.hasRequest.Load()
	if leftUsed != rightUsed {
		return !leftUsed
	}
	return left.lastUsedAt().Before(right.lastUsedAt())
}
