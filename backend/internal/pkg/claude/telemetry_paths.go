package claude

import "strings"

// Claude Code 的 Datadog 日志使用 /api/v2/logs，而 OTLP 使用 /v1/logs。
// 注册路由和日志豁免必须共享清单；/v1 前缀兼容带版本的网关 base URL。
var discardedTelemetryPaths = [...]string{
	"/api/claude_code/metrics", "/v1/api/claude_code/metrics",
	"/api/v2/rum", "/v1/api/v2/rum",
	"/api/v2/logs", "/v1/api/v2/logs",
	"/v1/logs", "/v1/metrics", "/v1/traces",
}

func DiscardedTelemetryPaths() []string {
	return append([]string(nil), discardedTelemetryPaths[:]...)
}

// IsTelemetryBatchPath 仅匹配明确的事件入口，供免计费鉴权复用；不能放宽模型路由。
func IsTelemetryBatchPath(path string) bool {
	switch path {
	case "/api/event_logging/batch", "/api/event_logging/v2/batch", "/v1/api/event_logging/batch", "/v1/api/event_logging/v2/batch":
		return true
	}
	return false
}

// IsTelemetryPath 同时涵盖未知版本，未知载荷只能在本地丢弃。
func IsTelemetryPath(path string) bool {
	if strings.HasPrefix(path, "/api/event_logging/") || strings.HasPrefix(path, "/v1/api/event_logging/") {
		return true
	}
	for _, discarded := range discardedTelemetryPaths {
		if path == discarded {
			return true
		}
	}
	return false
}
