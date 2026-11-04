package service

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var menuPathPattern = regexp.MustCompile(`^/[a-zA-Z0-9/_-]{0,160}$`)

// Menu visibility only controls navigation. Authentication and feature guards
// remain authoritative for every route and API.
func ValidateMenuVisibility(value map[string]bool) error {
	if len(value) > 160 {
		return fmt.Errorf("too many menu visibility entries (max 160)")
	}
	for path := range value {
		if !menuPathPattern.MatchString(path) || strings.Contains(path, "//") {
			return fmt.Errorf("invalid menu path")
		}
	}
	return nil
}

func ParseMenuVisibility(raw string) map[string]bool {
	value := map[string]bool{}
	if json.Unmarshal([]byte(raw), &value) != nil || value == nil || ValidateMenuVisibility(value) != nil {
		return map[string]bool{}
	}
	return value
}

// ValidateMenuLabels accepts only stable local menu identities and plain text.
func ValidateMenuLabels(value map[string]string) error {
	if len(value) > 160 {
		return fmt.Errorf("菜单名称最多设置 160 项")
	}
	for path, label := range value {
		if !menuPathPattern.MatchString(path) || strings.Contains(path, "//") {
			return fmt.Errorf("菜单标识无效")
		}
		if !utf8.ValidString(label) || utf8.RuneCountInString(label) > 50 {
			return fmt.Errorf("菜单名称最多 50 字")
		}
		for _, c := range label {
			if unicode.IsControl(c) {
				return fmt.Errorf("菜单名称不能包含控制字符")
			}
		}
	}
	return nil
}
func ParseMenuLabels(raw string) map[string]string {
	value := map[string]string{}
	if json.Unmarshal([]byte(raw), &value) != nil || ValidateMenuLabels(value) != nil {
		return map[string]string{}
	}
	for path, label := range value {
		label = strings.TrimSpace(label)
		if label == "" {
			delete(value, path)
		} else {
			value[path] = label
		}
	}
	return value
}
func PublicMenuLabels(labels map[string]string) map[string]string {
	result := map[string]string{}
	for path, label := range labels {
		if !strings.HasPrefix(path, "/admin") {
			result[path] = label
		}
	}
	return result
}
