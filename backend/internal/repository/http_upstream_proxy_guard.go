package repository

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/MACOS-DO/sub4api/internal/config"
	"github.com/MACOS-DO/sub4api/internal/pkg/httpclient"
	"github.com/MACOS-DO/sub4api/internal/pkg/proxyurl"
	"github.com/MACOS-DO/sub4api/internal/pkg/tlsfingerprint"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/tidwall/gjson"
)

type guardedHTTPUpstream struct {
	base  service.HTTPUpstream
	guard *service.ProxyFailureGuard
	idle  time.Duration
}

func NewGuardedHTTPUpstream(cfg *config.Config, guard *service.ProxyFailureGuard) service.HTTPUpstream {
	idle := time.Duration(0)
	if cfg != nil && cfg.Gateway.StreamDataIntervalTimeout > 0 {
		idle = time.Duration(cfg.Gateway.StreamDataIntervalTimeout) * time.Second
	}
	return &guardedHTTPUpstream{base: NewHTTPUpstream(cfg), guard: guard, idle: idle}
}

func (s *guardedHTTPUpstream) Do(req *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
	return s.DoWithTLS(req, proxy, id, concurrency, nil)
}

func (s *guardedHTTPUpstream) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	// Non-account public fetches retain their existing policy. Every assigned
	// account, including quota checks, telemetry and administrator tests, gates.
	if id <= 0 {
		return s.base.DoWithTLS(req, proxy, id, concurrency, profile)
	}
	lease, err := s.guard.Begin(req.Context(), id, proxy, req.URL.String())
	if err != nil {
		return nil, err
	}
	resp, err := s.base.DoWithTLS(req.WithContext(lease.Context()), proxy, id, concurrency, profile)
	if err != nil {
		err = lease.Failure(err)
		if errors.Is(err, service.ErrProxyQuarantined) {
			s.dropProxyConnections(proxy)
		}
		lease.Close()
		return nil, err
	}
	if resp.StatusCode == http.StatusProxyAuthRequired {
		_ = resp.Body.Close()
		err = lease.Failure(fmt.Errorf("proxy authentication required"))
		lease.Close()
		return nil, err
	}
	body := &proxyGuardResponseBody{ReadCloser: resp.Body, lease: lease}
	body.onFailure = func() { s.dropProxyConnections(proxy) }
	isSSE := strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream")
	body.anthropicSSE = isSSE && req.URL != nil && strings.HasSuffix(req.URL.Path, "/messages") && resp.StatusCode >= 200 && resp.StatusCode < 300
	if s.idle > 0 && isSSE {
		body.idle = s.idle
		body.timerMu.Lock()
		body.timer = time.AfterFunc(s.idle, func() {
			body.timerMu.Lock()
			if body.closed {
				body.timerMu.Unlock()
				return
			}
			body.timerMu.Unlock()
			_ = body.lease.Failure(proxyGuardIdleTimeout{})
			body.onFailure()
			_ = body.Close()
		})
		body.timerMu.Unlock()
	}
	resp.Body = body
	return resp, nil
}

type proxyGuardResponseBody struct {
	io.ReadCloser
	lease          *service.ProxyRequestLease
	once           sync.Once
	err            error
	idle           time.Duration
	timer          *time.Timer
	timerMu        sync.Mutex
	closed         bool
	onFailure      func()
	anthropicSSE   bool
	sseLine        []byte
	sseTerminal    bool
	sseLineTooLong bool
}

func (b *proxyGuardResponseBody) Read(data []byte) (int, error) {
	n, err := b.ReadCloser.Read(data)
	if b.anthropicSSE && n > 0 {
		b.observeSSE(data[:n])
	}
	if n > 0 {
		b.timerMu.Lock()
		if b.timer != nil && !b.closed {
			b.timer.Reset(b.idle)
		}
		b.timerMu.Unlock()
	}
	if err == io.EOF && b.anthropicSSE && !b.sseTerminal {
		err = io.ErrUnexpectedEOF
	}
	if err != nil && err != io.EOF {
		err = b.lease.Failure(err)
		if errors.Is(err, service.ErrProxyQuarantined) && b.onFailure != nil {
			b.onFailure()
		}
	}
	return n, err
}

func (s *guardedHTTPUpstream) dropProxyConnections(proxy string) {
	base, ok := s.base.(*httpUpstreamService)
	if !ok {
		return
	}
	key, _, err := normalizeProxyURL(proxy)
	if err != nil {
		return
	}
	base.mu.Lock()
	defer base.mu.Unlock()
	for cacheKey, entry := range base.clients {
		if entry.proxyKey == key {
			base.removeClientLocked(cacheKey, entry)
		}
	}
}

func (b *proxyGuardResponseBody) observeSSE(data []byte) {
	for len(data) > 0 {
		index := bytes.IndexByte(data, '\n')
		part := data
		if index >= 0 {
			part = data[:index]
		}
		// Keep only a bounded event line to recognize terminal metadata. It
		// never becomes audit data or retained thinking history.
		if !b.sseLineTooLong && len(b.sseLine)+len(part) <= 4096 {
			b.sseLine = append(b.sseLine, part...)
		} else {
			b.sseLineTooLong = true
		}
		if index < 0 {
			return
		}
		line := bytes.TrimSpace(b.sseLine)
		if !b.sseLineTooLong && (bytes.Equal(line, []byte("event: message_stop")) || bytes.Equal(line, []byte("event: error"))) {
			b.sseTerminal = true
		}
		if !b.sseLineTooLong && bytes.HasPrefix(line, []byte("data:")) {
			kind := gjson.GetBytes(bytes.TrimSpace(line[5:]), "type").String()
			if kind == "message_stop" || kind == "error" {
				b.sseTerminal = true
			}
		}
		b.sseLine = b.sseLine[:0]
		b.sseLineTooLong = false
		data = data[index+1:]
	}
}

func (b *proxyGuardResponseBody) Close() error {
	b.once.Do(func() {
		b.timerMu.Lock()
		b.closed = true
		if b.timer != nil {
			b.timer.Stop()
		}
		b.timerMu.Unlock()
		b.lease.Close()
		b.err = b.ReadCloser.Close()
	})
	return b.err
}

type proxyGuardIdleTimeout struct{}

func (proxyGuardIdleTimeout) Error() string   { return "proxied upstream stream timed out" }
func (proxyGuardIdleTimeout) Timeout() bool   { return true }
func (proxyGuardIdleTimeout) Temporary() bool { return true }

type proxyRecoveryProber struct {
	cfg *config.Config
}

func NewProxyRecoveryProber(cfg *config.Config) service.ProxyRecoveryProber {
	return &proxyRecoveryProber{cfg: cfg}
}

func (p *proxyRecoveryProber) ProbeAccountProxy(ctx context.Context, proxyURL, origin string) error {
	_, parsed, err := proxyurl.Parse(proxyURL)
	if err != nil || parsed == nil {
		return fmt.Errorf("proxy required for recovery probe")
	}
	allowPrivate := p.cfg != nil && p.cfg.Security.URLAllowlist.AllowPrivateHosts
	client, err := httpclient.GetClient(httpclient.Options{ProxyURL: proxyURL, Timeout: 8 * time.Second,
		ValidateResolvedIP: true, AllowPrivateHosts: allowPrivate})
	if err != nil {
		return err
	}
	copy := *client
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	request, err := http.NewRequestWithContext(ctx, http.MethodHead, origin, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "IKUN2api-proxy-health-check/1")
	response, err := copy.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 500 || response.StatusCode == http.StatusProxyAuthRequired {
		return fmt.Errorf("proxy recovery probe unsuccessful")
	}
	return nil
}
