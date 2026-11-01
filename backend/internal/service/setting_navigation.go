package service

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
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
