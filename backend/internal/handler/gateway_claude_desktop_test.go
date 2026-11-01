package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGatewayClaudeDesktopCapturedEngineRequest(t *testing.T) {
	data, err := os.ReadFile("testdata/claude_desktop_gateway_request.json")
	require.NoError(t, err)
	var fixture struct {
		Headers map[string]string `json:"headers"`
		Body    json.RawMessage   `json:"body"`
	}
	require.NoError(t, json.Unmarshal(data, &fixture))
	c, _ := newHelperTestContext(http.MethodPost, "/v1/messages")
	for name, value := range fixture.Headers {
		c.Request.Header.Set(name, value)
	}
	parsed, err := service.ParseGatewayRequest(service.NewRequestBodyRef(fixture.Body), service.PlatformAnthropic)
	require.NoError(t, err)
	SetClaudeCodeClientContext(c, fixture.Body, parsed)
	require.True(t, service.IsClaudeCodeClient(c.Request.Context()))
	require.True(t, service.IsClaudeDesktopClient(c.Request.Context()))
	g := &service.Group{Platform: service.PlatformAnthropic, ClaudeCodeOnly: true}
	require.False(t, g.AllowsClaudeClient(c.Request.Context()))
	g.AllowClaudeDesktop = true
	require.True(t, g.AllowsClaudeClient(c.Request.Context()))
}

func TestGatewayClaudeDesktopHasSeparateGroupPermission(t *testing.T) {
	for _, path := range []string{"/v1/messages", "/v1/messages/count_tokens"} {
		for _, client := range []struct {
			name, ua string
			valid    bool
		}{
			{"cli", "claude-cli/2.1.316 (external, cli)", true},
			{"desktop_3p", "claude-cli/2.1.316 (external, claude-desktop-3p, agent-sdk/0.3.316)", true},
			{"desktop", "claude-cli/2.1.316 (external, claude-desktop)", true},
			{"curl", "curl/8.10.0", false},
			{"third_party", "python-httpx/0.28.1", false},
			{"browser", "Mozilla/5.0 Electron/40.0.0 Claude/4.4212.0", false},
		} {
			t.Run(path+"/"+client.name, func(t *testing.T) {
				for _, allowDesktop := range []bool{false, true} {
					c, rec := newHelperTestContext(http.MethodPost, path)
					c.Request.Header.Set("User-Agent", client.ua)
					c.Request.Header.Set("X-App", "claude-code")
					c.Request.Header.Set("anthropic-beta", "claude-code-20250219")
					c.Request.Header.Set("anthropic-version", "2023-06-01")
					SetClaudeCodeClientContext(c, validClaudeCodeBodyJSON(), nil)
					require.Equal(t, client.valid, service.IsClaudeCodeClient(c.Request.Context()))
					g := &service.Group{Platform: service.PlatformAnthropic, ClaudeCodeOnly: true, AllowClaudeDesktop: allowDesktop}
					h := &GatewayHandler{}
					expected := client.name == "cli" || ((client.name == "desktop" || client.name == "desktop_3p") && allowDesktop)
					rejected := h.rejectClaudeClientRestriction(c, &service.APIKey{Group: g})
					require.Equal(t, !expected, rejected)
					if rejected {
						require.Equal(t, http.StatusForbidden, rec.Code)
						require.Contains(t, rec.Body.String(), "permission_error")
					}
				}
			})
		}
	}
}

func TestGatewayClaudeDesktopStillRequiresProtocolValidation(t *testing.T) {
	c, _ := newHelperTestContext(http.MethodPost, "/v1/messages")
	c.Request.Header.Set("User-Agent", "claude-cli/2.1.316 (external, claude-desktop-3p)")
	SetClaudeCodeClientContext(c, []byte(`{"model":"claude-sonnet-5","messages":[]}`), nil)
	require.False(t, service.IsClaudeCodeClient(c.Request.Context()))
	require.False(t, service.IsClaudeDesktopClient(c.Request.Context()))
	require.True(t, (&GatewayHandler{}).rejectClaudeClientRestriction(c, &service.APIKey{Group: &service.Group{Platform: service.PlatformAnthropic, ClaudeCodeOnly: true, AllowClaudeDesktop: true}}))
	// Re-evaluating a context cannot inherit a previous Desktop classification.
	c.Request = c.Request.WithContext(service.SetClaudeDesktopClient(context.Background(), true))
	c.Request.Header.Set("User-Agent", "curl/8")
	SetClaudeCodeClientContext(c, nil, nil)
	require.False(t, service.IsClaudeDesktopClient(c.Request.Context()))
}
