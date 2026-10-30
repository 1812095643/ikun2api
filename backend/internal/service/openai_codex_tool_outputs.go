package service

import (
	"encoding/json"
	"strings"

	"github.com/tidwall/gjson"
)

type openAICodexToolPair struct {
	kind   string
	callID string
}

func openAICodexToolPairKey(item json.RawMessage, output bool) (openAICodexToolPair, bool) {
	id := gjson.GetBytes(item, "call_id")
	if id.Type != gjson.String || strings.TrimSpace(id.String()) == "" {
		return openAICodexToolPair{}, false
	}
	return openAICodexToolPairFromFields(gjson.GetBytes(item, "type").String(), id.String(), output)
}

func openAICodexToolPairFromFields(kind, callID string, output bool) (openAICodexToolPair, bool) {
	if strings.TrimSpace(callID) == "" {
		return openAICodexToolPair{}, false
	}
	if output {
		if kind != "function_call_output" && kind != "custom_tool_call_output" {
			return openAICodexToolPair{}, false
		}
	} else {
		switch kind {
		case "function_call", "local_shell_call":
			kind = "function_call_output"
		case "custom_tool_call":
			kind = "custom_tool_call_output"
		default:
			return openAICodexToolPair{}, false
		}
	}
	return openAICodexToolPair{kind: kind, callID: callID}, true
}

// 只修补已提交历史中的缺失配对，沿用客户端的 aborted 标记，不伪造成功结果。
// prior 仅允许传入同账号、同执行作用域且 previous_response_id 匹配的既有缓存。
func completeOpenAICodexToolOutputs(items, prior []json.RawMessage) ([]json.RawMessage, int) {
	outputs := make(map[openAICodexToolPair]struct{})
	currentCalls := make(map[openAICodexToolPair]struct{})
	for _, sequence := range [][]json.RawMessage{prior, items} {
		for _, item := range sequence {
			if key, ok := openAICodexToolPairKey(item, true); ok {
				outputs[key] = struct{}{}
			}
		}
	}
	for _, item := range items {
		if key, ok := openAICodexToolPairKey(item, false); ok {
			currentCalls[key] = struct{}{}
		}
	}
	makeOutput := func(key openAICodexToolPair) json.RawMessage {
		output, _ := json.Marshal(struct {
			Type   string `json:"type"`
			CallID string `json:"call_id"`
			Output string `json:"output"`
		}{Type: key.kind, CallID: key.callID, Output: "aborted"})
		outputs[key] = struct{}{}
		return output
	}
	var repaired []json.RawMessage
	added := 0
	// 已知续接的调用在上一响应中，结果应放在本轮增量输入的前面。
	for _, item := range prior {
		key, ok := openAICodexToolPairKey(item, false)
		if !ok {
			continue
		}
		_, present := outputs[key]
		_, repeated := currentCalls[key]
		if !present && !repeated {
			repaired = append(repaired, makeOutput(key))
			added++
		}
	}
	for index, item := range items {
		if repaired != nil {
			repaired = append(repaired, item)
		}
		key, ok := openAICodexToolPairKey(item, false)
		if !ok {
			continue
		}
		if _, present := outputs[key]; present {
			continue
		}
		if repaired == nil {
			repaired = append(make([]json.RawMessage, 0, len(items)+1), items[:index+1]...)
		}
		repaired = append(repaired, makeOutput(key))
		added++
	}
	if added == 0 {
		return items, 0
	}
	return repaired, added
}

func completeOpenAICodexRequestTools(raw []byte) ([]byte, error) {
	// 未知锚点不能用于恢复隐藏历史，但当前 input 内明确出现的调用仍需配对。
	if !gjson.ValidBytes(raw) || !gjson.GetBytes(raw, "input").IsArray() {
		return raw, nil
	}
	items, _, err := openAIWSExtractNormalizedInputSequence(raw)
	if err != nil {
		return nil, err
	}
	repaired, count := completeOpenAICodexToolOutputs(items, nil)
	if count == 0 {
		return raw, nil
	}
	return setOpenAIWSPayloadInputSequence(raw, repaired, true)
}

func completeOpenAICodexRequestToolFields(body map[string]any) (bool, error) {
	input, ok := body["input"].([]any)
	if !ok {
		return false, nil
	}
	// 复用已有 map，避免对包含图片或长工具结果的整份历史再次编码解码。
	keyFor := func(value any, output bool) (openAICodexToolPair, bool) {
		item, ok := value.(map[string]any)
		if !ok {
			return openAICodexToolPair{}, false
		}
		kind, _ := item["type"].(string)
		callID, _ := item["call_id"].(string)
		return openAICodexToolPairFromFields(kind, callID, output)
	}
	outputs := make(map[openAICodexToolPair]struct{})
	for _, value := range input {
		if key, ok := keyFor(value, true); ok {
			outputs[key] = struct{}{}
		}
	}
	var repaired []any
	for index, value := range input {
		if repaired != nil {
			repaired = append(repaired, value)
		}
		key, ok := keyFor(value, false)
		if !ok {
			continue
		}
		if _, exists := outputs[key]; exists {
			continue
		}
		if repaired == nil {
			repaired = append(make([]any, 0, len(input)+1), input[:index+1]...)
		}
		repaired = append(repaired, map[string]any{"type": key.kind, "call_id": key.callID, "output": "aborted"})
		outputs[key] = struct{}{}
	}
	if repaired == nil {
		return false, nil
	}
	body["input"] = repaired
	return true, nil
}

// prior 只能来自当前已认证连接的上一轮，并且必须由响应锚点确认归属。
func completeOpenAICodexContinuationTools(raw []byte, prior []json.RawMessage, expectedPreviousID string) ([]byte, bool, error) {
	previousID := strings.TrimSpace(gjson.GetBytes(raw, "previous_response_id").String())
	if previousID == "" || expectedPreviousID == "" || previousID != expectedPreviousID {
		// 不读取归属不明的缓存；当前请求已提交的调用仍按自身内容补齐。
		prior = nil
	}
	input := gjson.GetBytes(raw, "input")
	if input.Exists() && !input.IsArray() {
		return raw, false, nil
	}
	items, _, err := openAIWSExtractNormalizedInputSequence(raw)
	if err != nil {
		return nil, false, err
	}
	repaired, count := completeOpenAICodexToolOutputs(items, prior)
	if count == 0 {
		return raw, false, nil
	}
	updated, err := setOpenAIWSPayloadInputSequence(raw, repaired, true)
	return updated, err == nil, err
}
