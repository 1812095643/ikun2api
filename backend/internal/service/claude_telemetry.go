package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/logger"
	"github.com/MACOS-DO/sub4api/internal/pkg/tlsfingerprint"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
)

const claudeTelemetryEndpoint = "https://api.anthropic.com/api/event_logging/v2/batch"

var claudeTelemetrySlots = make(chan struct{}, 16)

type claudeTelemetryDelivery struct {
	body          []byte
	authorization string
	userAgent     string
	proxyURL      string
	accountID     int64
	concurrency   int
	profile       *tlsfingerprint.Profile
	eventName     string
}

// 结构取自本机官方 Claude Code 2.1.302 的事件序列化器；只填写网关实际观测到的值。
func buildClaudeTelemetryBody(req *http.Request, wireBody []byte, account *Account, result *ForwardResult, forwardErr error, started time.Time, status, attempts int) ([]byte, string, error) {
	identity := ParseMetadataUserID(gjson.GetBytes(wireBody, "metadata.user_id").String())
	if identity == nil {
		return nil, "", fmt.Errorf("实际出站请求缺少可关联的 Claude 会话身份")
	}
	name := "tengu_api_success"
	metadata := map[string]any{
		"source":                     "ikun2api_gateway",
		"model":                      gjson.GetBytes(wireBody, "model").String(),
		"durationMs":                 time.Since(started).Milliseconds(),
		"durationMsIncludingRetries": time.Since(started).Milliseconds(),
		"attempt":                    attempts,
		"messageCount":               gjson.GetBytes(wireBody, "messages.#").Int(),
		"provider":                   "firstParty",
	}
	if result == nil || forwardErr != nil || status >= 400 || result.ClientDisconnect {
		name = "tengu_api_error"
		metadata["status"] = fmt.Sprint(status)
		// 不上传异常原文，避免其中的用户内容、代理地址或凭据进入遥测。
		errorType := "upstream_error"
		if status == 0 {
			errorType = "transport_error"
		} else if result != nil && result.ClientDisconnect {
			errorType = "client_disconnect"
		}
		metadata["errorType"] = errorType
		metadata["error"] = errorType
	} else {
		metadata["inputTokens"] = result.Usage.InputTokens
		metadata["outputTokens"] = result.Usage.OutputTokens
		metadata["cachedInputTokens"] = result.Usage.CacheReadInputTokens
		metadata["uncachedInputTokens"] = result.Usage.CacheCreationInputTokens
		metadata["cache_creation_5m_input_tokens"] = result.Usage.CacheCreation5mTokens
		metadata["cache_creation_1h_input_tokens"] = result.Usage.CacheCreation1hTokens
		if result.FirstTokenMs != nil {
			metadata["ttftMs"] = *result.FirstTokenMs
		}
	}
	if result != nil && result.RequestID != "" {
		metadata["requestId"] = result.RequestID
	}
	if id := getHeaderRaw(req.Header, "x-client-request-id"); id != "" {
		metadata["clientRequestId"] = id
	}
	encodedMetadata, err := json.Marshal(metadata)
	if err != nil {
		return nil, "", err
	}
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x64"
	}
	data := map[string]any{
		"event_name":          name,
		"event_id":            uuid.NewString(),
		"client_timestamp":    time.Now().UTC().Format(time.RFC3339Nano),
		"session_id":          identity.SessionID,
		"device_id":           identity.DeviceID,
		"model":               metadata["model"],
		"user_type":           "external",
		"is_interactive":      false,
		"client_type":         "ikun2api_gateway",
		"betas":               getHeaderRaw(req.Header, "anthropic-beta"),
		"additional_metadata": base64.StdEncoding.EncodeToString(encodedMetadata),
		"env":                 map[string]any{"platform": runtime.GOOS, "platform_raw": runtime.GOOS, "arch": arch, "is_claude_ai_auth": true, "deployment_environment": "production"},
	}
	auth := map[string]string{}
	if identity.AccountUUID != "" {
		auth["account_uuid"] = identity.AccountUUID
	}
	if org := account.GetExtraString("org_uuid"); org != "" {
		auth["organization_uuid"] = org
	} else if org = account.GetCredential("org_uuid"); org != "" {
		auth["organization_uuid"] = org
	}
	if len(auth) > 0 {
		data["auth"] = auth
	}
	body, err := json.Marshal(map[string]any{"events": []any{map[string]any{"event_type": "ClaudeCodeInternalEvent", "event_data": data}}})
	return body, name, err
}

func (s *GatewayService) enqueueClaudeTelemetry(req *http.Request, wireBody []byte, account *Account, result *ForwardResult, forwardErr error, started time.Time, status, attempts int, proxyURL string, profile *tlsfingerprint.Profile) {
	if s == nil || s.httpUpstream == nil || os.Getenv("SUB6_CLAUDE_TELEMETRY_ENABLED") != "true" || req == nil || account == nil || !account.IsAnthropicOAuthOrSetupToken() || account.IsCustomBaseURLEnabled() || req.URL == nil || req.URL.Hostname() != "api.anthropic.com" {
		return
	}
	// 仅为真正发出的 OAuth 模型请求上报；不创建启动、项目维护或后台假活动。
	// 模型请求保留了小写的线格式头，必须复用项目的大小写兼容读取。
	authorization := getHeaderRaw(req.Header, "authorization")
	if !strings.HasPrefix(authorization, "Bearer ") {
		return
	}
	body, name, err := buildClaudeTelemetryBody(req, wireBody, account, result, forwardErr, started, status, attempts)
	if err != nil {
		logger.LegacyPrintf("service.claude_telemetry", "account=%d skipped=missing_session_identity", account.ID)
		return
	}
	delivery := claudeTelemetryDelivery{body: body, authorization: authorization, userAgent: getHeaderRaw(req.Header, "User-Agent"), proxyURL: proxyURL, accountID: account.ID, concurrency: account.Concurrency, profile: profile, eventName: name}
	select {
	case claudeTelemetrySlots <- struct{}{}:
	default:
		logger.LegacyPrintf("service.claude_telemetry", "account=%d dropped=queue_full", account.ID)
		return
	}
	go func() {
		defer func() { <-claudeTelemetrySlots }()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		responseStatus, accepted, sendErr := s.sendClaudeTelemetry(ctx, delivery)
		if sendErr != nil {
			logger.LegacyPrintf("service.claude_telemetry", "account=%d event=%s status=%d accepted=%d delivery=unconfirmed", delivery.accountID, delivery.eventName, responseStatus, accepted)
			return
		}
		logger.LegacyPrintf("service.claude_telemetry", "account=%d event=%s status=%d accepted=%d delivery=accepted", delivery.accountID, delivery.eventName, responseStatus, accepted)
	}()
}

func (s *GatewayService) sendClaudeTelemetry(ctx context.Context, delivery claudeTelemetryDelivery) (int, int, error) {
	if s == nil || s.httpUpstream == nil {
		return 0, 0, fmt.Errorf("遥测传输尚未配置")
	}
	req, err := http.NewRequestWithContext(WithHTTPUpstreamRedirectsDisabled(ctx), http.MethodPost, claudeTelemetryEndpoint, bytes.NewReader(delivery.body))
	if err != nil {
		return 0, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", delivery.userAgent)
	req.Header.Set("x-service-name", "claude-code")
	req.Header.Set("Authorization", delivery.authorization)
	// 复用模型请求的代理、TLS 模板及传输实现，避免产生第二套出站身份。
	resp, err := s.httpUpstream.DoWithTLS(req, delivery.proxyURL, delivery.accountID, delivery.concurrency, delivery.profile)
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		return 0, 0, fmt.Errorf("遥测传输未完成")
	}
	if resp == nil || resp.Body == nil {
		return 0, 0, fmt.Errorf("遥测响应为空")
	}
	defer resp.Body.Close()
	var acknowledgement struct {
		AcceptedCount *int `json:"accepted_count"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<10)).Decode(&acknowledgement); err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 || acknowledgement.AcceptedCount == nil || *acknowledgement.AcceptedCount != 1 {
		return resp.StatusCode, 0, fmt.Errorf("遥测服务尚未确认接收")
	}
	return resp.StatusCode, *acknowledgement.AcceptedCount, nil
}
