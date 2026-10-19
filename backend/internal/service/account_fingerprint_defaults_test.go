package service

import "testing"

func TestLocalFingerprintAccountDefaults(t *testing.T) {
	tests := []struct {
		name        string
		platform    string
		accountType string
		extra       map[string]any
		wantTLS     bool
	}{
		{"claude_oauth", PlatformAnthropic, AccountTypeOAuth, nil, true},
		{"claude_setup_token", PlatformAnthropic, AccountTypeSetupToken, nil, true},
		{"claude_api_key", PlatformAnthropic, AccountTypeAPIKey, nil, false},
		{"openai_oauth", PlatformOpenAI, AccountTypeOAuth, nil, false},
		{"explicit_opt_out", PlatformAnthropic, AccountTypeOAuth, map[string]any{"enable_tls_fingerprint": false}, false},
		{"explicit_opt_in", PlatformAnthropic, AccountTypeOAuth, map[string]any{"enable_tls_fingerprint": true}, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			account := &Account{
				Platform: test.platform,
				Type:     test.accountType,
				Extra:    prepareAccountFingerprintExtraForCreate(test.platform, test.accountType, test.extra),
			}
			if got := account.IsTLSFingerprintEnabled(); got != test.wantTLS {
				t.Fatalf("TLS scope or explicit opt-out changed: got %v, want %v", got, test.wantTLS)
			}
		})
	}
	input := map[string]any{"user_tag": "preserve"}
	prepared := prepareAccountFingerprintExtraForCreate(PlatformAnthropic, AccountTypeOAuth, input)
	if _, changed := input["enable_tls_fingerprint"]; changed || prepared["user_tag"] != "preserve" {
		t.Fatal("account creation must preserve the caller's extra map")
	}
}
