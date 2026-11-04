package service

import (
	"fmt"

	"github.com/MACOS-DO/sub4api/internal/pkg/logger"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

var claudePrivacyToolOwnershipFields = map[string]bool{"verified": true, "owner": true}

// Claude Code 2.1.318 adds MCP provenance to tools and top-level tool_addition
// blocks. Keep the known attribution semantics, but never forward the client's
// server URL, directory entry or future identifying fields. Beta negotiation is
// handled separately; this privacy boundary must not enable capabilities.
func sanitizeClaudeToolOwnership(value gjson.Result) ([]byte, bool, error) {
	if value.Type == gjson.Null {
		return nil, false, nil
	}
	invalid := fmt.Errorf("Claude ownership_metadata has an unsupported structure")
	if !value.IsObject() || value.Get("verified").Type != gjson.False ||
		value.Get("owner").Type != gjson.String || value.Get("owner").String() != "mcp" {
		return nil, false, invalid
	}
	next, err := filterClaudePrivacyObject([]byte(value.Raw), claudePrivacyToolOwnershipFields, nil)
	return next, true, err
}

// Only the protocol definition is traversed. input_schema, descriptions, tool
// arguments/results and signed thinking blocks remain opaque and byte-stable.
func sanitizeClaudeToolDefinition(value gjson.Result) ([]byte, error) {
	return filterClaudePrivacyObject([]byte(value.Raw), nil, func(key string, field gjson.Result) ([]byte, bool, error) {
		switch key {
		case "ownership_metadata":
			return sanitizeClaudeToolOwnership(field)
		case "cache_control":
			// Preserve the existing auxiliary filter's malformed-block rejection.
			if (value.Get("type").String() == "thinking" || value.Get("type").String() == "redacted_thinking") && field.Type != gjson.Null {
				return nil, false, fmt.Errorf("Claude thinking blocks cannot contain cache_control")
			}
			return sanitizeClaudeCacheControl(field)
		default:
			return []byte(field.Raw), true, nil
		}
	})
}

func sanitizeClaudeToolDefinitions(value gjson.Result) ([]byte, bool, error) {
	if !value.IsArray() {
		return []byte(value.Raw), true, nil
	}
	items := make([][]byte, 0, len(value.Array()))
	changed := false
	for _, item := range value.Array() {
		if !item.IsObject() {
			items = append(items, []byte(item.Raw))
			continue
		}
		next, err := sanitizeClaudeToolDefinition(item)
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

func sanitizeClaudeToolAddition(value gjson.Result) ([]byte, bool, error) {
	next, err := filterClaudePrivacyObject([]byte(value.Raw), nil, func(key string, field gjson.Result) ([]byte, bool, error) {
		if key == "definition" {
			definition, err := sanitizeClaudeToolDefinition(field)
			return definition, true, err
		}
		return []byte(field.Raw), true, nil
	})
	return next, true, err
}

const claudeToolProvenanceBeta = "tool-provenance-2026-10-26"

// Builders call this after the privacy validator and after calculating their
// final beta header, including account overrides. Only protocol attribution is
// removed; tool definitions, arguments, results and signed blocks are untouched.
func stripClaudeToolOwnershipUnlessBeta(body []byte, finalBeta string) ([]byte, bool) {
	if anthropicBetaTokensContains(finalBeta, claudeToolProvenanceBeta) {
		return body, false
	}
	var paths []string
	tools := gjson.GetBytes(body, "tools")
	if tools.IsArray() {
		for i, tool := range tools.Array() {
			if tool.IsObject() && tool.Get("ownership_metadata").Exists() {
				paths = append(paths, fmt.Sprintf("tools.%d.ownership_metadata", i))
			}
		}
	}
	messages := gjson.GetBytes(body, "messages")
	if messages.IsArray() {
		for i, message := range messages.Array() {
			content := message.Get("content")
			if !content.IsArray() {
				continue
			}
			for j, block := range content.Array() {
				if block.Get("type").String() == "tool_addition" && block.Get("tool.definition.ownership_metadata").Exists() {
					paths = append(paths, fmt.Sprintf("messages.%d.content.%d.tool.definition.ownership_metadata", i, j))
				}
			}
		}
	}
	if len(paths) == 0 || !gjson.ValidBytes(body) {
		return body, false
	}
	next := body
	for _, path := range paths {
		var err error
		next, err = sjson.DeleteBytes(next, path)
		if err != nil {
			// Paths are fixed protocol keys plus array indices in validated
			// JSON. Keep the original body atomically on an unexpected library
			// error; private URLs were already removed at the shared boundary.
			logger.LegacyPrintf("service.gateway", "[BetaFieldSanitize] tool provenance removal failed (body len=%d)", len(body))
			return body, false
		}
	}
	return next, true
}
