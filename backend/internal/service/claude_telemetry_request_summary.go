package service

import "github.com/tidwall/gjson"

// These are numeric facts about one actual forwarded request, not claims about
// client-side Code Mode opt-in, execution or permission decisions. History
// blocks can be replayed across requests and must not be counted as new runs.
type claudeTelemetryRequestSummary struct {
	ToolDefinitions       int  `json:"toolDefinitions"`
	InlineToolDefinitions int  `json:"inlineToolDefinitions"`
	HistoryToolCalls      int  `json:"historyToolCalls"`
	HistoryToolResults    int  `json:"historyToolResults"`
	HistoryToolErrors     int  `json:"historyToolErrors"`
	Stream                bool `json:"stream"`
}

func buildClaudeTelemetryRequestSummary(wireBody []byte) claudeTelemetryRequestSummary {
	var summary claudeTelemetryRequestSummary
	summary.Stream = gjson.GetBytes(wireBody, "stream").Type == gjson.True
	tools := gjson.GetBytes(wireBody, "tools")
	if tools.IsArray() {
		tools.ForEach(func(_, tool gjson.Result) bool {
			if tool.IsObject() {
				summary.ToolDefinitions++
			}
			return true
		})
	}
	messages := gjson.GetBytes(wireBody, "messages")
	if !messages.IsArray() {
		return summary
	}
	messages.ForEach(func(_, message gjson.Result) bool {
		content := message.Get("content")
		if !content.IsArray() {
			return true
		}
		role := message.Get("role").String()
		content.ForEach(func(_, block gjson.Result) bool {
			switch block.Get("type").String() {
			case "tool_addition":
				if block.Get("tool.definition").IsObject() {
					summary.InlineToolDefinitions++
				}
			case "tool_use", "server_tool_use":
				if role == "assistant" {
					summary.HistoryToolCalls++
				}
			case "tool_result":
				if role == "user" {
					summary.HistoryToolResults++
					if block.Get("is_error").Type == gjson.True {
						summary.HistoryToolErrors++
					}
				}
			}
			return true
		})
		return true
	})
	return summary
}
