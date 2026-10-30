package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

func claudeCodeHeadersKey(accountID int64) string {
	return fmt.Sprintf("claude_code_headers:%d", accountID)
}

// Only protocol defaults may survive in an account cache. Old session fields
// are filtered before decoding, so malformed historical values cannot revive them.
func persistentClaudeProtocolHeader(name string) bool {
	switch strings.ToLower(name) {
	case "anthropic-version", "anthropic-beta", "anthropic-dangerous-direct-browser-access":
		return true
	default:
		return false
	}
}

func (c *identityCache) GetClaudeCodeHeaders(ctx context.Context, accountID int64) (http.Header, error) {
	fields, err := c.rdb.HGetAll(ctx, claudeCodeHeadersKey(accountID)).Result()
	if err != nil {
		return nil, err
	}
	headers := make(http.Header, len(fields))
	for name, encoded := range fields {
		if !persistentClaudeProtocolHeader(name) {
			_ = c.rdb.HDel(ctx, claudeCodeHeadersKey(accountID), name).Err()
			continue
		}
		var values []string
		if err := json.Unmarshal([]byte(encoded), &values); err != nil {
			return nil, fmt.Errorf("decode Claude Code header %q: %w", name, err)
		}
		headers[name] = values
	}
	return headers, nil
}

func (c *identityCache) UpdateClaudeCodeHeaders(ctx context.Context, accountID int64, headers http.Header) error {
	if len(headers) == 0 {
		return nil
	}
	fields := make(map[string]interface{}, len(headers))
	for name, values := range headers {
		if !persistentClaudeProtocolHeader(name) {
			continue
		}
		encoded, err := json.Marshal(values)
		if err != nil {
			return err
		}
		fields[name] = string(encoded)
	}
	if len(fields) == 0 {
		return nil
	}
	// Partial HSET and expiry execute together, without a read/modify/write
	// race that could discard concurrent updates to other fields.
	key := claudeCodeHeadersKey(accountID)
	pipe := c.rdb.TxPipeline()
	pipe.HSet(ctx, key, fields)
	pipe.Expire(ctx, key, fingerprintTTL)
	_, err := pipe.Exec(ctx)
	return err
}
