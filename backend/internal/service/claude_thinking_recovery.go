package service

import (
	"container/list"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	claudeThinkingRecoveryTTL        = time.Hour
	claudeThinkingRecoveryMaxEntries = 4096
	claudeThinkingRecoveryMaxBatch   = 256
	claudeThinkingDigestMaxBytes     = 2 << 20
)

// Official Claude Code 2.1.316 removes rejected thinking history and records a
// thinking_stripped marker for later turns. An API gateway cannot update the
// caller's transcript, so retain only bounded, scoped block digests locally.
// Neither signatures nor thinking text are retained in this cache.
type claudeThinkingRecoveryEntry struct {
	scope   [32]byte
	block   [32]byte
	expires time.Time
}

type claudeThinkingRecoveryCache struct {
	mu     sync.Mutex
	scopes map[[32]byte]map[[32]byte]*list.Element
	order  list.List
}

func (c *claudeThinkingRecoveryCache) remove(element *list.Element) {
	entry := element.Value.(claudeThinkingRecoveryEntry)
	delete(c.scopes[entry.scope], entry.block)
	if len(c.scopes[entry.scope]) == 0 {
		delete(c.scopes, entry.scope)
	}
	c.order.Remove(element)
}

func (c *claudeThinkingRecoveryCache) known(scope [32]byte, now time.Time) map[[32]byte]struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	var known map[[32]byte]struct{}
	for digest, element := range c.scopes[scope] {
		entry := element.Value.(claudeThinkingRecoveryEntry)
		if !now.Before(entry.expires) {
			c.remove(element)
			continue
		}
		if known == nil {
			known = make(map[[32]byte]struct{})
		}
		known[digest] = struct{}{}
	}
	return known
}

func (c *claudeThinkingRecoveryCache) remember(scope [32]byte, blocks [][32]byte, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.scopes == nil {
		c.scopes = make(map[[32]byte]map[[32]byte]*list.Element)
	}
	if len(blocks) > claudeThinkingRecoveryMaxBatch {
		blocks = blocks[len(blocks)-claudeThinkingRecoveryMaxBatch:]
	}
	for _, digest := range blocks {
		if element := c.scopes[scope][digest]; element != nil {
			entry := element.Value.(claudeThinkingRecoveryEntry)
			entry.expires = now.Add(claudeThinkingRecoveryTTL)
			element.Value = entry
			c.order.MoveToBack(element)
			continue
		}
		for c.order.Len() >= claudeThinkingRecoveryMaxEntries {
			c.remove(c.order.Front())
		}
		if c.scopes[scope] == nil {
			c.scopes[scope] = make(map[[32]byte]*list.Element)
		}
		entry := claudeThinkingRecoveryEntry{scope: scope, block: digest, expires: now.Add(claudeThinkingRecoveryTTL)}
		c.scopes[scope][digest] = c.order.PushBack(entry)
	}
}

func supportsClaudeThinkingHistoryRecovery(account *Account, model string) bool {
	return account != nil && account.IsAnthropicOAuthOrSetupToken() && ShouldApplyRetryFilters(model)
}

func claudeThinkingRecoveryScope(c *gin.Context, account *Account, model, metadataSession string) ([32]byte, bool) {
	if c == nil || account == nil || account.ID <= 0 {
		return [32]byte{}, false
	}
	value, _ := c.Get("api_key")
	key, ok := value.(*APIKey)
	if !ok || key == nil || key.ID <= 0 || key.UserID <= 0 {
		return [32]byte{}, false
	}
	headerSession := ClaudeCodeSessionIDFromHeader(c)
	metadataSession = sanitizeSessionID(metadataSession)
	if headerSession == "" && metadataSession == "" {
		return [32]byte{}, false
	}
	groupID := int64(0)
	if key.GroupID != nil {
		groupID = *key.GroupID
	}
	// Include both original session signals if present, before identity masking.
	// JSON framing prevents delimiter collisions; credentials are never included.
	encoded, _ := json.Marshal([]any{key.UserID, key.ID, groupID, account.ID,
		account.GetExtraString("account_uuid"), account.GetCustomBaseURL(), model, headerSession, metadataSession})
	return sha256.Sum256(encoded), true
}

func claudeThinkingBlockDigest(block gjson.Result) ([32]byte, bool) {
	if !block.IsObject() || len(block.Raw) > claudeThinkingDigestMaxBytes {
		return [32]byte{}, false
	}
	kind := block.Get("type").String()
	if kind != "thinking" && kind != "redacted_thinking" {
		return [32]byte{}, false
	}
	// Canonicalize object key order and JSON whitespace, but preserve all values,
	// including unknown fields and exact numbers. Changed content must not match.
	var value map[string]any
	if decodeJSONUseNumber([]byte(block.Raw), &value) != nil {
		return [32]byte{}, false
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return [32]byte{}, false
	}
	return sha256.Sum256(encoded), true
}

func claudeThinkingBlockDigests(body []byte) map[[32]byte]struct{} {
	var digests map[[32]byte]struct{}
	for _, message := range gjson.GetBytes(body, "messages").Array() {
		if message.Get("role").String() != "assistant" {
			continue
		}
		for _, block := range message.Get("content").Array() {
			if digest, ok := claudeThinkingBlockDigest(block); ok {
				if digests == nil {
					digests = make(map[[32]byte]struct{})
				}
				digests[digest] = struct{}{}
				if len(digests) >= claudeThinkingRecoveryMaxEntries {
					return digests
				}
			}
		}
	}
	return digests
}

// nil known strips the current historical thinking, like the official recovery.
// A non-nil set strips only previously rejected history, preserving new blocks.
// Unlike the older generic fallback, do not turn reasoning into ordinary text
// and do not disable the current request's thinking mode.
func stripClaudeThinkingHistory(body []byte, known map[[32]byte]struct{}) ([]byte, [][32]byte, bool) {
	out := body
	var removed [][32]byte
	changed := false
	for index, message := range gjson.GetBytes(body, "messages").Array() {
		if message.Get("role").String() != "assistant" || !message.Get("content").IsArray() {
			continue
		}
		var kept [][]byte
		messageChanged := false
		for _, block := range message.Get("content").Array() {
			kind := block.Get("type").String()
			if kind == "thinking" || kind == "redacted_thinking" {
				digest, valid := claudeThinkingBlockDigest(block)
				_, matched := known[digest]
				if known == nil || (valid && matched) {
					messageChanged = true
					if valid && len(removed) < claudeThinkingRecoveryMaxBatch {
						removed = append(removed, digest)
					}
					continue
				}
			}
			kept = append(kept, []byte(block.Raw))
		}
		if !messageChanged {
			continue
		}
		// Remove empty separators only in a message whose thinking was removed.
		nonempty := kept[:0]
		for _, raw := range kept {
			block := gjson.ParseBytes(raw)
			if block.Get("type").String() == "text" && strings.TrimSpace(block.Get("text").String()) == "" {
				continue
			}
			nonempty = append(nonempty, raw)
		}
		if len(nonempty) == 0 {
			nonempty = [][]byte{[]byte(`{"type":"text","text":"[Thinking removed]"}`)}
		}
		next, err := sjson.SetRawBytes(out, fmt.Sprintf("messages.%d.content", index), buildJSONArrayRaw(nonempty))
		if err != nil {
			return body, nil, false
		}
		out, changed = next, true
	}
	return out, removed, changed
}

var claudeInvalidThinkingSignature = regexp.MustCompile("(?i)invalid\\s+`?signature`?\\s+in\\s+`?thinking`?\\s+block")
var claudeThinkingErrorPosition = regexp.MustCompile(`messages\.(\d+)\.content\.(\d+)`)

func isClaudeInvalidThinkingSignature(body []byte) bool {
	return claudeInvalidThinkingSignature.MatchString(extractUpstreamErrorMessage(body))
}

func logClaudeThinkingHistoryRejection(c *gin.Context, accountID int64, mimic bool, input map[[32]byte]struct{}, trace *claudeThinkingTrace, wire, response []byte) {
	position := claudeThinkingErrorPosition.FindStringSubmatch(extractUpstreamErrorMessage(response))
	if len(position) != 3 {
		return
	}
	messageIndex, err1 := strconv.Atoi(position[1])
	blockIndex, err2 := strconv.Atoi(position[2])
	if err1 != nil || err2 != nil {
		return
	}
	block := gjson.GetBytes(wire, fmt.Sprintf("messages.%d.content.%d", messageIndex, blockIndex))
	digest, valid := claudeThinkingBlockDigest(block)
	_, unchanged := input[digest]
	// Presence in the snapshot proves preservation of this block through Forward;
	// absence is inconclusive if a snapshot bound was reached. No raw data is logged.
	hasToolRewrite := c != nil && toolNameRewriteFromContext(c) != nil
	logger.LegacyPrintf("service.gateway", "Claude thinking history rejected account=%d message_index=%d block_index=%d matched_forward_input=%t digest_available=%t origin=%s mimic_body=%t tool_name_rewrite=%t", accountID, messageIndex, blockIndex, valid && unchanged, valid, trace.origin(block), mimic, hasToolRewrite)
}
