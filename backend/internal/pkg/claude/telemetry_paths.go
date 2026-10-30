package claude

import "strings"

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
	switch path {
	case "/api/claude_code/metrics", "/api/v2/rum", "/v1/logs", "/v1/metrics", "/v1/traces":
		return true
	}
	return false
}
