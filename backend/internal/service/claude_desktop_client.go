package service

import (
	"context"
	"regexp"

	"github.com/MACOS-DO/sub4api/internal/pkg/ctxkey"
)

// Desktop's bundled Code engine declares claude-desktop / claude-desktop-3p
// as its entrypoint. Do not accept arbitrary Electron/browser UAs or substrings.
// These are client-declared signals, not cryptographic client attestation.
var claudeDesktopUserAgent = regexp.MustCompile(`(?i)^claude-cli/\d+\.\d+\.\d+ \(external, claude-desktop(?:-3p)?(?:, [^()\r\n]+)?\)$`)

func IsClaudeDesktopUserAgent(userAgent string) bool {
	return claudeDesktopUserAgent.MatchString(userAgent)
}

func IsClaudeDesktopClient(ctx context.Context) bool {
	value, _ := ctx.Value(ctxkey.IsClaudeDesktopClient).(bool)
	return value
}

func SetClaudeDesktopClient(ctx context.Context, desktop bool) context.Context {
	return context.WithValue(ctx, ctxkey.IsClaudeDesktopClient, desktop)
}

// Desktop still passes the existing Code protocol validation, but its access
// decision must precede the shared Code flag so it cannot bypass the opt-in.
func (g *Group) AllowsClaudeClient(ctx context.Context) bool {
	if g == nil || !g.ClaudeCodeOnly {
		return true
	}
	if !IsClaudeCodeClient(ctx) {
		return false
	}
	if IsClaudeDesktopClient(ctx) {
		return g.Platform == PlatformAnthropic && g.AllowClaudeDesktop
	}
	return true
}

func (g *Group) ClaudeClientRestrictionMessage() string {
	if g != nil && g.Platform == PlatformAnthropic && g.AllowClaudeDesktop {
		return "This group only allows Claude Code and Claude Desktop clients"
	}
	return "This group only allows Claude Code clients"
}
