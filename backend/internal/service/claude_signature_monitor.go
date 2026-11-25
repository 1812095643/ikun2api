package service

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/telemetry"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"strconv"
	"time"
)

func signatureMonitorSession(c *gin.Context, body []byte) string {
	if c == nil {
		return ""
	}
	v, _ := c.Get("api_key")
	key, ok := v.(*APIKey)
	if !ok || key == nil || key.UserID <= 0 || key.ID <= 0 {
		return ""
	}
	header := ClaudeCodeSessionIDFromHeader(c)
	meta := ""
	if id := ParseMetadataUserID(gjson.GetBytes(body, "metadata.user_id").String()); id != nil {
		meta = sanitizeSessionID(id.SessionID)
	}
	if header == "" && meta == "" {
		return ""
	}
	b, _ := json.Marshal([]any{key.UserID, key.ID, key.GroupID, header, meta})
	return fmt.Sprintf("session:%x", sha256.Sum256(b))
}
func monitorClaudeSignatureRejection(c *gin.Context, account int64, session string, input map[[32]byte]struct{}, trace *claudeThinkingTrace, wire, response []byte) {
	if c == nil || c.Request == nil || !isClaudeInvalidThinkingSignature(response) {
		return
	}
	metrics := map[string]float64{"signature_session_known": 0}
	if session != "" {
		metrics["signature_session_known"] = 1
	}
	p := claudeThinkingErrorPosition.FindStringSubmatch(extractUpstreamErrorMessage(response))
	if len(p) == 3 {
		mi, e1 := strconv.Atoi(p[1])
		bi, e2 := strconv.Atoi(p[2])
		if e1 == nil && e2 == nil {
			block := parseRawJSONView(wire).Get(fmt.Sprintf("messages.%d.content.%d", mi, bi))
			digest, valid := claudeThinkingBlockDigest(block)
			_, same := input[digest]
			if valid && same {
				metrics["signature_forward_unchanged"] = 1
			}
			metrics["signature_message_index"] = float64(mi)
			metrics["signature_block_index"] = float64(bi)
			switch trace.origin(block) {
			case "changed_during_request_forwarding":
				metrics["signature_changed_in_forward"] = 1
			case "matches_previous_upstream_response":
				metrics["signature_matches_previous"] = 1
			case "changed_before_request_forwarding":
				metrics["signature_changed_before_forward"] = 1
			}
		}
	}
	telemetry.FromContext(c.Request.Context()).Task("signature.rejected", session, "thinking_signature_invalid", metrics, time.Now())
	logClaudeThinkingHistoryRejection(c, account, false, input, trace, wire, response)
}
