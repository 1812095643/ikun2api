package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// Replay the bridged Responses output as the next Anthropic request. A valid
// opaque signature must survive the entire round trip without being spliced
// with a seed or an earlier complete value from the same stream.
func TestAnthropicThinkingSignatureStreamRoundTripUsesLastCompleteValue(t *testing.T) {
	for _, tc := range []struct {
		name       string
		seed       string
		signatures []string
		want       string
	}{
		{"single", "", []string{"complete-signature"}, "complete-signature"},
		{"seed_is_replaced", "seed-signature", []string{"complete-signature"}, "complete-signature"},
		{"successive_values", "seed-signature", []string{"previous-complete", "latest-complete"}, "latest-complete"},
		{"repeated_value", "", []string{"same-complete", "same-complete"}, "same-complete"},
		{"seed_without_delta", "seed-signature", nil, "seed-signature"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := NewAnthropicEventToResponsesState()
			AnthropicEventToResponsesEvents(&AnthropicStreamEvent{
				Type:    "message_start",
				Message: &AnthropicResponse{ID: "msg_signature", Model: "claude-opus-5-5"},
			}, state)
			AnthropicEventToResponsesEvents(&AnthropicStreamEvent{
				Type:         "content_block_start",
				ContentBlock: &AnthropicContentBlock{Type: "thinking", Thinking: "先", Signature: tc.seed},
			}, state)
			AnthropicEventToResponsesEvents(&AnthropicStreamEvent{
				Type:  "content_block_delta",
				Delta: &AnthropicDelta{Type: "thinking_delta", Thinking: "思考"},
			}, state)
			for _, signature := range tc.signatures {
				events := AnthropicEventToResponsesEvents(&AnthropicStreamEvent{
					Type:  "content_block_delta",
					Delta: &AnthropicDelta{Type: "signature_delta", Signature: signature},
				}, state)
				require.Empty(t, events, "opaque signatures must not become visible deltas")
			}
			closed := AnthropicEventToResponsesEvents(&AnthropicStreamEvent{Type: "content_block_stop"}, state)
			require.Len(t, state.Outputs, 1)
			require.NotEmpty(t, state.Outputs[0].EncryptedContent)
			var doneItem *ResponsesOutput
			for _, event := range closed {
				if event.Type == "response.output_item.done" {
					doneItem = event.Item
				}
			}
			require.NotNil(t, doneItem)
			require.Equal(t, state.Outputs[0].EncryptedContent, doneItem.EncryptedContent)
			raw, err := json.Marshal(state.Outputs)
			require.NoError(t, err)
			converted, err := ResponsesToAnthropicRequest(&ResponsesRequest{Model: "claude-opus-5-5", Input: raw})
			require.NoError(t, err)
			require.Len(t, converted.Messages, 1)
			var blocks []AnthropicContentBlock
			require.NoError(t, json.Unmarshal(converted.Messages[0].Content, &blocks))
			require.Equal(t, []AnthropicContentBlock{{Type: "thinking", Thinking: "先思考", Signature: tc.want}}, blocks)

			// Closing and reopening a block must neither reuse its signature nor
			// change the completed envelope retained by the client.
			priorEnvelope := state.Outputs[0].EncryptedContent
			AnthropicEventToResponsesEvents(&AnthropicStreamEvent{
				Type:         "content_block_start",
				ContentBlock: &AnthropicContentBlock{Type: "thinking", Thinking: "next"},
			}, state)
			require.Empty(t, state.CurrentThinking.Signature)
			AnthropicEventToResponsesEvents(&AnthropicStreamEvent{
				Type:  "content_block_delta",
				Delta: &AnthropicDelta{Type: "signature_delta", Signature: "next-complete"},
			}, state)
			AnthropicEventToResponsesEvents(&AnthropicStreamEvent{Type: "content_block_stop"}, state)
			require.Equal(t, priorEnvelope, state.Outputs[0].EncryptedContent)
			require.Len(t, state.Outputs, 2)
		})
	}
}
