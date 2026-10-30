package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHostedToolCapabilitiesBelongToFirstSuccessfulCreate(t *testing.T) {
	both := openAIWSHostedWebSearch | openAIWSHostedImageGeneration
	for _, tc := range []struct {
		name, raw string
		declared  openAIWSHostedTools
	}{
		{"none", `{"type":"response.create","input":[]}`, 0},
		{"image", `{"type":"response.create","tools":[{"type":"image_generation","quality":"high"}]}`, openAIWSHostedImageGeneration},
		{"search", `{"type":"response.create","tools":[{"type":"web_search_preview_2025_03_11"}]}`, openAIWSHostedWebSearch},
		{"both", `{"type":"response.create","tools":[{"type":"web_search"},{"type":"image_generation"}]}`, both},
		{"lite_image", `{"type":"response.create","input":[{"type":"additional_tools","tools":[{"type":"image_generation"}]}]}`, openAIWSHostedImageGeneration},
		{"split_carriers", `{"type":"response.create","tools":[{"type":"image_generation"}],"input":[{"type":"additional_tools","tools":[{"type":"web_search"}]}]}`, both},
		{"function_names", `{"type":"response.create","tools":[{"type":"function","name":"image_generation"},{"type":"custom","name":"web_search"}]}`, 0},
		{"unknown_tools", `{"type":"response.create","tools":[{"type":"file_search"},{"type":"image_generation_other"}]}`, 0},
	} {
		for _, format := range []string{"map", "raw", "bytes"} {
			t.Run(tc.name+"/"+format, func(t *testing.T) {
				conn := newOpenAIWSConn("capability", 42, &openAIWSCaptureConn{}, nil)
				defer conn.close()
				lease := &openAIWSConnLease{conn: conn}
				require.Zero(t, lease.missingHostedTools(both), "handshake does not commit tool capability")
				require.NoError(t, lease.WriteJSON(json.RawMessage(`{"type":"response.cancel"}`), time.Second))
				require.Zero(t, lease.missingHostedTools(both))
				var payload any
				switch format {
				case "map":
					require.NoError(t, json.Unmarshal([]byte(tc.raw), &payload))
				case "raw":
					payload = json.RawMessage(tc.raw)
				case "bytes":
					payload = []byte(tc.raw)
				}
				require.NoError(t, lease.WriteJSON(payload, time.Second))
				require.Equal(t, both&^tc.declared, lease.missingHostedTools(both))
				require.Zero(t, lease.missingHostedTools(tc.declared), "compatible subset remains reusable")
				require.NoError(t, lease.WriteJSON(json.RawMessage(`{"type":"response.create","tools":[{"type":"web_search"},{"type":"image_generation"}]}`), time.Second))
				require.Equal(t, both&^tc.declared, lease.missingHostedTools(both), "later declarations must not rewrite the first create")
				fresh := newOpenAIWSConn("different-socket", 42, &openAIWSCaptureConn{}, nil)
				defer fresh.close()
				require.Zero(t, (&openAIWSConnLease{conn: fresh}).missingHostedTools(both), "different sockets do not inherit declarations")
			})
		}
	}
	capture := &openAIWSCaptureConn{closed: true}
	conn := newOpenAIWSConn("failed-write", 42, capture, nil)
	defer conn.close()
	lease := &openAIWSConnLease{conn: conn}
	require.Error(t, lease.WriteJSON(map[string]any{"type": "response.create", "tools": []any{map[string]any{"type": "image_generation"}}}, time.Second))
	require.False(t, conn.responseCreateSent)
	require.Zero(t, conn.initialHostedTools)
}
