package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/claude"
	"github.com/MACOS-DO/sub4api/internal/pkg/logger"
	"github.com/MACOS-DO/sub4api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
)

const claudeTelemetryEndpoint = "https://api.anthropic.com/api/event_logging/v2/batch"

// 此处只限制等待及发送中的批次数；实际 HTTP 发送统一经过单路出口。
var claudeTelemetryPendingSlots = make(chan struct{}, 16)
var claudeTelemetryAgentVersion = regexp.MustCompile(`^claude-cli/(\d{1,3}\.\d{1,3}\.\d{1,4})(?:\s|$)`)

type claudeTelemetryDelivery struct {
	body          []byte
	authorization string
	userAgent     string
	proxyURL      string
	accountID     int64
	concurrency   int
	profile       *tlsfingerprint.Profile
	eventName     string
	apiKeyID      int64
	userID        int64
	groupID       int64
	generation    uint64
}

// 结构已核对本机 Claude Code 2.1.301 的事件序列化器；只填写网关实际观测到的值。
func buildClaudeTelemetryBody(req *http.Request, wireBody []byte, account *Account, result *ForwardResult, forwardErr error, started time.Time, status, attempts int) ([]byte, string, error) {
	if req == nil || account == nil {
		return nil, "", fmt.Errorf("遥测缺少实际出站请求")
	}
	identity := ParseMetadataUserID(gjson.GetBytes(wireBody, "metadata.user_id").String())
	if identity == nil || !claudeTelemetryUUID(identity.SessionID) || !claudeTelemetryUUID(identity.AccountUUID) || len(identity.DeviceID) != 64 {
		return nil, "", fmt.Errorf("实际出站请求缺少可关联的 Claude 会话身份")
	}
	if _, err := hex.DecodeString(identity.DeviceID); err != nil {
		return nil, "", fmt.Errorf("实际出站设备身份格式不匹配")
	}
	model := gjson.GetBytes(wireBody, "model").String()
	knownModel := false
	for _, known := range claude.DefaultModels {
		knownModel = knownModel || claude.NormalizeModelID(model) == known.ID
	}
	if !knownModel {
		return nil, "", fmt.Errorf("遥测模型不在允许列表中")
	}
	name := "tengu_api_success"
	duration := time.Since(started).Milliseconds()
	if duration < 0 {
		duration = 0
	}
	metadata := map[string]any{
		"source":                     "ikun2api_gateway",
		"model":                      model,
		"durationMs":                 duration,
		"durationMsIncludingRetries": duration,
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
	// 任意请求 ID、beta 文本和 UA 后缀都可被客户端写入隐私；遥测无需再次上传。
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
		"additional_metadata": base64.StdEncoding.EncodeToString(encodedMetadata),
		"env":                 map[string]any{"platform": runtime.GOOS, "platform_raw": runtime.GOOS, "arch": arch, "is_claude_ai_auth": true, "deployment_environment": "production"},
	}
	auth := map[string]string{}
	if identity.AccountUUID != "" {
		auth["account_uuid"] = identity.AccountUUID
	}
	if org := account.GetExtraString("org_uuid"); claudeTelemetryUUID(org) {
		auth["organization_uuid"] = org
	} else if org = account.GetCredential("org_uuid"); claudeTelemetryUUID(org) {
		auth["organization_uuid"] = org
	}
	if len(auth) > 0 {
		data["auth"] = auth
	}
	body, err := json.Marshal(map[string]any{"events": []any{map[string]any{"event_type": "ClaudeCodeInternalEvent", "event_data": data}}})
	return body, name, err
}

func (s *GatewayService) rememberClaudeTelemetry(c *gin.Context, clientSession string, req *http.Request, wireBody []byte, account *Account, result *ForwardResult, forwardErr error, started time.Time, status, attempts int, proxyURL string, profile *tlsfingerprint.Profile) {
	settings := s.telemetrySettings()
	if s == nil || s.httpUpstream == nil || !settings.Enabled || c == nil || c.Request == nil || req == nil || account == nil || !account.IsAnthropicOAuthOrSetupToken() || account.IsCustomBaseURLEnabled() || req.URL == nil || req.URL.Scheme != "https" || req.URL.Host != "api.anthropic.com" || req.URL.User != nil || req.URL.Path != "/v1/messages" {
		return
	}
	generation := uint64(0)
	if s.claudeTelemetryScheduler != nil {
		generation = s.claudeTelemetryScheduler.observationGeneration()
	}
	keyValue, _ := c.Get("api_key")
	apiKey, ok := keyValue.(*APIKey)
	if !ok || apiKey == nil || apiKey.ID <= 0 || !claudeTelemetryUUID(clientSession) {
		return
	}
	// 遥测只复用真实模型请求的代理，缺少代理时不从服务器公网地址直连。
	if strings.TrimSpace(proxyURL) == "" {
		return
	}
	// metadata 透传或指纹缓存缺失时只拦截，不把用户设备身份当成服务器身份发送。
	identity := ParseMetadataUserID(gjson.GetBytes(wireBody, "metadata.user_id").String())
	if identity == nil || s.identityService == nil || s.identityService.cache == nil || identity.AccountUUID != account.GetExtraString("account_uuid") || identity.SessionID == clientSession {
		return
	}
	fp, cacheErr := s.identityService.cache.GetFingerprint(c.Request.Context(), account.ID)
	if cacheErr != nil || fp == nil || fp.ClientID != identity.DeviceID {
		return
	}
	// 只缓存实际模型请求的脱敏摘要，由同一身份触发或定时任务领取一次。
	authorization := getHeaderRaw(req.Header, "authorization")
	if !strings.HasPrefix(authorization, "Bearer ") {
		return
	}
	body, name, err := buildClaudeTelemetryBody(req, wireBody, account, result, forwardErr, started, status, attempts)
	if err != nil {
		logger.LegacyPrintf("service.claude_telemetry", "account=%d skipped=missing_session_identity", account.ID)
		return
	}
	ua := "claude-cli/" + claude.EffectiveCLIVersion()
	delivery := claudeTelemetryDelivery{body: body, authorization: authorization, userAgent: ua, proxyURL: proxyURL, accountID: account.ID, concurrency: account.Concurrency, profile: profile, eventName: name, apiKeyID: apiKey.ID, userID: apiKey.UserID}
	if apiKey.GroupID != nil {
		delivery.groupID = *apiKey.GroupID
	}
	if s.claudeTelemetryScheduler != nil {
		s.claudeTelemetryScheduler.remember(apiKey.ID, clientSession, delivery, generation)
		select {
		case s.claudeTelemetryScheduler.wake <- struct{}{}:
		default:
		}
	} else {
		s.claudeTelemetry.remember(apiKey.ID, clientSession, delivery, time.Now(), time.Duration(settings.FlushIntervalSeconds)*time.Second)
	}
}

func (s *GatewayService) dispatchClaudeTelemetry(delivery claudeTelemetryDelivery) bool {
	if s == nil || !s.telemetrySettings().Enabled {
		return false
	}
	select {
	case claudeTelemetryPendingSlots <- struct{}{}:
	default:
		logger.LegacyPrintf("service.claude_telemetry", "account=%d dropped=queue_full", delivery.accountID)
		return false
	}
	parent := context.Background()
	if s.claudeTelemetryScheduler != nil {
		var ok bool
		parent, ok = s.claudeTelemetryScheduler.beginDelivery(delivery.generation)
		if !ok {
			<-claudeTelemetryPendingSlots
			return false
		}
	}
	go func() {
		defer func() {
			<-claudeTelemetryPendingSlots
			if s.claudeTelemetryScheduler != nil {
				s.claudeTelemetryScheduler.inflight.Done()
			}
		}()
		// 定时与客户端触发共享同一个出口；排队时间不占用 HTTP 超时。
		settings := s.telemetrySettings()
		if !settings.Enabled {
			return
		}
		release, err := claudeTelemetrySender.acquire(parent, time.Duration(settings.MinSendGapSeconds)*time.Second)
		if err != nil {
			return
		}
		defer release()
		ctx, cancel := context.WithTimeout(parent, time.Duration(s.telemetrySettings().TimeoutSeconds)*time.Second)
		defer cancel()
		responseStatus, accepted, sendErr := s.sendClaudeTelemetry(ctx, delivery)
		if sendErr != nil {
			logger.LegacyPrintf("service.claude_telemetry", "account=%d event=%s status=%d accepted=%d delivery=unconfirmed", delivery.accountID, delivery.eventName, responseStatus, accepted)
			return
		}
		logger.LegacyPrintf("service.claude_telemetry", "account=%d event=%s status=%d accepted=%d delivery=accepted", delivery.accountID, delivery.eventName, responseStatus, accepted)
	}()
	return true
}

func claudeTelemetryUUID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed != uuid.Nil && parsed.String() == value
}

func (s *GatewayService) sendClaudeTelemetry(ctx context.Context, delivery claudeTelemetryDelivery) (int, int, error) {
	if s == nil || s.httpUpstream == nil {
		return 0, 0, fmt.Errorf("遥测传输尚未配置")
	}
	if !s.telemetrySettings().Enabled || ctx.Err() != nil {
		return 0, 0, fmt.Errorf("遥测发送已关闭或取消")
	}
	if strings.TrimSpace(delivery.proxyURL) == "" {
		return 0, 0, fmt.Errorf("遥测缺少账号代理，禁止直连")
	}
	payload, err := decodeClaudeTelemetryPayload(delivery.body)
	if err != nil {
		return 0, 0, err
	}
	// 整批核对身份，不能只验证第一条后把其他账号的事件一起发送。
	accountUUID := gjson.GetBytes(payload.Events[0], "event_data.auth.account_uuid").String()
	deviceID := gjson.GetBytes(payload.Events[0], "event_data.device_id").String()
	for _, event := range payload.Events {
		if gjson.GetBytes(event, "event_data.auth.account_uuid").String() != accountUUID || gjson.GetBytes(event, "event_data.device_id").String() != deviceID {
			return 0, 0, fmt.Errorf("遥测批次包含不同账号或设备")
		}
	}
	if s.claudeTelemetryScheduler != nil {
		if err := s.claudeTelemetryScheduler.validateOrigin(ctx, delivery); err != nil {
			return 0, 0, err
		}
	}
	// 缓存摘要期间管理员可能已经停用账号或改过出口，发送前重新核对。
	if s.accountRepo != nil {
		account, err := s.accountRepo.GetByID(ctx, delivery.accountID)
		if err != nil || account == nil || !account.IsActive() || !account.IsAnthropicOAuthOrSetupToken() || account.IsCustomBaseURLEnabled() {
			return 0, 0, fmt.Errorf("遥测账号已不可用")
		}
		proxyURL := ""
		if account.ProxyID != nil {
			if account.Proxy == nil || !account.Proxy.IsActive() || account.Proxy.IsExpired(time.Now()) {
				return 0, 0, fmt.Errorf("遥测账号代理尚未加载")
			}
			proxyURL = account.Proxy.URL()
		}
		if proxyURL != delivery.proxyURL || account.GetExtraString("account_uuid") != accountUUID {
			return 0, 0, fmt.Errorf("遥测账号身份或出口已变更")
		}
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
	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, (16<<10)+1))
	if readErr != nil || len(responseBody) > 16<<10 {
		return resp.StatusCode, 0, fmt.Errorf("遥测接收回执无法读取")
	}
	responseBody, parseErr := filterClaudePrivacyObject(responseBody, map[string]bool{"accepted_count": true}, nil)
	if parseErr != nil || json.Unmarshal(responseBody, &acknowledgement) != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 || acknowledgement.AcceptedCount == nil || *acknowledgement.AcceptedCount < 0 || *acknowledgement.AcceptedCount > len(payload.Events) {
		return resp.StatusCode, 0, fmt.Errorf("遥测服务尚未确认接收")
	}
	if *acknowledgement.AcceptedCount != len(payload.Events) {
		// 无法确定部分回执对应哪些事件，不重发整批，避免重复上报。
		return resp.StatusCode, *acknowledgement.AcceptedCount, fmt.Errorf("遥测服务仅确认接收部分事件")
	}
	return resp.StatusCode, *acknowledgement.AcceptedCount, nil
}
