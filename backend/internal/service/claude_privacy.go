package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/tidwall/gjson"
)

// 请求包络采用允许列表；新客户端增加的旁路数据不能自动获得出站权限。
// messages、tools 等业务内容保持原始 JSON，不能按 git/hash 关键词删用户代码或工具签名。
var claudePrivacyBodyFields = map[string]bool{
	"model": true, "messages": true, "max_tokens": true, "system": true,
	"stream": true, "stop_sequences": true, "temperature": true, "top_p": true, "top_k": true,
	"tools": true, "tool_choice": true, "thinking": true, "metadata": true,
	"cache_control": true, "container": true, "context_management": true,
	"output_config": true, "output_format": true, "service_tier": true, "speed": true,
	"mcp_servers": true, "inference_geo": true, "compaction": true, "thread": true,
	"fallbacks": true, "fallback_credit_token": true, "diagnostics": true,
	"safeguards": true, "anthropic_version": true, "anthropic_beta": true,
}

var claudePrivacySystemFields = map[string]bool{"type": true, "text": true, "cache_control": true, "citations": true}
var claudePrivacyCacheFields = map[string]bool{"type": true, "ttl": true, "scope": true}
var claudePrivacyMetadataFields = map[string]bool{"user_id": true}
var claudePrivacyIdentityFields = map[string]bool{"device_id": true, "account_uuid": true, "session_id": true}
var claudePrivacyDiagnosticsFields = map[string]bool{"previous_message_id": true}
var claudePrivacyOpaqueID = regexp.MustCompile(`^[A-Za-z0-9_.:-]{0,256}$`)

var claudePrivacySystemLine = regexp.MustCompile(`(?i)^\s*(?:x-anthropic-billing-header|x-claude-code-[\w-]+|x-claude-remote-[\w-]+|x-cc-[\w-]+|tengu_[\w-]+|cc_[\w-]+|cch|git[_ -]remote(?:[_ -]url)?|git[_ -]hash|git[_ -]commit|git[_ -]branch|(?:primary )?working directory|home directory|(?:repo|repository|project)[_ -](?:url|hash|id|root|path))\s*[:=]`)

// 2.1.301 抓包确认：本机环境与 Git 快照以独立 system-reminder 文本块进入 user 消息。
// 仅识别客户端的固定包装和模板；普通用户文字、引用示例、工具结果与签名保持原样。
func stripClaudePrivacyAutoContext(text string) string {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "<system-reminder>\n") && !strings.HasPrefix(trimmed, "<system-reminder>\r\n") {
		return text
	}
	if !strings.HasSuffix(trimmed, "</system-reminder>") || strings.Count(trimmed, "<system-reminder>") != 1 {
		return text
	}
	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "<system-reminder>"), "</system-reminder>"))
	inner = strings.ReplaceAll(inner, "\r\n", "\n")
	lines := strings.SplitAfter(inner, "\n")
	var out strings.Builder
	changed := false
	if strings.HasPrefix(inner, "# Environment\nYou have been invoked in the following environment:") || strings.HasPrefix(inner, "# Environment update\n") {
		skipDirectories := false
		for _, line := range lines {
			if skipDirectories && strings.HasPrefix(line, "  - ") {
				continue
			}
			skipDirectories = false
			field := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- "))
			if strings.HasPrefix(field, "Additional working directories:") || strings.HasPrefix(field, "Additional working directories added:") || strings.HasPrefix(field, "Additional working directories removed:") {
				skipDirectories, changed = true, true
				continue
			}
			if strings.HasPrefix(field, "Primary working directory:") || strings.HasPrefix(field, "Scratchpad directory:") {
				changed = true
				continue
			}
			out.WriteString(line)
		}
	} else {
		announced := strings.HasPrefix(inner, "As you answer the user's questions, you can use the following context:\n") || strings.HasPrefix(inner, "The session context has changed; these values replace the earlier ones:\n") || (strings.HasPrefix(inner, "The session context was re-read ") && strings.Contains(inner, "; these values replace the earlier ones:\n"))
		if !announced || !strings.Contains(inner, "Claude Code attached this context automatically; it isn't part of the user's message.") {
			return text
		}
		skipSection, keptSection := false, false
		for _, line := range lines {
			if strings.HasPrefix(line, "# ") {
				section := strings.TrimSpace(strings.TrimPrefix(line, "# "))
				// 这个模板只承载自动账户/项目上下文；新字段不能默认获得出站权限。
				skipSection = section != "currentDate" && section != "perforceMode"
				changed = changed || skipSection
				keptSection = keptSection || !skipSection
			}
			if strings.HasPrefix(strings.TrimSpace(line), "Claude Code attached this context automatically;") {
				skipSection = false
			}
			if !skipSection {
				out.WriteString(line)
			}
		}
		if changed && !keptSection {
			return ""
		}
	}
	if !changed {
		return text
	}
	return "<system-reminder>\n" + strings.TrimSpace(out.String()) + "\n</system-reminder>"
}

// 仅处理 system 中独立的客户端附加行和已确认的自动上下文模板。
func stripClaudePrivacySystemText(text string) string {
	text = stripClaudePrivacyAutoContext(text)
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "gitStatus: This is the git status at the start of the conversation.") {
		return ""
	}
	lines := strings.SplitAfter(text, "\n")
	var out strings.Builder
	changed := false
	for _, line := range lines {
		if claudePrivacySystemLine.MatchString(line) {
			changed = true
			continue
		}
		out.WriteString(line)
	}
	if !changed {
		return text
	}
	return out.String()
}

// 只重建发生变化的对象，保留其余字段的顺序、数字精度和字节内容。
// 拒绝重复键，防止本地与上游 JSON 解析器选择不同值而绕过过滤。
func filterClaudePrivacyObject(raw []byte, allowed map[string]bool, transform func(string, gjson.Result) ([]byte, bool, error)) ([]byte, error) {
	root := gjson.ParseBytes(raw)
	if !root.IsObject() || !gjson.ValidBytes(raw) {
		return nil, fmt.Errorf("Claude 请求包络格式不正确，请使用 JSON 对象")
	}
	seen := make(map[string]bool)
	parts := make([][]byte, 0)
	changed := false
	var transformErr error
	root.ForEach(func(key, value gjson.Result) bool {
		name := key.String()
		if seen[name] {
			transformErr = fmt.Errorf("Claude 请求包络包含重复字段，请移除重复字段后重试")
			return false
		}
		seen[name] = true
		if allowed != nil && !allowed[name] {
			changed = true
			return true
		}
		next := []byte(value.Raw)
		keep := true
		if transform != nil {
			next, keep, transformErr = transform(name, value)
			if transformErr != nil {
				return false
			}
		}
		if !keep {
			changed = true
			return true
		}
		changed = changed || string(next) != value.Raw
		encodedKey, _ := json.Marshal(name)
		part := append(encodedKey, ':')
		parts = append(parts, append(part, next...))
		return true
	})
	if transformErr != nil {
		return nil, transformErr
	}
	if !changed {
		return raw, nil
	}
	out := []byte{'{'}
	for i, part := range parts {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, part...)
	}
	return append(out, '}'), nil
}

func sanitizeClaudePrivacySystem(value gjson.Result) ([]byte, bool, error) {
	if value.Type == gjson.Null {
		return nil, false, nil
	}
	if value.Type == gjson.String {
		text := stripClaudePrivacySystemText(value.String())
		if text == value.String() {
			return []byte(value.Raw), true, nil
		}
		encoded, err := json.Marshal(text)
		return encoded, strings.TrimSpace(text) != "", err
	}
	if !value.IsArray() {
		return nil, false, fmt.Errorf("Claude system 需要是文本或文本块数组")
	}
	items := make([][]byte, 0)
	changed := false
	for _, block := range value.Array() {
		next, err := filterClaudePrivacyObject([]byte(block.Raw), claudePrivacySystemFields, func(name string, field gjson.Result) ([]byte, bool, error) {
			if name == "text" && field.Type == gjson.String {
				text := stripClaudePrivacySystemText(field.String())
				if text != field.String() {
					encoded, err := json.Marshal(text)
					return encoded, true, err
				}
			}
			if name == "cache_control" {
				return sanitizeClaudeCacheControl(field)
			}
			return []byte(field.Raw), true, nil
		})
		if err != nil {
			return nil, false, err
		}
		if gjson.GetBytes(next, "type").String() != "text" || gjson.GetBytes(next, "text").Type != gjson.String {
			return nil, false, fmt.Errorf("Claude system 只接受文本块，请检查客户端请求格式")
		}
		changed = changed || string(next) != block.Raw
		if strings.TrimSpace(gjson.GetBytes(next, "text").String()) == "" {
			changed = true
			continue
		}
		items = append(items, next)
	}
	if !changed {
		return []byte(value.Raw), true, nil
	}
	return buildJSONArrayRaw(items), len(items) > 0, nil
}

func sanitizeClaudePrivacyMessageContent(value gjson.Result) ([]byte, bool, error) {
	if value.Type == gjson.String {
		clean := stripClaudePrivacyAutoContext(value.String())
		if clean == value.String() {
			return []byte(value.Raw), true, nil
		}
		encoded, err := json.Marshal(clean)
		return encoded, clean != "", err
	}
	if !value.IsArray() {
		return []byte(value.Raw), true, nil
	}
	items := make([][]byte, 0)
	changed := false
	for _, block := range value.Array() {
		if block.Get("type").String() != "text" {
			items = append(items, []byte(block.Raw))
			continue
		}
		clean := stripClaudePrivacyAutoContext(block.Get("text").String())
		// 即使正文未变，也拒绝目标文本块的重复键，避免另一解析器读到未过滤值。
		next, err := filterClaudePrivacyObject([]byte(block.Raw), nil, func(key string, field gjson.Result) ([]byte, bool, error) {
			if key == "text" && clean != field.String() {
				encoded, err := json.Marshal(clean)
				return encoded, true, err
			}
			return []byte(field.Raw), true, nil
		})
		if err != nil {
			return nil, false, err
		}
		changed = changed || string(next) != block.Raw
		if clean == "" && block.Get("text").String() != "" {
			changed = true
			continue
		}
		items = append(items, next)
	}
	if !changed {
		return []byte(value.Raw), true, nil
	}
	return buildJSONArrayRaw(items), len(items) > 0, nil
}

func sanitizeClaudePrivacyMessages(value gjson.Result) ([]byte, bool, error) {
	if !value.IsArray() {
		return []byte(value.Raw), true, nil
	}
	items := make([][]byte, 0)
	changed := false
	for _, message := range value.Array() {
		keepMessage := true
		next, err := filterClaudePrivacyObject([]byte(message.Raw), nil, func(key string, field gjson.Result) ([]byte, bool, error) {
			if key != "content" || message.Get("role").String() != "user" {
				return []byte(field.Raw), true, nil
			}
			clean, keep, err := sanitizeClaudePrivacyMessageContent(field)
			keepMessage = keep
			return clean, true, err
		})
		if err != nil {
			return nil, false, err
		}
		changed = changed || !keepMessage || string(next) != message.Raw
		if keepMessage {
			items = append(items, next)
		}
	}
	if !changed {
		return []byte(value.Raw), true, nil
	}
	if len(items) == 0 {
		return nil, false, fmt.Errorf("Claude 请求仅包含客户端自动上下文，请补充需要处理的消息")
	}
	return buildJSONArrayRaw(items), true, nil
}

func sanitizeClaudePrivacyBody(body []byte) ([]byte, error) {
	var err error
	body, err = SanitizeClaudePrivacyAuxiliary(body)
	if err != nil {
		return nil, err
	}
	return filterClaudePrivacyObject(body, claudePrivacyBodyFields, func(name string, value gjson.Result) ([]byte, bool, error) {
		var next []byte
		var err error
		switch name {
		case "messages":
			return sanitizeClaudePrivacyMessages(value)
		case "system":
			return sanitizeClaudePrivacySystem(value)
		case "metadata":
			if !value.IsObject() {
				return nil, false, nil
			}
			next, err = filterClaudePrivacyObject([]byte(value.Raw), claudePrivacyMetadataFields, func(key string, field gjson.Result) ([]byte, bool, error) {
				if field.Type == gjson.String && strings.HasPrefix(strings.TrimSpace(field.String()), "{") {
					identity, err := filterClaudePrivacyObject([]byte(field.String()), claudePrivacyIdentityFields, func(_ string, id gjson.Result) ([]byte, bool, error) {
						return []byte(id.Raw), id.Type == gjson.String && claudePrivacyOpaqueID.MatchString(id.String()), nil
					})
					if err != nil {
						return nil, false, err
					}
					encoded, err := json.Marshal(string(identity))
					return encoded, true, err
				}
				return []byte(field.Raw), field.Type == gjson.Null || (field.Type == gjson.String && claudePrivacyOpaqueID.MatchString(field.String())), nil
			})
		case "diagnostics":
			return sanitizeClaudeDiagnostics(value)
		case "cache_control":
			return sanitizeClaudeCacheControl(value)
		case "safeguards":
			return sanitizeClaudePrivacySafeguards(value)
		default:
			return []byte(value.Raw), true, nil
		}
		return next, true, err
	})
}

// 在账号覆写和指纹注入之后再收口，禁止设置、混合大小写或新客户端头绕过允许列表。
func sanitizeClaudePrivacyHeaders(headers http.Header) {
	for key := range headers {
		name := strings.ToLower(key)
		allowed := allowedHeaders[name] || name == "authorization" || name == "x-api-key" || name == "x-goog-api-key"
		switch name {
		case "x-claude-code-agent-id", "x-claude-code-parent-agent-id", "x-claude-code-prompt-id", "x-claude-remote-container-id", "x-claude-remote-session-id":
			allowed = false
		}
		if !allowed {
			delete(headers, key)
		}
	}
}
