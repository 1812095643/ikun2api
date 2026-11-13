package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"

	infraerrors "github.com/MACOS-DO/sub4api/internal/pkg/errors"
)

const SettingKeyOpenAIRequestTimezoneSettings = "openai_request_timezone_settings"

type OpenAIRequestTimezoneSettings struct {
	AutoDetectProxy bool   `json:"auto_detect_proxy"`
	DefaultTimezone string `json:"default_timezone"`
}

func DefaultOpenAIRequestTimezoneSettings() *OpenAIRequestTimezoneSettings {
	return &OpenAIRequestTimezoneSettings{AutoDetectProxy: true, DefaultTimezone: DefaultOpenAIRequestTimezone}
}
func ValidateOpenAIRequestTimezoneSettings(settings *OpenAIRequestTimezoneSettings) error {
	if settings != nil && !isAllowedOpenAIRequestTimezone(settings.DefaultTimezone) {
		return infraerrors.BadRequest("INVALID_OPENAI_DEFAULT_TIMEZONE", "default_timezone must be a supported IANA timezone")
	}
	return nil
}

type cachedOpenAIRequestTimezoneSettings struct {
	settings OpenAIRequestTimezoneSettings
	expires  time.Time
}

func (s *SettingService) GetOpenAIRequestTimezoneSettings(ctx context.Context) OpenAIRequestTimezoneSettings {
	if ctx == nil {
		ctx = context.Background()
	}
	fallback := *DefaultOpenAIRequestTimezoneSettings()
	if s == nil || s.settingRepo == nil {
		return fallback
	}
	now := time.Now()
	cached := s.openAIRequestTimezoneCache.Load()
	if cached != nil && now.Before(cached.expires) {
		return cached.settings
	}
	value, _, _ := s.openAIRequestTimezoneSF.Do(SettingKeyOpenAIRequestTimezoneSettings, func() (any, error) {
		before := s.openAIRequestTimezoneCache.Load()
		if before != nil && time.Now().Before(before.expires) {
			return before.settings, nil
		}
		dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		raw, err := s.settingRepo.GetValue(dbCtx, SettingKeyOpenAIRequestTimezoneSettings)
		result := fallback
		ttl := time.Minute
		if err == nil && raw != "" {
			parsed := fallback
			if json.Unmarshal([]byte(raw), &parsed) == nil && ValidateOpenAIRequestTimezoneSettings(&parsed) == nil {
				result = parsed
			}
		} else if err != nil {
			ttl = 5 * time.Second
			if before != nil {
				result = before.settings
			}
		}
		entry := &cachedOpenAIRequestTimezoneSettings{settings: result, expires: time.Now().Add(ttl)}
		if !s.openAIRequestTimezoneCache.CompareAndSwap(before, entry) {
			if latest := s.openAIRequestTimezoneCache.Load(); latest != nil {
				return latest.settings, nil
			}
		}
		return result, nil
	})
	if result, ok := value.(OpenAIRequestTimezoneSettings); ok {
		return result
	}
	return fallback
}

type proxyTimezoneEntry struct {
	confirmed time.Time
	timezone  string
	nextProbe time.Time
	touched   time.Time
	inFlight  bool
}

// Only hashed route identity is retained. Probes use the exact account proxy;
// an inaccessible proxy never triggers direct-IP geolocation.
type OpenAIRequestTimezoneResolver struct {
	settings *SettingService
	cache    ProxyLatencyCache
	prober   ProxyExitInfoProber
	mu       sync.Mutex
	entries  map[string]*proxyTimezoneEntry
	slots    chan struct{}
}

func NewOpenAIRequestTimezoneResolver(settings *SettingService, cache ProxyLatencyCache, prober ProxyExitInfoProber) *OpenAIRequestTimezoneResolver {
	return &OpenAIRequestTimezoneResolver{settings: settings, cache: cache, prober: prober, entries: make(map[string]*proxyTimezoneEntry), slots: make(chan struct{}, 2)}
}
func proxyRouteFingerprint(proxy *Proxy) string {
	if proxy == nil {
		return ""
	}
	sum := sha256.Sum256([]byte(proxy.URL()))
	return hex.EncodeToString(sum[:])
}
func (r *OpenAIRequestTimezoneResolver) Resolve(ctx context.Context, account *Account) string {
	if account != nil && account.IsOpenAI() {
		if name := account.getExtraString(openAIRequestTimezoneExtraKey); isAllowedOpenAIRequestTimezone(name) {
			return name
		}
	}
	settings := *DefaultOpenAIRequestTimezoneSettings()
	if r != nil {
		settings = r.settings.GetOpenAIRequestTimezoneSettings(ctx)
	}
	if r == nil || account == nil || !account.IsOpenAI() || !settings.AutoDetectProxy || account.Proxy == nil || account.ProxyID == nil || account.Proxy.ID != *account.ProxyID {
		return settings.DefaultTimezone
	}
	proxy := *account.Proxy
	fingerprint := proxyRouteFingerprint(&proxy)
	key := fingerprint
	now := time.Now()
	r.mu.Lock()
	entry := r.entries[key]
	if entry == nil {
		if len(r.entries) >= 512 {
			var oldestKey string
			oldest := now
			for k, v := range r.entries {
				if !v.inFlight && !v.touched.After(oldest) {
					oldestKey = k
					oldest = v.touched
				}
			}
			if oldestKey != "" {
				delete(r.entries, oldestKey)
			}
		}
		entry = &proxyTimezoneEntry{}
		r.entries[key] = entry
	}
	entry.touched = now
	timezone := entry.timezone
	if !entry.confirmed.IsZero() && now.Sub(entry.confirmed) > 30*time.Minute {
		timezone = ""
	}
	if !entry.inFlight && !now.Before(entry.nextProbe) {
		select {
		case r.slots <- struct{}{}:
			entry.inFlight = true
			go r.refresh(proxy, key, entry)
		default:
		}
	}
	r.mu.Unlock()
	if isAllowedOpenAIRequestTimezone(timezone) {
		return timezone
	}
	return settings.DefaultTimezone
}
func (r *OpenAIRequestTimezoneResolver) refresh(proxy Proxy, key string, entry *proxyTimezoneEntry) {
	defer func() { <-r.slots }()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	timezone := ""
	if r.cache != nil {
		if cached, err := r.cache.GetProxyLatencies(ctx, []int64{proxy.ID}); err == nil {
			if info := cached[proxy.ID]; info != nil && info.ProxyRouteFingerprint == key && time.Since(info.UpdatedAt) < 30*time.Minute && isAllowedOpenAIRequestTimezone(info.Timezone) {
				timezone = info.Timezone
			}
		}
	}
	if timezone == "" && r.prober != nil {
		if exit, _, err := r.prober.ProbeProxy(ctx, proxy.URL()); err == nil && exit != nil && isAllowedOpenAIRequestTimezone(exit.Timezone) {
			timezone = exit.Timezone
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	entry.inFlight = false
	if r.entries[key] != entry {
		return
	}
	entry.nextProbe = time.Now().Add(time.Minute)
	if timezone != "" {
		entry.timezone = timezone
		entry.confirmed = time.Now()
		entry.nextProbe = time.Now().Add(30 * time.Minute)
	}
}
func (s *OpenAIGatewayService) resolveOpenAIRequestTimezone(ctx context.Context, account *Account) string {
	if s != nil && s.requestTimezoneResolver != nil {
		return s.requestTimezoneResolver.Resolve(ctx, account)
	}
	if account != nil && account.IsOpenAI() {
		if name := account.getExtraString(openAIRequestTimezoneExtraKey); isAllowedOpenAIRequestTimezone(name) {
			return name
		}
	}
	if s != nil {
		return s.settingService.GetOpenAIRequestTimezoneSettings(ctx).DefaultTimezone
	}
	return DefaultOpenAIRequestTimezone
}
func (s *OpenAIGatewayService) normalizeOpenAIRequestLocale(ctx context.Context, account *Account, body []byte, transport string) []byte {
	if account == nil || !account.IsOpenAI() {
		return body
	}
	clone := *account
	clone.Extra = make(map[string]any, len(account.Extra)+1)
	for k, v := range account.Extra {
		clone.Extra[k] = v
	}
	clone.Extra[openAIRequestTimezoneExtraKey] = s.resolveOpenAIRequestTimezone(ctx, account)
	return normalizeOpenAIRequestLocale(ctx, &clone, body, transport)
}
