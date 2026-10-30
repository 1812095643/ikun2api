package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func isOpenAIOAuthWebSearchTool(toolType string) bool {
	return toolType == "web_search" || toolType == "web_search_2025_08_26" || strings.HasPrefix(toolType, "web_search_preview")
}

// OAuth 图片搜索降级为文字搜索，同时去掉图片专用配置，保留其余搜索设置。
func normalizeOpenAIOAuthImageSearchTool(tool map[string]any) bool {
	toolType, _ := tool["type"].(string)
	if !isOpenAIOAuthWebSearchTool(toolType) {
		return false
	}
	changed := false
	switch values := tool["search_content_types"].(type) {
	case []any:
		filtered := make([]any, 0, len(values))
		for _, value := range values {
			if text, ok := value.(string); ok && text == "image" {
				changed = true
				continue
			}
			filtered = append(filtered, value)
		}
		if changed {
			if len(filtered) == 0 {
				filtered = []any{"text"}
			}
			tool["search_content_types"] = filtered
		}
	case []string:
		filtered := make([]string, 0, len(values))
		for _, value := range values {
			if value == "image" {
				changed = true
				continue
			}
			filtered = append(filtered, value)
		}
		if changed {
			if len(filtered) == 0 {
				filtered = []string{"text"}
			}
			tool["search_content_types"] = filtered
		}
	}
	if changed {
		delete(tool, "image_settings")
	}
	return changed
}

func normalizeOpenAIOAuthImageSearchFields(body map[string]any) bool {
	changed := false
	visit := func(values any) {
		tools, _ := values.([]any)
		for _, value := range tools {
			if tool, ok := value.(map[string]any); ok && normalizeOpenAIOAuthImageSearchTool(tool) {
				changed = true
			}
		}
	}
	visit(body["tools"])
	input, _ := body["input"].([]any)
	for _, value := range input {
		if item, ok := value.(map[string]any); ok {
			visit(item["additional_tools"])
			if item["type"] == "additional_tools" {
				visit(item["tools"])
			}
		}
	}
	return changed
}

func normalizeOpenAIOAuthImageSearchBody(body []byte) ([]byte, bool, error) {
	paths := make([]string, 0)
	collect := func(tools gjson.Result, prefix string) {
		if !tools.IsArray() {
			return
		}
		for index, tool := range tools.Array() {
			if isOpenAIOAuthWebSearchTool(tool.Get("type").String()) && tool.Get("search_content_types").IsArray() {
				paths = append(paths, fmt.Sprintf("%s.%d", prefix, index))
			}
		}
	}
	collect(gjson.GetBytes(body, "tools"), "tools")
	if input := gjson.GetBytes(body, "input"); input.IsArray() {
		for index, item := range input.Array() {
			collect(item.Get("additional_tools"), fmt.Sprintf("input.%d.additional_tools", index))
			if item.Get("type").String() == "additional_tools" {
				collect(item.Get("tools"), fmt.Sprintf("input.%d.tools", index))
			}
		}
	}
	updated := body
	changed := false
	for _, path := range paths {
		values := gjson.GetBytes(updated, path+".search_content_types").Array()
		filtered := make([]json.RawMessage, 0, len(values))
		removed := false
		for _, value := range values {
			if value.Type == gjson.String && value.String() == "image" {
				removed = true
				continue
			}
			filtered = append(filtered, json.RawMessage(value.Raw))
		}
		if !removed {
			continue
		}
		if len(filtered) == 0 {
			// 显式使用文字搜索，避免空数组或默认搜索范围重新启用图片。
			filtered = []json.RawMessage{json.RawMessage(`"text"`)}
		}
		raw, err := json.Marshal(filtered)
		if err == nil {
			updated, err = sjson.SetRawBytes(updated, path+".search_content_types", raw)
		}
		if err == nil {
			updated, err = sjson.DeleteBytes(updated, path+".image_settings")
		}
		if err != nil {
			return body, false, fmt.Errorf("normalize oauth image search: %w", err)
		}
		changed = true
	}
	return updated, changed, nil
}
