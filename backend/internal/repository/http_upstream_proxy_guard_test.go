package repository

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/config"
	"github.com/MACOS-DO/sub4api/internal/pkg/tlsfingerprint"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/stretchr/testify/require"
)

type guardTransportStore struct {
	mu    sync.Mutex
	state *service.ProxyFailureState
	proxy *service.Proxy
}

func (s *guardTransportStore) GetAccountProxySnapshot(context.Context, int64) (*service.AccountProxySnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.proxy.ID
	return &service.AccountProxySnapshot{AccountID: 1, Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth, Active: true, ProxyID: &id, Proxy: s.proxy, State: s.state}, nil
}
func (s *guardTransportStore) QuarantineAccountProxy(_ context.Context, _ *service.AccountProxySnapshot, state service.ProxyFailureState) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = &state
	return true, nil
}
func (s *guardTransportStore) ClearAccountProxyQuarantine(context.Context, *service.AccountProxySnapshot) (bool, error) {
	return false, nil
}
func (s *guardTransportStore) ListProxyQuarantinedAccounts(context.Context, int64, int) ([]int64, error) {
	return nil, nil
}

func guardTestProxy(t *testing.T, raw string) *service.Proxy {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	port, err := strconv.Atoi(u.Port())
	require.NoError(t, err)
	return &service.Proxy{ID: 8, Protocol: u.Scheme, Host: u.Hostname(), Port: port, Status: service.StatusActive}
}

func TestProxyFailureGuardTransportNeverFallsBackToDirect(t *testing.T) {
	for _, withTLS := range []bool{false, true} {
		t.Run(strconv.FormatBool(withTLS), func(t *testing.T) {
			var directHits, proxyHits atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { directHits.Add(1); w.Write([]byte("direct")) }))
			defer target.Close()
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { proxyHits.Add(1); w.Write([]byte("via-proxy")) }))
			store := &guardTransportStore{proxy: guardTestProxy(t, proxy.URL)}
			guard := service.NewProxyFailureGuard(store, nil)
			cfg := &config.Config{}
			cfg.Security.URLAllowlist.AllowPrivateHosts = true
			upstream := NewGuardedHTTPUpstream(cfg, guard)
			send := func() (*http.Response, error) {
				req, err := http.NewRequest(http.MethodGet, target.URL, nil)
				require.NoError(t, err)
				if withTLS {
					return upstream.DoWithTLS(req, proxy.URL, 1, 1, &tlsfingerprint.Profile{Name: "proxy-test"})
				}
				return upstream.Do(req, proxy.URL, 1, 1)
			}
			resp, err := send()
			require.NoError(t, err)
			b, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			resp.Body.Close()
			require.Equal(t, "via-proxy", string(b))
			proxy.Close()
			resp, err = send()
			require.Nil(t, resp)
			require.ErrorIs(t, err, service.ErrProxyQuarantined)
			require.Zero(t, directHits.Load())
			require.Equal(t, int32(1), proxyHits.Load())
			_, err = send()
			require.ErrorIs(t, err, service.ErrProxyQuarantined)
			require.Zero(t, directHits.Load())
		})
	}
}

func TestProxyFailureGuardTLSFingerprintCannotReachTargetWhenProxyIsOffline(t *testing.T) {
	for _, withTLS := range []bool{false, true} {
		t.Run(strconv.FormatBool(withTLS), func(t *testing.T) {
			var directHits atomic.Int32
			target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
			target.Config.ConnState = func(_ net.Conn, state http.ConnState) {
				if state == http.StateNew {
					directHits.Add(1)
				}
			}
			target.StartTLS()
			defer target.Close()
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("offline proxy handler reached") }))
			proxyURL := proxy.URL
			proxy.Close()
			store := &guardTransportStore{proxy: guardTestProxy(t, proxyURL)}
			cfg := &config.Config{}
			cfg.Security.URLAllowlist.AllowPrivateHosts = true
			upstream := NewGuardedHTTPUpstream(cfg, service.NewProxyFailureGuard(store, nil))
			req, _ := http.NewRequest(http.MethodGet, target.URL, nil)
			var err error
			if withTLS {
				_, err = upstream.DoWithTLS(req, proxyURL, 1, 1, &tlsfingerprint.Profile{Name: "offline-proxy"})
			} else {
				_, err = upstream.Do(req, proxyURL, 1, 1)
			}
			require.ErrorIs(t, err, service.ErrProxyQuarantined)
			require.Zero(t, directHits.Load())
			require.NotNil(t, store.state)
		})
	}
}

func TestProxyFailureGuardTransportHandlesProxy407AndMissingProxy(t *testing.T) {
	var requests atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusProxyAuthRequired)
	}))
	defer proxy.Close()
	store := &guardTransportStore{proxy: guardTestProxy(t, proxy.URL)}
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.AllowPrivateHosts = true
	upstream := NewGuardedHTTPUpstream(cfg, service.NewProxyFailureGuard(store, nil))
	req, _ := http.NewRequest(http.MethodGet, "http://unresolved.example/", nil)
	_, err := upstream.Do(req, proxy.URL, 1, 1)
	require.ErrorIs(t, err, service.ErrProxyQuarantined)
	require.Equal(t, int32(1), requests.Load())
	store.state = nil
	_, err = upstream.Do(req, "", 1, 1)
	require.ErrorIs(t, err, service.ErrProxyQuarantined)
	require.Equal(t, int32(1), requests.Load())
}

type guardBlockedBody struct{ ctx context.Context }

func (b guardBlockedBody) Read([]byte) (int, error) { <-b.ctx.Done(); return 0, b.ctx.Err() }
func (b guardBlockedBody) Close() error             { return nil }

type guardStreamingUpstream struct{}

func (guardStreamingUpstream) Do(r *http.Request, p string, id int64, c int) (*http.Response, error) {
	return guardStreamingUpstream{}.DoWithTLS(r, p, id, c, nil)
}
func (guardStreamingUpstream) DoWithTLS(r *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: guardBlockedBody{r.Context()}}, nil
}

func TestProxyFailureGuardTransportIdleTimeoutStopsBodyAndQuarantines(t *testing.T) {
	store := &guardTransportStore{proxy: &service.Proxy{ID: 8, Protocol: "http", Host: "127.0.0.1", Port: 8081, Status: service.StatusActive}}
	guard := service.NewProxyFailureGuard(store, nil)
	upstream := &guardedHTTPUpstream{base: guardStreamingUpstream{}, guard: guard, idle: 30 * time.Millisecond}
	req, _ := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", strings.NewReader(`{}`))
	resp, err := upstream.Do(req, "http://127.0.0.1:8081", 1, 1)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { _, err := io.ReadAll(resp.Body); done <- err }()
	select {
	case err := <-done:
		require.ErrorIs(t, err, service.ErrProxyQuarantined)
	case <-time.After(time.Second):
		t.Fatal("idle stream was not cancelled")
	}
	resp.Body.Close()
	store.mu.Lock()
	require.NotNil(t, store.state)
	store.mu.Unlock()
}

func TestProxyFailureGuardRecoveryProbeRequiresWorkingProxy(t *testing.T) {
	var directHits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { directHits.Add(1) }))
	defer target.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodHead, r.Method)
		require.Empty(t, r.Header.Get("Authorization"))
		w.WriteHeader(200)
	}))
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.AllowPrivateHosts = true
	prober := NewProxyRecoveryProber(cfg)
	require.NoError(t, prober.ProbeAccountProxy(context.Background(), proxy.URL, target.URL))
	proxy.Close()
	require.Error(t, prober.ProbeAccountProxy(context.Background(), proxy.URL, target.URL))
	require.Zero(t, directHits.Load())
	require.Error(t, prober.ProbeAccountProxy(context.Background(), "", target.URL))
}
