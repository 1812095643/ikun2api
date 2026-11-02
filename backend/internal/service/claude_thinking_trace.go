package service

import (
	"crypto/sha256"
	"hash"
	"io"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const claudeThinkingTraceContextKey = "claude_thinking_integrity_trace"

// Provenance is diagnostic only. It never causes history deletion, account
// changes or a retry. Digests are scoped to the same user/key/account/model and
// original session as recovery, and use the same bounded, expiring storage.
type claudeThinkingTrace struct {
	cache    *claudeThinkingRecoveryCache
	scope    [32]byte
	prior    map[[32]byte]struct{}
	input    map[[32]byte][32]byte
	blocks   map[int]*claudeThinkingStreamDigest
	observed [][32]byte
}

type claudeThinkingStreamDigest struct {
	kind      string
	text      hash.Hash
	signature [32]byte
	hasSig    bool
}

func thinkingSignatureDigest(kind, signature string) [32]byte {
	h := sha256.New()
	_, _ = io.WriteString(h, "signature:"+kind+":")
	_, _ = io.WriteString(h, signature)
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func thinkingContentPairDigest(kind string, content, signature [32]byte) [32]byte {
	h := sha256.New()
	_, _ = io.WriteString(h, "signed-content:"+kind+":")
	_, _ = h.Write(content[:])
	_, _ = h.Write(signature[:])
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func thinkingBlockTraceDigests(block gjson.Result) (signature, pair [32]byte, ok bool) {
	kind := block.Get("type").String()
	text, signed := "", ""
	switch kind {
	case "thinking":
		if block.Get("thinking").Type != gjson.String || block.Get("signature").Type != gjson.String {
			return signature, pair, false
		}
		text, signed = block.Get("thinking").String(), block.Get("signature").String()
	case "redacted_thinking":
		if block.Get("data").Type != gjson.String {
			return signature, pair, false
		}
		signed = block.Get("data").String()
	default:
		return signature, pair, false
	}
	if signed == "" {
		return signature, pair, false
	}
	signature = thinkingSignatureDigest(kind, signed)
	pair = thinkingContentPairDigest(kind, sha256.Sum256([]byte(text)), signature)
	return signature, pair, true
}

func (s *GatewayService) beginClaudeThinkingTrace(c *gin.Context, account *Account, model, originalSession string, body []byte) *claudeThinkingTrace {
	if c == nil {
		return nil
	}
	// A failover can reuse gin.Context. Never reuse the previous account's trace.
	c.Set(claudeThinkingTraceContextKey, (*claudeThinkingTrace)(nil))
	if !supportsClaudeThinkingHistoryRecovery(account, model) {
		return nil
	}
	scope, valid := claudeThinkingRecoveryScope(c, account, model, originalSession)
	if !valid {
		return nil
	}
	trace := &claudeThinkingTrace{cache: &s.claudeThinkingObserved, scope: scope,
		prior: s.claudeThinkingObserved.known(scope, time.Now()), input: make(map[[32]byte][32]byte),
		blocks: make(map[int]*claudeThinkingStreamDigest)}
	for _, message := range gjson.GetBytes(body, "messages").Array() {
		if message.Get("role").String() != "assistant" {
			continue
		}
		for _, block := range message.Get("content").Array() {
			if signature, pair, ok := thinkingBlockTraceDigests(block); ok {
				trace.input[signature] = pair
				if len(trace.input) >= claudeThinkingRecoveryMaxBatch {
					break
				}
			}
		}
		if len(trace.input) >= claudeThinkingRecoveryMaxBatch {
			break
		}
	}
	c.Set(claudeThinkingTraceContextKey, trace)
	return trace
}

func claudeThinkingTraceFromContext(c *gin.Context) *claudeThinkingTrace {
	if c == nil {
		return nil
	}
	value, _ := c.Get(claudeThinkingTraceContextKey)
	trace, _ := value.(*claudeThinkingTrace)
	return trace
}

func (trace *claudeThinkingTrace) observeJSON(body []byte) {
	if trace == nil {
		return
	}
	for _, block := range gjson.GetBytes(body, "content").Array() {
		if signature, pair, ok := thinkingBlockTraceDigests(block); ok {
			trace.record(signature, pair)
		}
	}
}

func (trace *claudeThinkingTrace) record(signature, pair [32]byte) {
	if len(trace.observed)+2 <= claudeThinkingRecoveryMaxBatch {
		trace.observed = append(trace.observed, signature, pair)
	}
}

// Signature deltas are complete signed values in the official CLI; never append
// them like text deltas. Only a closed block with a signature enters provenance.
func (trace *claudeThinkingTrace) observeEvent(data []byte) {
	if trace == nil {
		return
	}
	root := gjson.ParseBytes(data)
	index := root.Get("index")
	if index.Type != gjson.Number || index.Int() < 0 || index.Int() > 1<<20 {
		return
	}
	id := int(index.Int())
	switch root.Get("type").String() {
	case "content_block_start":
		delete(trace.blocks, id)
		block := root.Get("content_block")
		kind := block.Get("type").String()
		if kind != "thinking" && kind != "redacted_thinking" || len(trace.blocks) >= claudeThinkingRecoveryMaxBatch {
			return
		}
		digest := &claudeThinkingStreamDigest{kind: kind, text: sha256.New()}
		_, _ = io.WriteString(digest.text, block.Get("thinking").String())
		signed := block.Get("signature").String()
		if kind == "redacted_thinking" {
			signed = block.Get("data").String()
		}
		if signed != "" {
			digest.signature = thinkingSignatureDigest(kind, signed)
			digest.hasSig = true
		}
		trace.blocks[id] = digest
	case "content_block_delta":
		digest := trace.blocks[id]
		if digest == nil || digest.kind != "thinking" {
			return
		}
		delta := root.Get("delta")
		switch delta.Get("type").String() {
		case "thinking_delta":
			if delta.Get("thinking").Type == gjson.String {
				_, _ = io.WriteString(digest.text, delta.Get("thinking").String())
			}
		case "signature_delta":
			if delta.Get("signature").Type == gjson.String {
				signed := delta.Get("signature").String()
				digest.signature = thinkingSignatureDigest(digest.kind, signed)
				digest.hasSig = signed != ""
			}
		}
	case "content_block_stop":
		digest := trace.blocks[id]
		delete(trace.blocks, id)
		if digest != nil && digest.hasSig {
			var content [32]byte
			copy(content[:], digest.text.Sum(nil))
			trace.record(digest.signature, thinkingContentPairDigest(digest.kind, content, digest.signature))
		}
	}
}

func (trace *claudeThinkingTrace) commit() {
	if trace != nil && trace.cache != nil && len(trace.observed) > 0 {
		trace.cache.remember(trace.scope, trace.observed, time.Now())
		trace.observed = nil
	}
}

func (trace *claudeThinkingTrace) origin(block gjson.Result) string {
	if trace == nil {
		return "unavailable"
	}
	signature, pair, valid := thinkingBlockTraceDigests(block)
	if !valid {
		return "unsigned_or_invalid_shape"
	}
	inputPair, inInput := trace.input[signature]
	if inInput && inputPair != pair {
		return "changed_during_request_forwarding"
	}
	if _, seen := trace.prior[signature]; !seen {
		return "not_observed_in_this_scope"
	}
	if _, seen := trace.prior[pair]; seen {
		return "matches_previous_upstream_response"
	}
	return "changed_before_request_forwarding"
}
