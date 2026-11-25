package service

import (
	"unsafe"

	"github.com/tidwall/gjson"
)

// replaceOpenAIRawInput copies unchanged image strings only into the final
// request, without allocating a second complete input array first.
func replaceOpenAIRawInput(body []byte, input gjson.Result, items []string) []byte {
	return replaceRawJSONArray(body, input, items)
}

// The standard decoder keeps the last duplicate key; GJSON selects the first.
// Callers validate the enclosing JSON before this scan. Decode keys only:
// GJSON.ForEach also unescapes string values, copying large prompt/tool text.
func hasDuplicateJSONObjectKeys(object gjson.Result) bool {
	if !object.IsObject() {
		return false
	}
	// Synchronous, read-only view; neither these bytes nor the key map escape.
	raw := unsafe.Slice(unsafe.StringData(object.Raw), len(object.Raw))
	seen := make(map[string]struct{})
	i := skipOpenAIWSJSONSpace(raw, 0) + 1
	for i < len(raw) {
		i = skipOpenAIWSJSONSpace(raw, i)
		if i >= len(raw) {
			return true
		}
		if raw[i] == '}' {
			return false
		}
		if raw[i] != '"' {
			return true
		}
		end := scanOpenAIWSJSONString(raw, i)
		key := gjson.Parse(object.Raw[i:end]).Str
		if _, exists := seen[key]; exists {
			return true
		}
		seen[key] = struct{}{}
		i = skipOpenAIWSJSONSpace(raw, end)
		if i >= len(raw) || raw[i] != ':' {
			return true
		}
		i = skipOpenAIWSJSONSpace(raw, i+1)
		if i >= len(raw) {
			return true
		}
		i = skipOpenAIWSJSONSpace(raw, skipOpenAIWSJSONValue(raw, i))
		if i < len(raw) && raw[i] == ',' {
			i++
		}
	}
	return true
}
