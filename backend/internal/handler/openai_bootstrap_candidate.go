package handler

import (
	"bytes"
	"strings"
	"unsafe"

	"github.com/tidwall/gjson"

	"github.com/MACOS-DO/sub4api/internal/service"
)

// Check metadata on actual input items, not tool names mentioned inside prompt
// text. A byte-only match sends large normal histories through several complete
// JSON decodes. This view is synchronous and read-only; it must never escape or
// be used while body is mutated. A positive match still takes the original
// uniqueness checks, envelope validation and decoder path.
func mayContainCodexCallOutputCandidate(body []byte, toolNames ...string) bool {
	if len(body) == 0 {
		return false
	}
	if !service.JSONMayEscapePlainASCII(body) {
		if !bytes.Contains(body, []byte("function_call_output")) {
			return false
		}
		found := false
		for _, name := range toolNames {
			if bytes.Contains(body, []byte(name)) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	input := gjson.Parse(unsafe.String(unsafe.SliceData(body), len(body))).Get("input")
	if !input.IsArray() {
		return false
	}
	found := false
	input.ForEach(func(_, item gjson.Result) bool {
		if item.Get("type").String() != "function_call_output" {
			return true
		}
		name := item.Get("name").String()
		namespace := item.Get("namespace").String()
		for _, candidate := range toolNames {
			matches := name == candidate
			if candidate == "codex_apps_selected_page_part_" {
				matches = strings.HasPrefix(name, candidate)
			}
			if !matches {
				continue
			}
			switch candidate {
			case "create_thread", "send_message_to_thread":
				if namespace != "codex_app" && namespace != "codex_tui" {
					continue
				}
			case "automation_update":
				if namespace != "codex_app" {
					continue
				}
			default:
				if namespace != "agent" {
					continue
				}
			}
			found = true
			return false
		}
		return true
	})
	return found
}
