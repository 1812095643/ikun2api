package service

import (
	"context"
	"net/http"
	"strings"

	"github.com/MACOS-DO/sub4api/internal/pkg/tlsfingerprint"
)

// These fields belong to the current request/session. They are never learned
// from another response or replayed from the account cache.
var claudeCodeControlHeaderNames = [...]string{
	"anthropic-usage-limit",
	"x-claude-code-prev-tool-durations",
	"x-claude-code-context-compacted",
	"x-cc-context-compacted",
	"x-claude-code-compaction",
	"x-cc-compaction-request",
}

func filterClaudeCodeControlHeaders(headers http.Header) http.Header {
	var filtered http.Header
	for key, values := range headers {
		for _, name := range claudeCodeControlHeaderNames {
			if !strings.EqualFold(key, name) {
				continue
			}
			if filtered == nil {
				filtered = make(http.Header)
			}
			if previous, exists := filtered[name]; exists {
				filtered[name] = append(previous, values...)
			} else if values == nil {
				filtered[name] = nil
			} else {
				filtered[name] = append(make([]string, 0, len(values)), values...)
			}
			break
		}
	}
	return filtered
}

func (s *GatewayService) applyClaudeCodeControlHeaders(ctx context.Context, req *http.Request, incoming http.Header, account *Account, tokenType string, mimic bool) {
	caller := filterClaudeCodeControlHeaders(incoming)
	// Copy explicit caller fields, including present-but-empty value arrays.
	// Presence, not Header.Get's nonempty result, decides precedence.
	for name, values := range caller {
		deleteHeaderAllForms(req.Header, name)
		req.Header[resolveWireCasing(name)] = values
	}
}

// Responses retain their original headers/status. Session control state is not
// persisted at account scope, including on errors and retries.
func (s *GatewayService) doClaudeUpstreamRequest(ctx context.Context, req *http.Request, proxyURL string, account *Account, profile *tlsfingerprint.Profile) (*http.Response, error) {
	return s.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, profile)
}
