package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClaudePrivacyExpiredScratchpad326(t *testing.T) {
	reminder := "# Environment update\n - The scratchpad directory announced earlier (/PRIVATE_EXPIRED) is out of use: put no new files there. Files that you wrote there may still be at that path. To go on working with one, first copy it under the primary working directory: a tool that attaches or publishes a file may refuse the old path.\n\nUser instruction: keep /BUSINESS_PATH"
	for _, wrapped := range []bool{false, true} {
		for _, crlf := range []bool{false, true} {
			text := reminder
			if wrapped {
				text = "<system-reminder>\n" + text + "\n</system-reminder>"
			}
			if crlf {
				text = strings.ReplaceAll(text, "\n", "\r\n")
			}
			clean := stripClaudePrivacyAutoContext(text)
			require.NotContains(t, clean, "PRIVATE_EXPIRED")
			require.Contains(t, clean, "put no new files there")
			require.Contains(t, clean, "first copy it under the primary working directory")
			require.Contains(t, clean, "/BUSINESS_PATH")
			require.Equal(t, clean, stripClaudePrivacyAutoContext(clean))
		}
	}
	userText := strings.TrimPrefix(reminder, "# Environment update\n")
	require.Equal(t, userText, stripClaudePrivacyAutoContext(userText))
}
