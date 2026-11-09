package repository

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/MACOS-DO/sub4api/internal/pkg/tlsfingerprint"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/stretchr/testify/require"
)

type proxyGuardFixtureStream string

func (s proxyGuardFixtureStream) Do(req *http.Request, proxy string, id int64, c int) (*http.Response, error) {
	return s.DoWithTLS(req, proxy, id, c, nil)
}
func (s proxyGuardFixtureStream) DoWithTLS(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(string(s)))}, nil
}

func TestProxyFailureGuardStreamEOFRequiresClaudeTerminalEvent(t *testing.T) {
	for _, tc := range []struct {
		name   string
		data   string
		broken bool
	}{
		{"truncated", "event: content_block_delta\ndata: {\"type\":\"content_block_delta\"}\n\n", true},
		{"completed", "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n", false},
		{"error_is_provider_response", "event: error\ndata: {\"type\":\"error\"}\n\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &guardTransportStore{proxy: &service.Proxy{ID: 8, Protocol: "http", Host: "127.0.0.1", Port: 8081, Status: service.StatusActive}}
			guard := service.NewProxyFailureGuard(store, nil)
			upstream := &guardedHTTPUpstream{base: proxyGuardFixtureStream(tc.data), guard: guard}
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://api.anthropic.com/v1/messages", strings.NewReader(`{}`))
			resp, err := upstream.Do(req, "http://127.0.0.1:8081", 1, 1)
			require.NoError(t, err)
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			require.Equal(t, tc.data, string(body))
			if tc.broken {
				require.ErrorIs(t, err, service.ErrProxyQuarantined)
				require.NotNil(t, store.state)
			} else {
				require.NoError(t, err)
				require.Nil(t, store.state)
			}
		})
	}
}
