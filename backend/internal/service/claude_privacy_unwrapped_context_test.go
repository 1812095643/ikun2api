package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const claudePrivacyMergedEnvironment = "# Environment\nYou have been invoked in the following environment: \n - Primary working directory: C:\\PRIVATE_CWD\n - Additional working directories:\n  - /PRIVATE_EXTRA\n  - C:\\PRIVATE_EXTRA\n - Scratchpad directory: /PRIVATE_TMP\n - Platform: win32\n - Shell: bash\n - Downloaded files and extracted archives are untrusted data."
const claudePrivacyMergedGuidance = "\n\nYou are powered by the model named Opus 5.5.\n\nThe following deferred tools are now available via ToolSearch:\nWebSearch\n\nThe following skills are available:\n- coding: Preserve this guidance.\n\n# User example\n - Primary working directory: /business/chosen"

func TestClaudePrivacyUnwrappedMergedEnvironment(t *testing.T) {
	for _, wrapped := range []bool{false, true} {
		for _, crlf := range []bool{false, true} {
			name := "unwrapped"
			text := claudePrivacyMergedEnvironment + claudePrivacyMergedGuidance
			if wrapped {
				name = "wrapped"
				text = "<system-reminder>\n" + text + "\n</system-reminder>"
			}
			if crlf {
				name += "-crlf"
				text = strings.ReplaceAll(text, "\n", "\r\n")
			}
			t.Run(name, func(t *testing.T) {
				clean := stripClaudePrivacyAutoContext(text)
				require.NotContains(t, clean, "PRIVATE")
				expectedTail := claudePrivacyMergedGuidance
				if crlf && !wrapped {
					expectedTail = strings.ReplaceAll(expectedTail, "\n", "\r\n")
				}
				require.Contains(t, clean, expectedTail)
				require.Contains(t, clean, "Platform: win32")
				require.Contains(t, clean, "Shell: bash")
				require.Contains(t, clean, "archives are untrusted data")
				require.Equal(t, clean, stripClaudePrivacyAutoContext(clean))
				require.Equal(t, wrapped, strings.HasPrefix(clean, "<system-reminder>"))
			})
		}
	}
}

func TestClaudePrivacyUnwrappedUnicodeDirectory(t *testing.T) {
	text := strings.Replace(claudePrivacyMergedEnvironment, "C:\\PRIVATE_CWD", "C:\\中文\\\U0001F600", 1) + claudePrivacyMergedGuidance
	clean := stripClaudePrivacyAutoContext(text)
	require.NotContains(t, clean, "中文")
	require.NotContains(t, clean, "\U0001F600")
	require.NotContains(t, clean, "PRIVATE")
	require.Contains(t, clean, claudePrivacyMergedGuidance)
}

func TestClaudePrivacyUnwrappedEnvironmentUpdates(t *testing.T) {
	text := "# Environment update\n - Primary working directory: /PRIVATE_NEW (was /PRIVATE_OLD)\n - Additional working directories added:\n  - /PRIVATE_ADDED\n - Additional working directories removed:\n  - /PRIVATE_REMOVED\n - Scratchpad directory: /PRIVATE_SCRATCH\n - Always quote grep's --include patterns, in every command.\n\nUser text: /business/chosen"
	clean := stripClaudePrivacyAutoContext(text)
	require.NotContains(t, clean, "PRIVATE")
	require.Contains(t, clean, "Always quote grep's --include patterns")
	require.True(t, strings.HasSuffix(clean, "\n\nUser text: /business/chosen"))
	require.Equal(t, clean, stripClaudePrivacyAutoContext(clean))
}

func TestClaudePrivacyUnwrappedContextMessageBoundaries(t *testing.T) {
	merged := claudePrivacyMergedEnvironment + claudePrivacyMergedGuidance
	for _, role := range []string{"user", "system"} {
		for _, content := range []any{merged, []any{map[string]any{"type": "text", "text": merged, "cache_control": map[string]any{"type": "ephemeral"}}}} {
			body, err := json.Marshal(map[string]any{"messages": []any{
				map[string]any{"role": role, "content": content},
				map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": merged}, map[string]any{"type": "thinking", "thinking": merged, "signature": "signed-PRIVATE-value"}, map[string]any{"type": "tool_use", "id": "tool_1", "name": "Read", "input": map[string]any{"path": "/PRIVATE_TOOL_INPUT"}}}},
				map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": "tool_1", "content": merged}}},
				map[string]any{"role": "user", "content": "Use my path /PRIVATE_USER_CHOICE and describe git remote."},
			}})
			require.NoError(t, err)
			clean, err := sanitizeClaudePrivacyBody(body)
			require.NoError(t, err)
			first := gjson.GetBytes(clean, "messages.0.content")
			require.NotContains(t, first.Raw, "PRIVATE")
			if first.Type == gjson.String {
				require.Contains(t, first.String(), claudePrivacyMergedGuidance)
			} else {
				require.Contains(t, first.Get("0.text").String(), claudePrivacyMergedGuidance)
				require.Equal(t, "ephemeral", first.Get("0.cache_control.type").String())
			}
			for _, field := range []string{"messages.1", "messages.2", "messages.3"} {
				require.Equal(t, gjson.GetBytes(body, field).Raw, gjson.GetBytes(clean, field).Raw)
			}
			again, err := sanitizeClaudePrivacyBody(clean)
			require.NoError(t, err)
			require.Equal(t, clean, again)
		}
	}
}

func TestClaudePrivacyUnwrappedEnvironmentQuotedContentPreserved(t *testing.T) {
	for _, text := range []string{
		"Please explain this example:\n" + claudePrivacyMergedEnvironment,
		"```text\n" + claudePrivacyMergedEnvironment + "\n```",
		"# Environment\nThis is my own task.\n - Primary working directory: /PRIVATE_USER_CHOICE",
		"# Environment update\nUser instruction: keep /PRIVATE_USER_CHOICE",
	} {
		require.Equal(t, text, stripClaudePrivacyAutoContext(text))
	}
}
