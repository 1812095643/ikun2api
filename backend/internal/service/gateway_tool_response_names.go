package service

import (
	"bytes"
	"encoding/json"
	"sort"

	"github.com/tidwall/gjson"
)

// Only traverse response-envelope locations. In particular, do not recurse into
// input/arguments, thinking, signatures, encrypted_content, text or tool IDs.
func toolResponseNameSpans(data []byte, rw *ToolNameRewrite) []toolNameSpan {
	if !gjson.ValidBytes(data) {
		return nil
	}
	var edits []toolNameSpan
	add := func(name gjson.Result) {
		if name.Type != gjson.String {
			return
		}
		real, exists := rw.Reverse[name.String()]
		if !exists || real == name.String() || name.Index < 0 || name.Index+len(name.Raw) > len(data) ||
			!bytes.Equal(data[name.Index:name.Index+len(name.Raw)], []byte(name.Raw)) {
			return
		}
		encoded, err := json.Marshal(real)
		if err == nil {
			edits = append(edits, toolNameSpan{name.Index, name.Index + len(name.Raw), encoded})
		}
	}
	addTool := func(block gjson.Result) {
		switch block.Get("type").String() {
		case "tool_use", "function_call", "custom_tool_call":
			add(block.Get("name"))
		}
	}
	root := gjson.ParseBytes(data)
	addTool(root)
	for _, path := range []string{"content_block", "item"} {
		addTool(root.Get(path))
	}
	for _, path := range []string{"content", "message.content", "output", "response.output"} {
		for _, block := range root.Get(path).Array() {
			addTool(block)
		}
	}
	for _, choice := range root.Get("choices").Array() {
		for _, path := range []string{"message", "delta"} {
			message := choice.Get(path)
			add(message.Get("function_call.name"))
			for _, call := range message.Get("tool_calls").Array() {
				add(call.Get("function.name"))
				add(call.Get("custom.name"))
			}
		}
	}
	return edits
}

func applyToolResponseNameSpans(data []byte, edits []toolNameSpan) []byte {
	if len(edits) == 0 {
		return data
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	out := make([]byte, 0, len(data))
	position := 0
	for _, edit := range edits {
		if edit.start < position || edit.end < edit.start || edit.end > len(data) {
			continue
		}
		out = append(out, data[position:edit.start]...)
		out = append(out, edit.value...)
		position = edit.end
	}
	return append(out, data[position:]...)
}

type toolResponseSSESegment struct {
	payloadStart int
	payloadEnd   int
	inputStart   int
}

func restoreSSEToolResponseNames(data []byte, rw *ToolNameRewrite) []byte {
	var edits []toolNameSpan
	var payload []byte
	var segments []toolResponseSSESegment
	flush := func() {
		if len(segments) == 0 {
			return
		}
		for _, edit := range toolResponseNameSpans(payload, rw) {
			for _, segment := range segments {
				// JSON string values cannot contain a literal newline, so a tool
				// name token lives wholly inside one SSE data line. Preserve all
				// framing bytes, comments, id/retry fields and original CRLF.
				if edit.start >= segment.payloadStart && edit.end <= segment.payloadEnd {
					delta := segment.inputStart - segment.payloadStart
					edits = append(edits, toolNameSpan{edit.start + delta, edit.end + delta, edit.value})
					break
				}
			}
		}
		payload = payload[:0]
		segments = segments[:0]
	}
	for start := 0; start < len(data); {
		end := bytes.IndexByte(data[start:], '\n')
		next := len(data)
		if end < 0 {
			end = len(data)
		} else {
			end += start
			next = end + 1
		}
		lineEnd := end
		if lineEnd > start && data[lineEnd-1] == '\r' {
			lineEnd--
		}
		line := data[start:lineEnd]
		if len(line) == 0 {
			flush()
		} else if bytes.HasPrefix(line, []byte("data:")) {
			valueStart := start + len("data:")
			if valueStart < lineEnd && data[valueStart] == ' ' {
				valueStart++
			}
			if len(segments) > 0 {
				payload = append(payload, '\n')
			}
			segment := toolResponseSSESegment{payloadStart: len(payload), inputStart: valueStart}
			payload = append(payload, data[valueStart:lineEnd]...)
			segment.payloadEnd = len(payload)
			segments = append(segments, segment)
		}
		start = next
	}
	flush()
	return applyToolResponseNameSpans(data, edits)
}
