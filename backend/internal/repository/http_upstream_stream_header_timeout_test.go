package repository

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/config"
	pkghttputil "github.com/MACOS-DO/sub4api/internal/pkg/httputil"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestHTTPUpstreamStreamHeaderTimeoutPolicy(t *testing.T) {
	svc := &httpUpstreamService{cfg: &config.Config{Gateway: config.GatewayConfig{StreamResponseHeaderTimeout: 90}}}
	require.Equal(t, 90*time.Second, svc.streamResponseHeaderTimeout(service.HTTPUpstreamProfileDefault))
	for _, profile := range []service.HTTPUpstreamProfile{
		service.HTTPUpstreamProfileOpenAI, service.HTTPUpstreamProfileOpenAIHarvest,
		service.HTTPUpstreamProfileGrok, service.HTTPUpstreamProfileLongStream,
	} {
		require.Zero(t, svc.streamResponseHeaderTimeout(profile), "独立协议通道不得继承通用流式超时")
	}
	svc.cfg.Gateway.StreamResponseHeaderTimeout = 0
	require.Zero(t, svc.streamResponseHeaderTimeout(service.HTTPUpstreamProfileDefault))
	svc.cfg = nil
	require.Zero(t, svc.streamResponseHeaderTimeout(service.HTTPUpstreamProfileDefault))
}

func TestHTTPUpstreamStreamHeaderTimeoutAbortsHeaderWait(t *testing.T) {
	for _, http2 := range []bool{false, true} {
		name := "http1"
		if http2 {
			name = "http2"
		}
		t.Run(name, func(t *testing.T) {
			release := make(chan struct{})
			srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				select {
				case <-r.Context().Done():
				case <-release:
				}
			}))
			srv.EnableHTTP2 = http2
			srv.StartTLS()
			defer srv.Close()
			defer close(release)
			req, err := http.NewRequestWithContext(pkghttputil.MarkStreamRequest(context.Background()), http.MethodPost, srv.URL, strings.NewReader("{}"))
			require.NoError(t, err)
			started := time.Now()
			resp, err := doUpstreamRequestWithHeaderTimeout(srv.Client(), req, 0, 80*time.Millisecond)
			require.Nil(t, resp)
			require.ErrorIs(t, err, pkghttputil.ErrStreamResponseHeaderTimeout)
			require.NotErrorIs(t, err, context.Canceled)
			require.NoError(t, req.Context().Err(), "一次上游尝试不能取消后续切号使用的上下文")
			require.Less(t, time.Since(started), 2*time.Second)
		})
	}
}

func TestHTTPUpstreamStreamHeaderTimeoutKeepsOtherRequests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(120 * time.Millisecond)
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()
	for _, tc := range []struct {
		name   string
		ctx    context.Context
		budget time.Duration
	}{
		{"non-streaming", context.Background(), 40 * time.Millisecond},
		{"disabled", pkghttputil.MarkStreamRequest(context.Background()), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(tc.ctx, http.MethodPost, srv.URL, strings.NewReader("{}"))
			require.NoError(t, err)
			resp, err := doUpstreamRequestWithHeaderTimeout(srv.Client(), req, 0, tc.budget)
			require.NoError(t, err)
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.Equal(t, "ok", string(body))
		})
	}
}

func TestHTTPUpstreamStreamHeaderTimeoutStopsAtHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_ = http.NewResponseController(w).Flush()
		time.Sleep(180 * time.Millisecond)
		_, _ = io.WriteString(w, "data: {\"usage\":7}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()
	req, err := http.NewRequestWithContext(pkghttputil.MarkStreamRequest(context.Background()), http.MethodPost, srv.URL, nil)
	require.NoError(t, err)
	resp, err := doUpstreamRequestWithHeaderTimeout(srv.Client(), req, 0, 60*time.Millisecond)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Contains(t, string(body), "[DONE]")
	require.Contains(t, string(body), "\"usage\":7")
}

func TestHTTPUpstreamStreamHeaderTimeoutExcludesDialAndUpload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()
	t.Run("slow dial", func(t *testing.T) {
		transport := srv.Client().Transport.(*http.Transport).Clone()
		defer transport.CloseIdleConnections()
		transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			select {
			case <-time.After(180 * time.Millisecond):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return (&net.Dialer{}).DialContext(ctx, network, address)
		}
		client := &http.Client{Transport: transport}
		req, err := http.NewRequestWithContext(pkghttputil.MarkStreamRequest(context.Background()), http.MethodPost, srv.URL, nil)
		require.NoError(t, err)
		resp, err := doUpstreamRequestWithHeaderTimeout(client, req, 0, 60*time.Millisecond)
		require.NoError(t, err)
		defer resp.Body.Close()
	})
	t.Run("slow upload", func(t *testing.T) {
		reader, writer := io.Pipe()
		defer reader.Close()
		defer writer.Close()
		done := make(chan error, 1)
		go func() {
			_, err := io.WriteString(writer, "first")
			if err == nil {
				time.Sleep(180 * time.Millisecond)
				_, err = io.WriteString(writer, "last")
			}
			_ = writer.CloseWithError(err)
			done <- err
		}()
		req, err := http.NewRequestWithContext(pkghttputil.MarkStreamRequest(context.Background()), http.MethodPost, srv.URL, reader)
		require.NoError(t, err)
		resp, err := doUpstreamRequestWithHeaderTimeout(srv.Client(), req, 0, 60*time.Millisecond)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.NoError(t, <-done)
	})
}

func TestHTTPUpstreamStreamHeaderTimeoutKeepsClientCancellation(t *testing.T) {
	ready := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(ready)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { <-ready; cancel() }()
	req, err := http.NewRequestWithContext(pkghttputil.MarkStreamRequest(ctx), http.MethodPost, srv.URL, nil)
	require.NoError(t, err)
	resp, err := doUpstreamRequestWithHeaderTimeout(srv.Client(), req, 0, time.Second)
	require.Nil(t, resp)
	require.ErrorIs(t, err, context.Canceled)
	require.NotErrorIs(t, err, pkghttputil.ErrStreamResponseHeaderTimeout)
}

func TestHTTPUpstreamStreamHeaderTimeoutLateWriteCallbackCannotAbortBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://upstream.test/v1/messages", nil)
	req = req.WithContext(pkghttputil.MarkStreamRequest(req.Context()))
	tracked, finish := withStreamResponseHeaderTimeout(req, 20*time.Millisecond)
	require.False(t, finish())
	httptrace.ContextClientTrace(tracked.Context()).WroteRequest(httptrace.WroteRequestInfo{})
	time.Sleep(60 * time.Millisecond)
	require.NoError(t, tracked.Context().Err())
}
