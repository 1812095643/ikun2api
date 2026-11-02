package service

import (
	"bytes"
	"container/list"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	claudeToolTextContextKey = "claude_tool_text_roundtrip"
	toolTextMaxEntries       = 2048
	toolTextMaxPatches       = 256
	toolTextMaxBytes         = 4 << 20
	toolTextMaxCacheBytes    = 8 << 20
	toolTextTTL              = 24 * time.Hour
)

// Store positions and tool names, never assistant prose or signed thinking.
type toolTextPatch struct {
	start, end        int
	display, upstream string
}
type toolTextKey struct{ scope, display [32]byte }
type toolTextEntry struct {
	key       toolTextKey
	source    [32]byte
	patches   []toolTextPatch
	expires   time.Time
	ambiguous bool
	bytes     int
}
type claudeToolTextCache struct {
	mu      sync.Mutex
	entries map[toolTextKey]*list.Element
	order   list.List
	bytes   int
}

func (cache *claudeToolTextCache) remove(element *list.Element) {
	entry := element.Value.(*toolTextEntry)
	delete(cache.entries, entry.key)
	cache.bytes -= entry.bytes
	cache.order.Remove(element)
}

func (cache *claudeToolTextCache) remember(scope, display, source [32]byte, patches []toolTextPatch, now time.Time) {
	if len(patches) > toolTextMaxPatches {
		return
	}
	key := toolTextKey{scope, display}
	cost := 128
	for _, patch := range patches {
		cost += 48 + len(patch.display) + len(patch.upstream)
	}
	if cost > toolTextMaxCacheBytes {
		return
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.entries == nil {
		cache.entries = make(map[toolTextKey]*list.Element)
	}
	if element := cache.entries[key]; element != nil {
		entry := element.Value.(*toolTextEntry)
		if now.Before(entry.expires) {
			// Equal rendered prose with different origins is not reversible.
			// Preserve ambiguity; never choose another origin by last-writer wins.
			if entry.source != source {
				entry.ambiguous = true
			}
			entry.expires = now.Add(toolTextTTL)
			cache.order.MoveToBack(element)
			return
		}
		cache.remove(element)
	}
	for cache.order.Len() >= toolTextMaxEntries || cache.bytes+cost > toolTextMaxCacheBytes {
		cache.remove(cache.order.Front())
	}
	owned := append([]toolTextPatch(nil), patches...)
	for i := range owned {
		owned[i].display = strings.Clone(owned[i].display)
		owned[i].upstream = strings.Clone(owned[i].upstream)
	}
	entry := &toolTextEntry{key: key, source: source, patches: owned, expires: now.Add(toolTextTTL), bytes: cost}
	cache.entries[key] = cache.order.PushBack(entry)
	cache.bytes += cost
}

func (cache *claudeToolTextCache) restore(scope [32]byte, display string, now time.Time) (string, bool) {
	key := toolTextKey{scope, sha256.Sum256([]byte(display))}
	cache.mu.Lock()
	element := cache.entries[key]
	if element == nil {
		cache.mu.Unlock()
		return display, false
	}
	entry := element.Value.(*toolTextEntry)
	if !now.Before(entry.expires) {
		cache.remove(element)
		cache.mu.Unlock()
		return display, false
	}
	if entry.ambiguous {
		cache.mu.Unlock()
		return display, false
	}
	patches := append([]toolTextPatch(nil), entry.patches...)
	sourceHash := entry.source
	cache.mu.Unlock()
	var out strings.Builder
	position := 0
	for _, patch := range patches {
		if patch.start < position || patch.end < patch.start || patch.end > len(display) || display[patch.start:patch.end] != patch.display {
			return display, false
		}
		out.WriteString(display[position:patch.start])
		out.WriteString(patch.upstream)
		position = patch.end
	}
	out.WriteString(display[position:])
	result := out.String()
	if sha256.Sum256([]byte(result)) != sourceHash {
		return display, false
	}
	return result, true
}

type toolTextStream struct {
	source, display           hash.Hash
	sourceBytes, displayBytes int
	patches                   []toolTextPatch
}
type claudeToolTextRoundtrip struct {
	cache   *claudeToolTextCache
	scope   [32]byte
	streams map[string]*toolTextStream
}

func (s *GatewayService) prepareClaudeToolText(c *gin.Context, account *Account, model string, body []byte) []byte {
	if c == nil {
		return body
	}
	c.Set(claudeToolTextContextKey, (*claudeToolTextRoundtrip)(nil))
	if !supportsClaudeThinkingHistoryRecovery(account, model) {
		return body
	}
	metadataSession := ""
	if identity := ParseMetadataUserID(gjson.GetBytes(body, "metadata.user_id").String()); identity != nil {
		metadataSession = identity.SessionID
	}
	if metadataSession == "" {
		metadataSession = ExtractClientSessionID(c)
	}
	if metadataSession == "" {
		// Stateless callers still need a conversation discriminator. Only the
		// digest is used, never retain the prompt. API Key/account are in scope.
		if first := extractFirstUserText(body); first != "" {
			metadataSession = fmt.Sprintf("first-user:%x", sha256.Sum256([]byte(first)))
		}
	}
	scope, valid := claudeThinkingRecoveryScope(c, account, model, metadataSession)
	if !valid {
		return body
	}
	roundtrip := &claudeToolTextRoundtrip{cache: &s.claudeToolText, scope: scope, streams: make(map[string]*toolTextStream)}
	c.Set(claudeToolTextContextKey, roundtrip)
	return roundtrip.restoreHistory(body)
}

func claudeToolTextFromContext(c interface{ Get(string) (any, bool) }) *claudeToolTextRoundtrip {
	if c == nil {
		return nil
	}
	value, _ := c.Get(claudeToolTextContextKey)
	roundtrip, _ := value.(*claudeToolTextRoundtrip)
	return roundtrip
}

func (roundtrip *claudeToolTextRoundtrip) restoreHistory(body []byte) []byte {
	if roundtrip == nil {
		return body
	}
	out := body
	for index, message := range gjson.GetBytes(body, "messages").Array() {
		if message.Get("role").String() != "assistant" {
			continue
		}
		content := message.Get("content")
		restore := func(value gjson.Result, path string) {
			if value.Type != gjson.String {
				return
			}
			if original, ok := roundtrip.cache.restore(roundtrip.scope, value.String(), time.Now()); ok {
				if next, err := sjson.SetBytes(out, path, original); err == nil {
					out = next
				}
			}
		}
		if content.Type == gjson.String {
			restore(content, fmt.Sprintf("messages.%d.content", index))
			continue
		}
		for part, block := range content.Array() {
			if block.Get("type").String() == "text" {
				restore(block.Get("text"), fmt.Sprintf("messages.%d.content.%d.text", index, part))
			}
		}
	}
	return out
}

func (roundtrip *claudeToolTextRoundtrip) finish(id string) {
	stream := roundtrip.streams[id]
	delete(roundtrip.streams, id)
	if stream == nil || stream.sourceBytes == 0 {
		return
	}
	var source, display [32]byte
	copy(source[:], stream.source.Sum(nil))
	copy(display[:], stream.display.Sum(nil))
	roundtrip.cache.remember(roundtrip.scope, display, source, stream.patches, time.Now())
}

func (roundtrip *claudeToolTextRoundtrip) finishAll() {
	if roundtrip == nil {
		return
	}
	for id := range roundtrip.streams {
		roundtrip.finish(id)
	}
}

// Match original tool aliases once, longest-first, without cascading a restored
// real name into another alias. Returns reversible offsets in rendered UTF-8.
func restoreToolText(text string, rw *ToolNameRewrite, offset, maxPatches int) (string, []toolTextPatch) {
	var names []string
	for fake, real := range rw.Reverse {
		if fake != "" && fake != real && len(fake) <= 512 && len(real) <= 512 {
			names = append(names, fake)
		}
	}
	sort.Slice(names, func(i, j int) bool {
		if len(names[i]) == len(names[j]) {
			return names[i] < names[j]
		}
		return len(names[i]) > len(names[j])
	})
	var out strings.Builder
	var patches []toolTextPatch
	for position := 0; position < len(text); {
		at, found := -1, ""
		for _, name := range names {
			if n := strings.Index(text[position:], name); n >= 0 && (at < 0 || n < at) {
				at, found = n, name
			}
		}
		if at < 0 || len(patches) >= maxPatches {
			out.WriteString(text[position:])
			break
		}
		out.WriteString(text[position : position+at])
		real := rw.Reverse[found]
		start := offset + out.Len()
		out.WriteString(real)
		patches = append(patches, toolTextPatch{start, start + len(real), real, found})
		position += at + len(found)
	}
	return out.String(), patches
}

func (roundtrip *claudeToolTextRoundtrip) render(text string, rw *ToolNameRewrite, id string, complete bool) string {
	if roundtrip == nil {
		return text
	}
	stream := roundtrip.streams[id]
	if complete {
		stream = nil
	}
	if stream == nil {
		if !complete && len(roundtrip.streams) >= 64 {
			return text
		}
		stream = &toolTextStream{source: sha256.New(), display: sha256.New()}
		if !complete {
			roundtrip.streams[id] = stream
		}
	}
	remaining := toolTextMaxPatches - len(stream.patches)
	if stream.sourceBytes+len(text) > toolTextMaxBytes {
		remaining = 0
	}
	rendered, patches := restoreToolText(text, rw, stream.displayBytes, remaining)
	_, _ = io.WriteString(stream.source, text)
	_, _ = io.WriteString(stream.display, rendered)
	stream.sourceBytes += len(text)
	stream.displayBytes += len(rendered)
	stream.patches = append(stream.patches, patches...)
	if complete && stream.sourceBytes > 0 {
		var source, display [32]byte
		copy(source[:], stream.source.Sum(nil))
		copy(display[:], stream.display.Sum(nil))
		roundtrip.cache.remember(roundtrip.scope, display, source, stream.patches, time.Now())
	}
	return rendered
}

func (roundtrip *claudeToolTextRoundtrip) renderJSON(data []byte, rw *ToolNameRewrite) []byte {
	if roundtrip == nil || !gjson.ValidBytes(data) {
		return data
	}
	root := gjson.ParseBytes(data)
	var edits []toolNameSpan
	text := func(value gjson.Result, id string, complete bool) {
		if value.Type != gjson.String || value.Index < 0 || value.Index+len(value.Raw) > len(data) || !bytes.Equal(data[value.Index:value.Index+len(value.Raw)], []byte(value.Raw)) {
			return
		}
		old := value.String()
		next := roundtrip.render(old, rw, id, complete)
		if next != old {
			encoded, _ := json.Marshal(next)
			edits = append(edits, toolNameSpan{value.Index, value.Index + len(value.Raw), encoded})
		}
	}
	content := func(parts gjson.Result) {
		for _, block := range parts.Array() {
			if block.Get("type").String() == "text" || block.Get("type").String() == "output_text" {
				text(block.Get("text"), "", true)
			}
		}
	}
	kind := root.Get("type").String()
	switch kind {
	case "message":
		content(root.Get("content"))
	case "content_block_start":
		if root.Get("content_block.type").String() == "text" {
			text(root.Get("content_block.text"), "a:"+root.Get("index").Raw, false)
		}
	case "content_block_delta":
		if root.Get("delta.type").String() == "text_delta" {
			text(root.Get("delta.text"), "a:"+root.Get("index").Raw, false)
		}
	case "content_block_stop":
		roundtrip.finish("a:" + root.Get("index").Raw)
	case "message_stop":
		roundtrip.finishAll()
	case "response.output_text.delta":
		text(root.Get("delta"), "r:"+root.Get("output_index").Raw+":"+root.Get("content_index").Raw, false)
	case "response.output_text.done":
		text(root.Get("text"), "", true)
		roundtrip.finish("r:" + root.Get("output_index").Raw + ":" + root.Get("content_index").Raw)
	case "response.output_item.added", "response.output_item.done":
		if root.Get("item.type").String() == "message" {
			content(root.Get("item.content"))
		}
	}
	for _, choice := range root.Get("choices").Array() {
		id := "c:" + choice.Get("index").Raw
		text(choice.Get("delta.content"), id, false)
		text(choice.Get("message.content"), "", true)
		if choice.Get("finish_reason").Type == gjson.String {
			roundtrip.finish(id)
		}
	}
	for _, path := range []string{"output", "response.output"} {
		for _, item := range root.Get(path).Array() {
			if item.Get("type").String() == "message" {
				content(item.Get("content"))
			}
		}
	}
	return applyToolResponseNameSpans(data, edits)
}

func (roundtrip *claudeToolTextRoundtrip) renderPayload(data []byte, rw *ToolNameRewrite) []byte {
	if roundtrip == nil || rw == nil || len(rw.Reverse) == 0 {
		return data
	}
	if gjson.ValidBytes(data) {
		return roundtrip.renderJSON(data, rw)
	}
	// Existing callers pass complete SSE events or one data line, not arbitrary
	// transport fragments. Preserve non-data fields and framing unchanged.
	lines := strings.SplitAfter(string(data), "\n")
	var out strings.Builder
	var event []string
	flush := func() {
		if len(event) == 0 {
			return
		}
		var payload []string
		for _, line := range event {
			raw := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
			if v, ok := extractAnthropicSSEDataLine(raw); ok {
				payload = append(payload, v)
			}
		}
		original := strings.Join(payload, "\n")
		if original == "[DONE]" {
			roundtrip.finishAll()
		}
		next := string(roundtrip.renderJSON([]byte(original), rw))
		if original == next {
			for _, line := range event {
				out.WriteString(line)
			}
			event = nil
			return
		}
		replacementLines := strings.Split(next, "\n")
		if len(replacementLines) != len(payload) {
			for _, line := range event {
				out.WriteString(line)
			}
			event = nil
			return
		}
		dataIndex := 0
		for _, line := range event {
			raw := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
			if _, ok := extractAnthropicSSEDataLine(raw); ok {
				out.WriteString("data: ")
				out.WriteString(replacementLines[dataIndex])
				dataIndex++
				if strings.HasSuffix(line, "\r\n") {
					out.WriteString("\r\n")
				} else if strings.HasSuffix(line, "\n") {
					out.WriteByte('\n')
				}
			} else {
				out.WriteString(line)
			}
		}
		event = nil
	}
	for _, line := range lines {
		event = append(event, line)
		if line == "\n" || line == "\r\n" {
			flush()
		}
	}
	flush()
	return []byte(out.String())
}
