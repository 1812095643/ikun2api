package service

import (
	"fmt"
	"regexp"

	"github.com/tidwall/gjson"
)

var claudePreviousMessageID = regexp.MustCompile(`^msg_[A-Za-z0-9_-]{1,128}$`)

func sanitizeClaudeCacheControl(value gjson.Result) ([]byte, bool, error) {
	if value.Type == gjson.Null {
		return nil, false, nil
	}
	if !value.IsObject() {
		return nil, false, fmt.Errorf("Claude cache_control must be an object")
	}
	if value.Get("type").Type != gjson.String || value.Get("type").String() != "ephemeral" {
		return nil, false, fmt.Errorf("Claude cache_control.type must be ephemeral")
	}
	next, err := filterClaudePrivacyObject([]byte(value.Raw), claudePrivacyCacheFields, func(key string, v gjson.Result) ([]byte, bool, error) {
		if v.Type == gjson.Null && key != "type" {
			return nil, false, nil
		}
		if v.Type != gjson.String {
			return nil, false, fmt.Errorf("Claude cache_control.%s must be a string", key)
		}
		switch key {
		case "ttl":
			if v.String() != "5m" && v.String() != "1h" {
				return nil, false, fmt.Errorf("Claude cache_control.ttl must be 5m or 1h")
			}
		case "scope":
			if v.String() != "org" && v.String() != "global" {
				return nil, false, fmt.Errorf("Claude cache_control.scope must be org or global")
			}
		}
		return []byte(v.Raw), true, nil
	})
	return next, true, err
}

func sanitizeClaudeDiagnostics(value gjson.Result) ([]byte, bool, error) {
	if value.Type == gjson.Null {
		return nil, false, nil
	}
	if !value.IsObject() {
		return nil, false, fmt.Errorf("Claude diagnostics must be an object")
	}
	next, err := filterClaudePrivacyObject([]byte(value.Raw), claudePrivacyDiagnosticsFields, func(_ string, v gjson.Result) ([]byte, bool, error) {
		if v.Type == gjson.Null {
			return nil, false, nil
		}
		if v.Type != gjson.String || !claudePreviousMessageID.MatchString(v.String()) {
			return nil, false, fmt.Errorf("Claude diagnostics.previous_message_id must be a message ID")
		}
		return []byte(v.Raw), true, nil
	})
	return next, true, err
}

// Traverse protocol cache locations only. Tool inputs, JSON schemas, thinking
// text/signatures and other user data are opaque, even if they use these names.
func sanitizeClaudeCacheArray(value gjson.Result, content bool, depth int) ([]byte, bool, error) {
	if !value.IsArray() {
		return []byte(value.Raw), true, nil
	}
	if depth > 32 {
		return nil, false, fmt.Errorf("Claude content nesting is too deep")
	}
	items := make([][]byte, 0, len(value.Array()))
	changed := false
	for _, item := range value.Array() {
		if !item.IsObject() {
			items = append(items, []byte(item.Raw))
			continue
		}
		next, err := filterClaudePrivacyObject([]byte(item.Raw), nil, func(key string, field gjson.Result) ([]byte, bool, error) {
			if key == "cache_control" {
				if item.Get("type").String() == "thinking" || item.Get("type").String() == "redacted_thinking" {
					if field.Type != gjson.Null {
						return nil, false, fmt.Errorf("Claude thinking blocks cannot contain cache_control")
					}
				}
				return sanitizeClaudeCacheControl(field)
			}
			if content && key == "content" {
				return sanitizeClaudeCacheArray(field, true, depth+1)
			}
			return []byte(field.Raw), true, nil
		})
		if err != nil {
			return nil, false, err
		}
		changed = changed || string(next) != item.Raw
		items = append(items, next)
	}
	if !changed {
		return []byte(value.Raw), true, nil
	}
	return buildJSONArrayRaw(items), true, nil
}

// Runs before accounting/audit as well as at every outbound builder. Invalid
// structures stop locally; they are never serialized or echoed in errors.
func SanitizeClaudePrivacyAuxiliary(body []byte) ([]byte, error) {
	return filterClaudePrivacyObject(body, nil, func(key string, field gjson.Result) ([]byte, bool, error) {
		switch key {
		case "cache_control":
			return sanitizeClaudeCacheControl(field)
		case "diagnostics":
			return sanitizeClaudeDiagnostics(field)
		case "tools", "system":
			return sanitizeClaudeCacheArray(field, false, 0)
		case "messages":
			return sanitizeClaudeCacheArray(field, true, 0)
		default:
			return []byte(field.Raw), true, nil
		}
	})
}
