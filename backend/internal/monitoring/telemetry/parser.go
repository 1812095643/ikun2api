package telemetry

import (
	"bytes"
	"github.com/tidwall/gjson"
	"strings"
)

type Signal struct {
	OutputBudgetExhausted            bool
	Output                           string
	Bytes                            int
	Done, Failed, Partial, Supported bool
	ReasoningStarted                 bool
	Reason                           string
}
type Parser struct {
	line, data []byte
	event      string
	discard    bool
	seenOutput bool
	emit       func(Signal)
}

const maxFrame = 64 << 10

func NewParser(emit func(Signal)) *Parser {
	p := &Parser{}
	p.emit = func(s Signal) {
		if s.Output != "" {
			p.seenOutput = true
		}
		emit(s)
	}
	return p
}
func (p *Parser) Feed(b []byte) {
	for _, c := range b {
		if c == '\n' {
			p.consumeLine()
			p.line = p.line[:0]
			continue
		}
		if len(p.line)+len(p.data) >= maxFrame {
			if !p.discard {
				p.emit(Signal{Partial: true, Reason: "frame_limit"})
			}
			p.discard = true
		}
		if !p.discard {
			p.line = append(p.line, c)
		}
	}
}
func (p *Parser) consumeLine() {
	line := bytes.TrimSuffix(p.line, []byte{'\r'})
	if len(line) == 0 {
		if !p.discard {
			if p.event == "error" {
				reason := ReasonCode(gjson.GetBytes(p.data, "error.code").String())
				if reason == "unclassified" {
					reason = "stream_error"
				}
				p.emit(Signal{Failed: true, Supported: true, Reason: reason})
			} else {
				p.JSON(p.data)
			}
		}
		p.data = p.data[:0]
		p.event = ""
		p.discard = false
		return
	}
	if p.discard {
		return
	}
	if bytes.HasPrefix(line, []byte("event:")) {
		p.event = strings.TrimSpace(string(line[6:]))
	}
	if bytes.HasPrefix(line, []byte("data:")) {
		if len(p.data) > 0 {
			p.data = append(p.data, '\n')
		}
		p.data = append(p.data, bytes.TrimSpace(line[5:])...)
	}
}
func (p *Parser) JSON(data []byte) {
	raw := strings.TrimSpace(string(data))
	if raw == "" {
		return
	}
	if raw == "[DONE]" {
		p.emit(Signal{Done: true, Supported: true})
		return
	}
	if !gjson.Valid(raw) {
		p.emit(Signal{Partial: true, Reason: "malformed_frame"})
		return
	}
	r := gjson.Parse(raw)
	typ := r.Get("type").String()
	if strings.HasPrefix(typ, "response.reasoning_") || ((typ == "response.output_item.added" || typ == "response.output_item.done") && r.Get("item.type").String() == "reasoning") {
		p.emit(Signal{ReasoningStarted: true, Supported: true})
	}
	err := r.Get("error")
	if (err.Exists() && err.Type != gjson.Null) || typ == "error" || typ == "response.failed" || typ == "response.incomplete" {
		reason := ReasonCode(err.Get("code").String())
		if reason == "unclassified" {
			reason = "stream_error"
		}
		p.emit(Signal{Failed: true, Supported: true, Reason: reason})
		return
	}
	output := func(kind string, v gjson.Result) {
		if v.Exists() && v.String() != "" {
			p.emit(Signal{Output: kind, Bytes: len(v.String()), Supported: true})
		}
	}
	switch typ {
	case "message_delta":
		if r.Get("delta.stop_reason").String() == "max_tokens" {
			p.emit(Signal{OutputBudgetExhausted: true, Supported: true})
		}
	case "content_block_delta":
		d := r.Get("delta")
		switch d.Get("type").String() {
		case "text_delta":
			output("content", d.Get("text"))
		case "thinking_delta":
			p.emit(Signal{ReasoningStarted: true, Supported: true})
			output("reasoning", d.Get("thinking"))
		case "input_json_delta":
			output("tool", d.Get("partial_json"))
		}
	case "content_block_start":
		switch r.Get("content_block.type").String() {
		case "tool_use":
			p.emit(Signal{Output: "tool", Supported: true})
		case "thinking", "redacted_thinking":
			p.emit(Signal{ReasoningStarted: true, Supported: true})
			output("reasoning", r.Get("content_block.thinking"))
		case "text":
			output("content", r.Get("content_block.text"))
		}
	case "message_stop", "response.completed":
		p.emit(Signal{Done: true, Supported: true})
	case "response.output_text.delta":
		output("content", r.Get("delta"))
	case "response.refusal.delta":
		output("content", r.Get("delta"))
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		output("reasoning", r.Get("delta"))
	case "response.function_call_arguments.delta":
		output("tool", r.Get("delta"))
	case "response.output_item.added":
		if r.Get("item.type").String() == "function_call" {
			p.emit(Signal{Output: "tool", Supported: true})
		}
	}
	r.Get("choices").ForEach(func(_, c gjson.Result) bool {
		for _, path := range []string{"delta", "message"} {
			d := c.Get(path)
			if d.Get("reasoning_content").Exists() || d.Get("reasoning").Exists() {
				p.emit(Signal{ReasoningStarted: true, Supported: true})
			}
			output("content", d.Get("content"))
			output("reasoning", d.Get("reasoning_content"))
			output("reasoning", d.Get("reasoning"))
			if len(d.Get("tool_calls").Array()) > 0 || d.Get("function_call").Exists() {
				p.emit(Signal{Output: "tool", Supported: true})
			}
		}
		f := c.Get("finish_reason")
		if f.Exists() && f.Type != gjson.Null && f.String() != "" {
			if f.String() == "content_filter" {
				p.emit(Signal{Failed: true, Supported: true, Reason: "content_filtered"})
			} else {
				p.emit(Signal{Done: true, Supported: true})
			}
		}
		return true
	})
	r.Get("candidates").ForEach(func(_, c gjson.Result) bool {
		c.Get("content.parts").ForEach(func(_, part gjson.Result) bool {
			if part.Get("functionCall").Exists() {
				p.emit(Signal{Output: "tool", Supported: true})
			}
			kind := "content"
			if part.Get("thought").Bool() {
				p.emit(Signal{ReasoningStarted: true, Supported: true})
				kind = "reasoning"
			}
			output(kind, part.Get("text"))
			return true
		})
		f := c.Get("finishReason").String()
		if f != "" {
			if f == "STOP" || f == "MAX_TOKENS" {
				p.emit(Signal{Done: true, Supported: true})
			} else {
				p.emit(Signal{Failed: true, Supported: true, Reason: "provider_finish_rejected"})
			}
		}
		return true
	})
	r.Get("content").ForEach(func(_, c gjson.Result) bool {
		switch c.Get("type").String() {
		case "text":
			output("content", c.Get("text"))
		case "thinking", "redacted_thinking":
			p.emit(Signal{ReasoningStarted: true, Supported: true})
			output("reasoning", c.Get("thinking"))
		case "tool_use":
			p.emit(Signal{Output: "tool", Supported: true})
		}
		return true
	})
	if typ == "message" && r.Get("stop_reason").Exists() && r.Get("stop_reason").Type != gjson.Null {
		p.emit(Signal{Done: true, Supported: true})
	}
	parseItem := func(item gjson.Result) {
		t := item.Get("type").String()
		if strings.HasSuffix(t, "_call") {
			p.emit(Signal{Output: "tool", Supported: true})
		}
		item.Get("content").ForEach(func(_, c gjson.Result) bool { output("content", c.Get("text")); return true })
		if t == "reasoning" {
			p.emit(Signal{ReasoningStarted: true, Supported: true})
			item.Get("summary").ForEach(func(_, c gjson.Result) bool { output("reasoning", c.Get("text")); return true })
			output("reasoning", item.Get("encrypted_content"))
		}
	}
	r.Get("output").ForEach(func(_, item gjson.Result) bool {
		parseItem(item)
		return true
	})
	if typ == "response.output_item.done" {
		parseItem(r.Get("item"))
	}
	if typ == "response.completed" {
		r.Get("response.output").ForEach(func(_, item gjson.Result) bool { parseItem(item); return true })
		if !p.seenOutput && r.Get("response.usage.output_tokens").Int() > 0 {
			p.emit(Signal{Partial: true, Reason: "unrecognized_output"})
		}
	}
	if r.Get("object").String() == "response" && r.Get("status").String() == "completed" {
		p.emit(Signal{Done: true, Supported: true})
	}
}
