package repository

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	pkghttputil "github.com/MACOS-DO/sub4api/internal/pkg/httputil"
	"github.com/stretchr/testify/require"
)

// A streaming request whose upstream never sends headers must be aborted at the
// stream header budget with ErrStreamResponseHeaderTimeout (not context.Canceled),
// so the gateway fails over to another account instead of waiting out the client.
func TestUpstreamStreamHeaderTimeoutAbortsHeaderWait(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/messages", nil)
	require.NoError(t, err)
	req = req.WithContext(pkghttputil.MarkStreamRequest(context.Background()))

	started := time.Now()
	resp, err := doUpstreamRequestWithHeaderTimeout(srv.Client(), req, 0, 80*time.Millisecond)
	require.Nil(t, resp)
	require.ErrorIs(t, err, pkghttputil.ErrStreamResponseHeaderTimeout)
	require.NotErrorIs(t, err, context.Canceled)
	require.Less(t, time.Since(started), 2*time.Second)
}

// Non-streaming requests and requests without a budget keep waiting: their
// headers only arrive after the full response is generated.
func TestUpstreamStreamHeaderTimeoutNotApplied(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(150 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	tests := map[string]struct {
		ctx     context.Context
		budget  time.Duration
		wantErr bool
	}{
		"non-streaming request keeps no stream budget": {
			ctx:    context.Background(),
			budget: 50 * time.Millisecond,
		},
		"streaming request without budget": {
			ctx:    pkghttputil.MarkStreamRequest(context.Background()),
			budget: 0,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, srv.URL, nil)
			require.NoError(t, err)
			req = req.WithContext(tc.ctx)
			resp, err := doUpstreamRequestWithHeaderTimeout(srv.Client(), req, 0, tc.budget)
			require.NoError(t, err)
			require.NotNil(t, resp)
			_ = resp.Body.Close()
		})
	}
}

// Headers that arrive inside the budget must not be aborted mid-stream: the
// timer only covers the header wait, never the body.
func TestUpstreamStreamHeaderTimeoutStopsAtHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: first\n\n"))
		http.NewResponseController(w).Flush()
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte("data: late\n\n"))
	}))
	defer srv.Close()

	req, err := http.NewRequest(http.MethodPost, srv.URL, nil)
	require.NoError(t, err)
	req = req.WithContext(pkghttputil.MarkStreamRequest(context.Background()))
	resp, err := doUpstreamRequestWithHeaderTimeout(srv.Client(), req, 0, 80*time.Millisecond)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	time.Sleep(300 * time.Millisecond)
	buf := make([]byte, 64)
	n, err := resp.Body.Read(buf)
	require.NoError(t, err)
	require.NotEmpty(t, buf[:n])
}
