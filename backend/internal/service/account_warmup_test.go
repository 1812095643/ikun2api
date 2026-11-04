package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type warmupUsageProbe struct {
	calls int
	raw   string
	err   error
}

func (p *warmupUsageProbe) FetchUsage(context.Context, string, string) (*ClaudeUsageResponse, error) {
	panic("unscoped fetch used")
}
func (p *warmupUsageProbe) FetchUsageWithOptions(context.Context, *ClaudeUsageFetchOptions) (*ClaudeUsageResponse, error) {
	p.calls++
	if p.err != nil {
		return nil, p.err
	}
	var v ClaudeUsageResponse
	err := json.Unmarshal([]byte(p.raw), &v)
	return &v, err
}

type warmupUsageRepo struct{ AccountRepository }

func (r *warmupUsageRepo) UpdateExtra(context.Context, int64, map[string]any) error { return nil }
func TestAccountWarmupQuotaStrictThreshold(t *testing.T) {
	for _, tc := range []struct {
		q    WarmupQuota
		skip bool
	}{
		{WarmupQuota{FiveHour: 99, SevenDay: 99}, false},
		{WarmupQuota{FiveHour: 99.001, SevenDay: 0}, true},
		{WarmupQuota{FiveHour: 0, SevenDay: 100}, true},
		{WarmupQuota{FiveHour: -1, SevenDay: 0}, true},
	} {
		if (tc.q.SkipReason() != "") != tc.skip {
			t.Fatal("threshold mismatch")
		}
	}
}
func TestAccountWarmupQuotaAlwaysFetchesWithoutPassiveOrCache(t *testing.T) {
	p := &warmupUsageProbe{raw: `{"five_hour":{"utilization":99.5,"resets_at":null},"seven_day":{"utilization":1}}`}
	s := &AccountUsageService{usageFetcher: p, cache: NewUsageCache(), accountRepo: &warmupUsageRepo{}}
	a := &Account{ID: 123, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "TEST_ONLY"}, Extra: map[string]any{"session_window_utilization": 0, "passive_usage_7d_utilization": 0}}
	s.cache.apiCache.Store(a.ID, &apiUsageCache{response: &ClaudeUsageResponse{}, timestamp: time.Now()})
	for i := 0; i < 2; i++ {
		q, err := s.RefreshClaudeForWarmup(context.Background(), a)
		if err != nil || q.Source != "active" || q.FiveHour != 99.5 || q.SkipReason() == "" {
			t.Fatal("cached or passive quota used")
		}
	}
	if p.calls != 2 {
		t.Fatal("active calls missing")
	}
}
func TestAccountWarmupMissingQuotaFailsClosed(t *testing.T) {
	for _, raw := range []string{`{}`, `{"five_hour":{"utilization":0}}`, `{"five_hour":null,"seven_day":{"utilization":0}}`, `{"five_hour":{"utilization":0},"seven_day":{}}`} {
		p := &warmupUsageProbe{raw: raw}
		s := &AccountUsageService{usageFetcher: p}
		a := &Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "TEST_ONLY"}}
		if _, err := s.RefreshClaudeForWarmup(context.Background(), a); err == nil {
			t.Fatal("incomplete quota treated as zero")
		}
	}
	p := &warmupUsageProbe{err: errors.New("TEST_SENSITIVE_DIAGNOSTIC")}
	s := &AccountUsageService{usageFetcher: p}
	_, err := s.RefreshClaudeForWarmup(context.Background(), &Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "TEST_ONLY"}})
	if err == nil || err.Error() == "TEST_SENSITIVE_DIAGNOSTIC" {
		t.Fatal("unsafe diagnostic")
	}
}

func TestAccountWarmupOptionalWeekQuotaWithoutReset(t *testing.T) {
	p := &warmupUsageProbe{raw: "{\"five_hour\":{\"utilization\":0},\"seven_day\":{\"utilization\":1},\"seven_day_sonnet\":{\"utilization\":99.1}}"}
	svc := &AccountUsageService{usageFetcher: p, accountRepo: &warmupUsageRepo{}}
	q, err := svc.RefreshClaudeForWarmup(context.Background(), &Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "TEST_ONLY"}})
	if err != nil || q.SkipReason() == "" {
		t.Fatal("model-specific weekly quota bypassed")
	}
}
