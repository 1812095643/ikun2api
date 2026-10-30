package workspace

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/service"
)

func balanceFixture(raw string) map[string]any {
	var p map[string]any
	d := json.NewDecoder(strings.NewReader(raw))
	d.UseNumber()
	if d.Decode(&p) != nil {
		panic(raw)
	}
	return p
}
func TestUpstreamBalanceNormalizeAndDiscover(t *testing.T) {
	cases := map[string]string{" https://EXAMPLE.com:443/v1/ ": "https://example.com", "https://example.com/tenant/v1": "https://example.com/tenant", "https://example.com/v1beta/openai": "https://example.com", "https://[2606:4700:4700::1111]:443/v1": "https://[2606:4700:4700::1111]"}
	for in, want := range cases {
		got, err := normalizeUpstreamBalanceURL(in)
		if err != nil || got != want {
			t.Fatalf("%q: %q %v", in, got, err)
		}
	}
	for _, s := range []string{"javascript:alert(1)", "https://user:pw@example.com", "https://example.com/?token=secret", "https://example.com/#frag", "https://example.com:0", "https://example.com/%00", "https://example.com?"} {
		if _, e := normalizeUpstreamBalanceURL(s); e == nil {
			t.Fatalf("accepted %s", s)
		}
	}
	accounts := []service.Account{{ID: 1, Name: "one", Platform: "openai", Type: "apikey", Credentials: map[string]any{"api_key": "secret-A", "base_url": "https://EXAMPLE.com/v1"}}, {ID: 2, Platform: "claude", Type: "apikey", Credentials: map[string]any{"api_key": "secret-B", "base_url": "https://example.com/"}}, {ID: 3, Platform: "gemini", Type: "oauth", Credentials: map[string]any{"api_key": "oauth-secret", "base_url": "https://oauth.example"}}, {ID: 4, Platform: "kimi", Type: "apikey", Credentials: map[string]any{"api_key": "secret-A", "base_url": "https://example.com", "api_base_urls": map[string]any{"responses": "https://example.com/v1", "anthropic": "https://second.example/v1"}}}}
	groups := upstreamBalanceGroups(accounts)
	if len(groups) != 2 || len(groups[0].Accounts) != 3 || len(groups[1].Accounts) != 1 {
		t.Fatalf("wrong groups: %+v", groups)
	}
	raw, _ := json.Marshal(groups[0].Accounts)
	if strings.Contains(string(raw), "secret-") {
		t.Fatal("key leaked to JSON")
	}
	first := groups[0].SourceHash
	accounts[0].Credentials["api_key"] = "rotated"
	if upstreamBalanceGroups(accounts)[0].SourceHash == first {
		t.Fatal("rotation was not detected")
	}
}
func TestUpstreamBalancePrivateNetworkGuard(t *testing.T) {
	for _, s := range []string{"127.0.0.1", "::1", "::ffff:127.0.0.1", "10.0.0.1", "192.168.1.1", "169.254.169.254", "100.100.100.200", "fc00::1", "fe80::1", "224.0.0.1", "0.0.0.0", "240.0.0.1"} {
		if balancePublicIP(netip.MustParseAddr(s), false) {
			t.Fatalf("accepted %s", s)
		}
	}
	for _, s := range []string{"8.8.8.8", "108.186.222.151", "2606:4700:4700::1111"} {
		if !balancePublicIP(netip.MustParseAddr(s), false) {
			t.Fatalf("rejected public %s", s)
		}
	}
	if !balancePublicIP(netip.MustParseAddr("192.168.1.1"), true) {
		t.Fatal("explicit private policy ignored")
	}
	h := &Handler{}
	if _, _, e := h.upstreamBalanceClient(context.Background(), "https://127.0.0.1", nil); e == nil {
		t.Fatal("loopback connection accepted")
	}
}
func TestUpstreamBalanceParseScopesAndUnits(t *testing.T) {
	for _, tc := range []struct {
		raw, scope string
		want       float64
		unlimited  bool
	}{
		{`{"remaining":42.5,"quota_limited":false}`, "wallet", 42.5, false},
		{`{"mode":"unrestricted","balance":-1,"remaining":-1}`, "wallet", -1, false},
		{`{"mode":"quota_limited","remaining":"2.50"}`, "key", 2.5, false},
		{`{"mode":"unrestricted","subscription":{},"remaining":-1}`, "subscription", 0, true},
		{`{"data":{"remaining_balance":"99.90"}}`, "wallet", 99.9, false},
	} {
		r, e := parseSubUpstreamBalance(balanceFixture(tc.raw))
		if e != nil || r.Scope != tc.scope || r.Unlimited != tc.unlimited || (!tc.unlimited && (r.Balance == nil || *r.Balance != tc.want)) {
			t.Fatalf("%s: %+v %v", tc.raw, r, e)
		}
	}
	for _, raw := range []string{`{"remaining":"NaN"}`, `{"remaining":"1e309"}`, `{"error":{"message":"secret"}}`, `{"code":401,"remaining":100}`, `{"isValid":false,"remaining":5}`} {
		if _, e := parseSubUpstreamBalance(balanceFixture(raw)); e == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	for _, tc := range []struct {
		status, unit string
		want         float64
	}{
		{`{"quota_per_unit":500000,"quota_display_type":"USD"}`, "USD", 2},
		{`{"data":{"quota_per_unit":500000,"quota_display_type":"CNY","usd_exchange_rate":7}}`, "CNY", 14},
		{`{"quota_per_unit":500000,"quota_display_type":"TOKENS"}`, "Tokens", 1000000},
		{`{"quota_per_unit":500000,"quota_display_type":"CUSTOM","custom_currency_exchange_rate":3,"custom_currency_symbol":"X"}`, "X", 6},
	} {
		r, e := parseNewAPIWalletBalance(balanceFixture(`{"success":true,"data":{"id":12,"quota":1000000}}`), balanceFixture(tc.status))
		if e != nil || r.Balance == nil || *r.Balance != tc.want || r.Unit != tc.unit || r.WalletUserID != 12 {
			t.Fatalf("%+v %v", r, e)
		}
	}
	status := balanceFixture(`{"quota_per_unit":500000}`)
	r, e := parseNewAPITokenBalance(balanceFixture(`{"code":true,"data":{"unlimited_quota":true,"total_available":100000000,"user_quota":{"total_available":2500000}}}`), status)
	if e != nil || r.Scope != "wallet" || r.Unlimited || r.Balance == nil || *r.Balance != 5 {
		t.Fatalf("key cap confused with wallet: %+v %v", r, e)
	}
	r, e = parseNewAPITokenBalance(balanceFixture(`{"code":true,"data":{"unlimited_quota":true,"total_available":100000000}}`), status)
	if e != nil || r.Scope != "key" || !r.Unlimited || r.Balance != nil {
		t.Fatal("unlimited fake balance accepted")
	}
	if _, e = parseNewAPIWalletBalance(balanceFixture(`{"data":{"quota":1}}`), balanceFixture(`{"quota_per_unit":0}`)); e == nil {
		t.Fatal("invalid conversion accepted")
	}
}
func TestUpstreamBalanceHTTPReadOnlyAndAuthCompatibility(t *testing.T) {
	calls := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Error("non-read-only request")
		}
		calls = append(calls, r.URL.Path)
		switch r.URL.Path {
		case "/api/status":
			if r.Header.Get("Authorization") != "" {
				t.Error("token sent to public status")
			}
			w.Write([]byte(`{"data":{"quota_per_unit":500000}}`))
		case "/api/user/self":
			if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
				w.WriteHeader(401)
				return
			}
			if r.Header.Get("Authorization") != "private-canary" || r.Header.Get("New-Api-User") != "12" {
				t.Error("legacy credentials mismatch")
			}
			w.Write([]byte(`{"success":true,"data":{"id":12,"quota":1500000}}`))
		case "/api/usage/token/":
			w.WriteHeader(401)
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	c := defaultUpstreamBalanceConfig()
	c.Kind = "newapi"
	c.NewAPIUserID = 12
	r, e := queryUpstreamBalance(context.Background(), server.Client(), server.URL, "model-key", "private-canary", c)
	if e != nil || r.Balance == nil || *r.Balance != 3 || len(calls) != 3 {
		t.Fatalf("%+v %v calls=%v", r, e, calls)
	}
	for _, p := range calls {
		if p != "/api/status" && p != "/api/user/self" {
			t.Fatalf("unneeded endpoint %s", p)
		}
	}
}
func TestUpstreamBalanceErrorRedactionAndResponseLimit(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
	}{
		{401, `{"error":"private-canary"}`}, {200, `{"success":false,"message":"private-canary"}`}, {200, strings.Repeat("x", (1<<20)+2)},
	} {
		t.Run(string(rune(tc.status))+strconvForTest(len(tc.body)), func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); w.Write([]byte(tc.body)) }))
			defer s.Close()
			c := defaultUpstreamBalanceConfig()
			c.Kind = "sub2api"
			_, err := queryUpstreamBalance(context.Background(), s.Client(), s.URL, "private-canary", "", c)
			if err == nil || strings.Contains(err.Error(), "private-canary") {
				t.Fatalf("unredacted error %v", err)
			}
		})
	}
}
func strconvForTest(n int) string { return time.Duration(n).String() }
func TestUpstreamBalanceComparisonNeverSumsOrHidesFailures(t *testing.T) {
	f := 5.0
	g := 6.0
	a := upstreamBalanceReading{AccountIDs: []int64{1}, Balance: &f, Unit: "USD", Scope: "wallet"}
	b := a
	b.AccountIDs = []int64{2}
	s := summarizeUpstreamBalances([]upstreamBalanceReading{a, b}, 0)
	if s.Comparison != "consistent" || s.Balance == nil || *s.Balance != 5 {
		t.Fatal("wallet summed")
	}
	b.Balance = &g
	s = summarizeUpstreamBalances([]upstreamBalanceReading{a, b}, 0)
	if s.Comparison != "different" || s.Balance != nil {
		t.Fatal("conflicting wallets silently selected")
	}
	s = summarizeUpstreamBalances([]upstreamBalanceReading{a, b}, 2)
	if s.Balance == nil || *s.Balance != 6 {
		t.Fatal("preferred source ignored")
	}
	b.Error = "unavailable"
	s = summarizeUpstreamBalances([]upstreamBalanceReading{a, b}, 0)
	if s.Comparison != "partial" || s.Balance != nil {
		t.Fatal("failed key hidden")
	}
	b.Error = ""
	b.Balance = &f
	b.Scope = "key"
	if summarizeUpstreamBalances([]upstreamBalanceReading{a, b}, 0).Balance != nil {
		t.Fatal("scope mismatch accepted")
	}
	b.Scope = "wallet"
	b.WalletUserID = 2
	a.WalletUserID = 1
	if sameUpstreamBalance(a, b) {
		t.Fatal("different wallet users accepted")
	}
}
func TestUpstreamBalanceMonitorValidationAndAlarmDedup(t *testing.T) {
	for _, mutate := range []func(*upstreamBalanceConfig){func(c *upstreamBalanceConfig) { c.IntervalMinutes = 0 }, func(c *upstreamBalanceConfig) { c.Threshold = math.NaN() }, func(c *upstreamBalanceConfig) { c.Recipients = []string{"a@x.com\r\nBcc: b@y.com"} }, func(c *upstreamBalanceConfig) { c.AuthMode = "legacy" }, func(c *upstreamBalanceConfig) { c.Kind = "unknown" }} {
		c := defaultUpstreamBalanceConfig()
		mutate(&c)
		if normalizeUpstreamBalanceConfig(&c) == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
	now := time.Now()
	amount := 2.0
	r := upstreamBalanceRecord{Config: defaultUpstreamBalanceConfig()}
	r.Config.MonitorEnabled = true
	r.Config.Recipients = []string{"admin@example.com"}
	s := upstreamBalanceSnapshot{Balance: &amount, Unit: "USD"}
	alert, pending := upstreamBalanceAlertPlan(r, s, now)
	if !alert.Low || len(pending) != 1 {
		t.Fatal("low alert missed")
	}
	alert.LastAttempt = &now
	alert.RecipientsSent = pending
	r.storedAlert = alert
	if _, p := upstreamBalanceAlertPlan(r, s, now.Add(2*time.Hour)); len(p) != 0 {
		t.Fatal("low alert repeated")
	}
	recovered := 20.0
	s.Balance = &recovered
	alert, _ = upstreamBalanceAlertPlan(r, s, now.Add(2*time.Hour))
	if alert.Low {
		t.Fatal("recovery missed")
	}
	r.storedAlert = alert
	s.Balance = &amount
	if _, p := upstreamBalanceAlertPlan(r, s, now.Add(3*time.Hour)); len(p) != 1 {
		t.Fatal("second low event missed")
	}
	r.leaseUntil = &now
	r.Queued = true
	if !upstreamBalanceDue(r, now.Add(time.Second)) || upstreamBalanceDue(r, now.Add(-time.Second)) {
		t.Fatal("lease does not protect worker")
	}
}

func TestUpstreamBalanceRedirectCannotExfiltrateKey(t *testing.T) {
	received := false
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { received = true; w.Write([]byte(`{"remaining":1}`)) }))
	defer evil.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, evil.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	_, err := balanceGetJSON(context.Background(), origin.Client(), origin.URL, "v1/usage", "Bearer canary-secret", 0)
	if err == nil || received {
		t.Fatal("redirect forwarded a credential to another endpoint")
	}
}
