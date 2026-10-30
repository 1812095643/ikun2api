package service

import (
	"errors"

	"github.com/tidwall/gjson"
)

var errClaudePrivacySafetyShape = errors.New("Claude 安全检查上下文包含尚未支持的结构，已停止转发；请更新客户端或改用本地逐次授权模式")
var errClaudePrivacySafetyPaths = errors.New("Claude 安全检查依赖私有目录或浏览器上下文，无法在脱敏后保持原始权限判断，已停止转发；请改用本地逐次授权模式")

// SanitizeClaudePrivacySafeguards 在选号、计费和正文审计之前处理安全上下文。
// 不提前改写其余业务内容，避免影响 Claude Code 识别与现有会话机制。
func SanitizeClaudePrivacySafeguards(body []byte) ([]byte, error) {
	var err error
	body, err = SanitizeClaudePrivacyAuxiliary(body)
	if err != nil {
		return nil, err
	}
	if !gjson.GetBytes(body, "safeguards").Exists() {
		return body, nil
	}
	return filterClaudePrivacyObject(body, nil, func(name string, value gjson.Result) ([]byte, bool, error) {
		if name == "safeguards" {
			return sanitizeClaudePrivacySafeguards(value)
		}
		return []byte(value.Raw), true, nil
	})
}

func sanitizeClaudePrivacySafeguards(value gjson.Result) ([]byte, bool, error) {
	if value.IsObject() {
		clean, err := sanitizeClaudePrivacySafeguard(value, false)
		return clean, true, err
	}
	if !value.IsArray() {
		return nil, false, errClaudePrivacySafetyShape
	}
	items := make([][]byte, 0)
	changed := false
	for _, entry := range value.Array() {
		clean, err := sanitizeClaudePrivacySafeguard(entry, true)
		if err != nil {
			return nil, false, err
		}
		changed = changed || string(clean) != entry.Raw
		items = append(items, clean)
	}
	if !changed {
		return []byte(value.Raw), true, nil
	}
	return buildJSONArrayRaw(items), true, nil
}

func sanitizeClaudePrivacySafeguard(value gjson.Result, requireType bool) ([]byte, error) {
	if requireType && value.Get("type").String() != "dangerous_tool_use" {
		return nil, errClaudePrivacySafetyShape
	}
	return filterClaudePrivacySafetyObject(value, func(name string, field gjson.Result) ([]byte, bool, error) {
		switch name {
		case "type":
			return claudePrivacySafetyField(field, field.Type == gjson.String && field.String() == "dangerous_tool_use")
		case "enabled":
			return claudePrivacySafetyField(field, claudePrivacySafetyBool(field))
		case "classifier_context":
			clean, err := sanitizeClaudePrivacyClassifierContext(field)
			return clean, true, err
		default:
			// 未知安全策略不能被静默删除，也不能原样放行。
			return nil, false, errClaudePrivacySafetyShape
		}
	})
}

func sanitizeClaudePrivacyClassifierContext(value gjson.Result) ([]byte, error) {
	return filterClaudePrivacySafetyObject(value, func(name string, field gjson.Result) ([]byte, bool, error) {
		switch name {
		case "live_cwd", "home_dir", "git_state":
			// 整块丢弃自动采集的 Git 数据，包含其嵌套的 remote、分支、文件名及哈希。
			return nil, false, nil
		case "user_identity":
			// 客户端 schema 接受 null；不能把客户端身份误当作网关账号身份转发。
			return []byte("null"), true, nil
		case "rule_roots":
			return claudePrivacySafetyEmpty(field, true)
		case "trusted_directories":
			clean, err := sanitizeClaudePrivacyTrustedDirectories(field)
			return clean, true, err
		case "chrome_navigation":
			clean, err := sanitizeClaudePrivacyChromeNavigation(field)
			return clean, true, err
		case "rules":
			clean, err := sanitizeClaudePrivacySafetyRules(field)
			return clean, true, err
		case "auto_mode":
			clean, err := filterClaudePrivacySafetyObject(field, func(key string, item gjson.Result) ([]byte, bool, error) {
				switch key {
				case "allow", "soft_deny", "hard_deny", "environment":
					return claudePrivacySafetyField(item, claudePrivacySafetyStringArray(item))
				default:
					return nil, false, errClaudePrivacySafetyShape
				}
			})
			return clean, true, err
		case "permission_mode", "platform":
			return claudePrivacySafetyField(field, field.Type == gjson.String && claudePrivacyOpaqueID.MatchString(field.String()))
		case "case_insensitive_paths", "artifact_consent_holdback", "restricted", "is_remote_mode", "classify_all_shell", "mcp_allow_rules_exact", "truncated", "tool_host_attached":
			return claudePrivacySafetyField(field, claudePrivacySafetyBool(field))
		case "v":
			return claudePrivacySafetyField(field, field.Type == gjson.Number && field.Num == 1)
		case "judge_only_tools", "shed":
			return claudePrivacySafetyField(field, claudePrivacySafetyStringArray(field))
		case "dropped":
			clean, err := filterClaudePrivacySafetyObject(field, func(key string, count gjson.Result) ([]byte, bool, error) {
				switch key {
				case "allow", "deny", "ask", "auto_mode_allow", "auto_mode_soft_deny", "auto_mode_hard_deny", "auto_mode_environment":
					return claudePrivacySafetyField(count, count.Type == gjson.Number && count.Num >= 0 && count.Num == float64(count.Int()))
				default:
					return nil, false, errClaudePrivacySafetyShape
				}
			})
			return clean, true, err
		case "prior_turn_context", "system_reminders":
			return claudePrivacySafetyEmpty(field, false)
		case "artifacts":
			return claudePrivacySafetyEmpty(field, true)
		default:
			return nil, false, errClaudePrivacySafetyShape
		}
	})
}

func sanitizeClaudePrivacyTrustedDirectories(value gjson.Result) ([]byte, error) {
	return filterClaudePrivacySafetyObject(value, func(name string, field gjson.Result) ([]byte, bool, error) {
		switch name {
		case "primary":
			// 这些路径定义了权限边界，删掉或伪造路径都可能改变判断，因此只能拒绝。
			return nil, false, errClaudePrivacySafetyPaths
		case "additional", "network":
			return claudePrivacySafetyEmpty(field, false)
		case "block_reads_outside_working_directories":
			return claudePrivacySafetyField(field, claudePrivacySafetyBool(field))
		default:
			return nil, false, errClaudePrivacySafetyShape
		}
	})
}

func sanitizeClaudePrivacyChromeNavigation(value gjson.Result) ([]byte, error) {
	return filterClaudePrivacySafetyObject(value, func(name string, field gjson.Result) ([]byte, bool, error) {
		switch name {
		case "enabled":
			return claudePrivacySafetyField(field, claudePrivacySafetyBool(field))
		case "tabs":
			if !field.IsObject() {
				return nil, false, errClaudePrivacySafetyShape
			}
			return nil, false, nil
		case "baselines":
			return claudePrivacySafetyEmpty(field, true)
		default:
			return nil, false, errClaudePrivacySafetyShape
		}
	})
}

func sanitizeClaudePrivacySafetyRules(value gjson.Result) ([]byte, error) {
	return filterClaudePrivacySafetyObject(value, func(name string, field gjson.Result) ([]byte, bool, error) {
		if name != "allow" && name != "deny" && name != "ask" || !field.IsArray() {
			return nil, false, errClaudePrivacySafetyShape
		}
		for _, rule := range field.Array() {
			_, err := filterClaudePrivacySafetyObject(rule, func(key string, item gjson.Result) ([]byte, bool, error) {
				switch key {
				case "rule", "canonical", "source", "pattern":
					// 显式权限规则必须保留原文；不能按路径或 Git 关键词删改规则。
					return claudePrivacySafetyField(item, item.Type == gjson.String)
				case "root":
					if item.Type == gjson.Null || item.Type == gjson.String && item.Str == "" {
						return []byte(item.Raw), true, nil
					}
					return nil, false, errClaudePrivacySafetyPaths
				default:
					return nil, false, errClaudePrivacySafetyShape
				}
			})
			if err != nil {
				return nil, false, err
			}
		}
		return []byte(field.Raw), true, nil
	})
}

func filterClaudePrivacySafetyObject(value gjson.Result, transform func(string, gjson.Result) ([]byte, bool, error)) ([]byte, error) {
	if !value.IsObject() {
		return nil, errClaudePrivacySafetyShape
	}
	return filterClaudePrivacyObject([]byte(value.Raw), nil, transform)
}

func claudePrivacySafetyEmpty(value gjson.Result, object bool) ([]byte, bool, error) {
	if object && !value.IsObject() || !object && !value.IsArray() {
		return nil, false, errClaudePrivacySafetyShape
	}
	nonempty := false
	value.ForEach(func(_, _ gjson.Result) bool {
		nonempty = true
		return false
	})
	if nonempty {
		return nil, false, errClaudePrivacySafetyPaths
	}
	return []byte(value.Raw), true, nil
}

func claudePrivacySafetyBool(value gjson.Result) bool {
	return value.Type == gjson.True || value.Type == gjson.False
}

func claudePrivacySafetyStringArray(value gjson.Result) bool {
	if !value.IsArray() {
		return false
	}
	for _, item := range value.Array() {
		if item.Type != gjson.String {
			return false
		}
	}
	return true
}

func claudePrivacySafetyField(value gjson.Result, valid bool) ([]byte, bool, error) {
	if !valid {
		return nil, false, errClaudePrivacySafetyShape
	}
	return []byte(value.Raw), true, nil
}
