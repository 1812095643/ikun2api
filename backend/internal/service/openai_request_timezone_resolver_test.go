package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type timezoneProberFixture struct {
	calls    atomic.Int32
	urlMu    sync.Mutex
	urls     []string
	timezone string
	err      error
	block    <-chan struct{}
}

func (p *timezoneProberFixture) ProbeProxy(ctx context.Context, url string) (*ProxyExitInfo, int64, error) {
	p.calls.Add(1)
	p.urlMu.Lock()
	p.urls = append(p.urls, url)
	p.urlMu.Unlock()
	if p.block != nil {
		select {
		case <-p.block:
		case <-ctx.Done():
			return nil, 0, ctx.Err()
		}
	}
	return &ProxyExitInfo{Timezone: p.timezone}, 1, p.err
}

type timezoneCacheFixture struct {
	ProxyLatencyCache
	info *ProxyLatencyInfo
}

func (c *timezoneCacheFixture) GetProxyLatencies(context.Context, []int64) (map[int64]*ProxyLatencyInfo, error) {
	return map[int64]*ProxyLatencyInfo{1: c.info}, nil
}
func timezoneAccountFixture() *Account {
	id := int64(1)
	return &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ProxyID: &id, Proxy: &Proxy{ID: 1, Protocol: "http", Host: "fixture.invalid", Port: 1080}}
}
func timezoneSettingsFixture(t *testing.T, auto bool, fallback string) *SettingService {
	t.Helper()
	s := NewSettingService(&openAIFastPolicyRepoStub{values: map[string]string{SettingKeyOpenAIRequestTimezoneSettings: `{"auto_detect_proxy":` + fmtBool(auto) + `,"default_timezone":"` + fallback + `"}`}}, &config.Config{})
	return s
}
func TestOpenAIRequestTimezoneResolverAutomaticAndManual(t *testing.T) {
	ctx := context.Background()
	block := make(chan struct{})
	prober := &timezoneProberFixture{timezone: "America/Los_Angeles", block: block}
	r := NewOpenAIRequestTimezoneResolver(timezoneSettingsFixture(t, true, "Europe/London"), nil, prober)
	a := timezoneAccountFixture()
	start := time.Now()
	require.Equal(t, "Europe/London", r.Resolve(ctx, a))
	require.Less(t, time.Since(start), 200*time.Millisecond, "lookup must not block a model request")
	for i := 0; i < 30; i++ {
		require.Equal(t, "Europe/London", r.Resolve(ctx, a))
	}
	require.Eventually(t, func() bool { return prober.calls.Load() == 1 }, time.Second, time.Millisecond)
	close(block)
	require.Eventually(t, func() bool { return r.Resolve(ctx, a) == "America/Los_Angeles" }, time.Second, time.Millisecond)
	require.Equal(t, int32(1), prober.calls.Load(), "requests must share the cached result")
	a.Extra = map[string]any{openAIRequestTimezoneExtraKey: "Asia/Tokyo"}
	require.Equal(t, "Asia/Tokyo", r.Resolve(ctx, a), "manual selection takes precedence")
	a.Extra = nil
	a.ProxyID = nil
	a.Proxy = nil
	require.Equal(t, "Europe/London", r.Resolve(ctx, a), "direct routes use configured fallback without probing server or client IPs")
	require.Equal(t, int32(1), prober.calls.Load())
}
func TestOpenAIRequestTimezoneResolverDisabledAndFailures(t *testing.T) {
	p := &timezoneProberFixture{timezone: "America/Los_Angeles"}
	r := NewOpenAIRequestTimezoneResolver(timezoneSettingsFixture(t, false, "Asia/Tokyo"), nil, p)
	require.Equal(t, "Asia/Tokyo", r.Resolve(context.Background(), timezoneAccountFixture()))
	require.Zero(t, p.calls.Load())
	p = &timezoneProberFixture{err: errors.New("fixture proxy unreachable")}
	r = NewOpenAIRequestTimezoneResolver(timezoneSettingsFixture(t, true, "Asia/Tokyo"), nil, p)
	for i := 0; i < 20; i++ {
		require.Equal(t, "Asia/Tokyo", r.Resolve(context.Background(), timezoneAccountFixture()))
	}
	require.Eventually(t, func() bool { return p.calls.Load() == 1 }, time.Second, time.Millisecond)
	r.mu.Lock()
	entry := r.entries[proxyRouteFingerprint(timezoneAccountFixture().Proxy)]
	r.mu.Unlock()
	require.Eventually(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return !entry.inFlight }, time.Second, time.Millisecond)
	require.Equal(t, "Asia/Tokyo", r.Resolve(context.Background(), timezoneAccountFixture()))
	require.Equal(t, int32(1), p.calls.Load(), "failed probes use backoff")
}
func TestOpenAIRequestTimezoneResolverRouteAwareCache(t *testing.T) {
	a := timezoneAccountFixture()
	p := &timezoneProberFixture{timezone: "Asia/Tokyo"}
	c := &timezoneCacheFixture{info: &ProxyLatencyInfo{Timezone: "Europe/London", ProxyRouteFingerprint: proxyRouteFingerprint(a.Proxy), UpdatedAt: time.Now()}}
	r := NewOpenAIRequestTimezoneResolver(nil, c, p)
	r.Resolve(context.Background(), a)
	require.Eventually(t, func() bool { return r.Resolve(context.Background(), a) == "Europe/London" }, time.Second, time.Millisecond)
	require.Zero(t, p.calls.Load())
	changedProxy := *a.Proxy
	changedProxy.Port = 1081
	a.Proxy = &changedProxy
	require.Equal(t, DefaultOpenAIRequestTimezone, r.Resolve(context.Background(), a), "same proxy ID with changed route must not reuse the old timezone")
	require.Eventually(t, func() bool { return r.Resolve(context.Background(), a) == "Asia/Tokyo" }, time.Second, time.Millisecond)
	require.Equal(t, int32(1), p.calls.Load())
}
func TestOpenAIRequestTimezoneGlobalSettingsRoundtrip(t *testing.T) {
	s := timezoneSettingsFixture(t, false, "America/New_York")
	require.Equal(t, OpenAIRequestTimezoneSettings{AutoDetectProxy: false, DefaultTimezone: "America/New_York"}, s.GetOpenAIRequestTimezoneSettings(context.Background()))
	parsed := s.parseSettings(map[string]string{SettingKeyOpenAIRequestTimezoneSettings: `{"auto_detect_proxy":true,"default_timezone":"Asia/Tokyo"}`})
	require.NotNil(t, parsed.OpenAIRequestTimezoneSettings)
	require.Equal(t, "Asia/Tokyo", parsed.OpenAIRequestTimezoneSettings.DefaultTimezone)
	updates, err := s.buildSystemSettingsUpdates(context.Background(), parsed)
	require.NoError(t, err)
	require.Contains(t, updates, SettingKeyOpenAIRequestTimezoneSettings)
	require.Error(t, ValidateOpenAIRequestTimezoneSettings(&OpenAIRequestTimezoneSettings{DefaultTimezone: "Mars/Crater"}))
	parsed.OpenAIRequestTimezoneSettings = &OpenAIRequestTimezoneSettings{AutoDetectProxy: false, DefaultTimezone: "Europe/London"}
	s.refreshCachedSettings(parsed)
	require.Equal(t, "Europe/London", s.GetOpenAIRequestTimezoneSettings(context.Background()).DefaultTimezone)
}
func TestOpenAIRequestTimezoneFormalRewriteUsesResolvedTimezone(t *testing.T) {
	a := timezoneAccountFixture()
	a.ProxyID = nil
	a.Proxy = nil
	original := `{"input":[{"role":"user","content":"<environment_context><current_date>2001-01-01</current_date><timezone>Asia/Shanghai</timezone><cwd>/fixture</cwd></environment_context>"}]}`
	s := &OpenAIGatewayService{settingService: timezoneSettingsFixture(t, false, "America/New_York")}
	body := s.normalizeOpenAIRequestLocale(context.Background(), a, []byte(original), "http")
	text := gjson.GetBytes(body, "input.0.content").String()
	require.Contains(t, text, "<timezone>America/New_York</timezone>")
	require.Contains(t, text, "2001-01-01")
	require.Contains(t, text, "/fixture")
	require.Nil(t, a.Extra, "resolved locale must never modify persisted account settings")
}
