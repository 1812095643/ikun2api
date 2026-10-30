package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MACOS-DO/sub4api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type claudeControlCache struct {
	stubIdentityCache
	headers  map[int64]http.Header
	readErr  error
	writeErr error
	updates  int
}

func (c *claudeControlCache) GetClaudeCodeHeaders(_ context.Context, accountID int64) (http.Header, error) {
	return c.headers[accountID].Clone(), c.readErr
}

func (c *claudeControlCache) UpdateClaudeCodeHeaders(_ context.Context, accountID int64, headers http.Header) error {
	if c.writeErr != nil {
		return c.writeErr
	}
	c.updates++
	if c.headers == nil {
		c.headers = make(map[int64]http.Header)
	}
	if c.headers[accountID] == nil {
		c.headers[accountID] = make(http.Header)
	}
	for name, values := range headers.Clone() {
		c.headers[accountID][name] = values
	}
	return nil
}

func buildClaudeControlTestRequest(t *testing.T, s *GatewayService, endpoint string, account *Account, incoming http.Header, mimic bool) *http.Request {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header = incoming
	body := []byte(`{"model":"claude-haiku-4-5","messages":[{"role":"user","content":"hello"}]}`)
	tokenType := "oauth"
	if account.Type == AccountTypeAPIKey {
		tokenType = "apikey"
	}
	var req *http.Request
	var err error
	if endpoint == "messages" {
		req, _, err = s.buildUpstreamRequest(context.Background(), c, account, body, "test-token", tokenType, "claude-haiku-4-5", false, mimic)
	} else {
		req, _, err = s.buildCountTokensRequest(context.Background(), c, account, body, "test-token", tokenType, "claude-haiku-4-5", mimic)
	}
	require.NoError(t, err)
	t.Cleanup(func() { _ = req.Body.Close() })
	return req
}

func TestClaudeCodeControlHeadersDoNotCrossSessions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []string{"messages", "count_tokens"} {
		for _, accountType := range []string{AccountTypeOAuth, AccountTypeSetupToken, AccountTypeAPIKey} {
			t.Run(endpoint+accountType, func(t *testing.T) {
				old := http.Header{}
				for _, name := range claudeCodeControlHeaderNames {
					old[name] = []string{"session-a"}
				}
				cache := &claudeControlCache{headers: map[int64]http.Header{41: old}}
				upstream := &anthropicHTTPUpstreamRecorder{resp: &http.Response{StatusCode: 403, Header: old.Clone(), Body: io.NopCloser(strings.NewReader("unchanged"))}}
				svc := &GatewayService{cfg: &config.Config{}, identityService: newTestIdentityService(cache), httpUpstream: upstream}
				account := &Account{ID: 41, Platform: PlatformAnthropic, Type: accountType}
				for _, mimic := range []bool{false, true} {
					// Historical account defaults and responses from A cannot enter B.
					b := buildClaudeControlTestRequest(t, svc, endpoint, account, http.Header{}, mimic)
					require.Empty(t, filterClaudeCodeControlHeaders(b.Header))
					resp, err := svc.doClaudeUpstreamRequest(context.Background(), b, "", account, nil)
					require.NoError(t, err)
					require.Same(t, upstream.resp, resp)
					require.Equal(t, 403, resp.StatusCode)
					require.Zero(t, cache.updates)
					retry := buildClaudeControlTestRequest(t, svc, endpoint, account, http.Header{}, mimic)
					require.Empty(t, filterClaudeCodeControlHeaders(retry.Header))
					caller := old.Clone()
					caller["x-cc-compaction-request"] = []string{""}
					caller["x-claude-code-compaction"] = []string{}
					caller["x-cc-context-compacted"] = nil
					own := buildClaudeControlTestRequest(t, svc, endpoint, account, caller, mimic)
					require.Equal(t, caller, filterClaudeCodeControlHeaders(own.Header))
				}
				raw, err := io.ReadAll(upstream.resp.Body)
				require.NoError(t, err)
				require.Equal(t, "unchanged", string(raw))
				_ = upstream.resp.Body.Close()
			})
		}
	}
}
