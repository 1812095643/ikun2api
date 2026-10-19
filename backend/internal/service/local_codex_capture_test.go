package service

import (
	"encoding/json"
	"net/http"
	"os"
	"testing"
)

func TestLocalCodexIdentityMatchesCapture(t *testing.T) {
	raw, err := os.ReadFile("testdata/local-codex-0.163.0-headers.json")
	if err != nil {
		t.Fatal(err)
	}
	var captured struct {
		Headers map[string]string `json:"headers"`
	}
	if err := json.Unmarshal(raw, &captured); err != nil {
		t.Fatal(err)
	}
	codexCanonicalUAMu.RLock()
	previousResolver := codexCanonicalUAResolver
	codexCanonicalUAMu.RUnlock()
	previousEnforcement := codexIdentityEnforcement.Load()
	SetCodexCanonicalUserAgentResolver(func() string { return captured.Headers["User-Agent"] })
	SetCodexIdentityEnforcementEnabled(true)
	t.Cleanup(func() {
		SetCodexCanonicalUserAgentResolver(previousResolver)
		SetCodexIdentityEnforcementEnabled(previousEnforcement)
	})
	headers := make(http.Header)
	ensureCodexIdentityHeaders(headers)
	enforceCodexIdentityHeaders(headers)
	if headers.Get("User-Agent") != captured.Headers["User-Agent"] || headers.Get("Originator") != captured.Headers["originator"] || headers.Get("Version") != "0.163.0" {
		t.Fatalf("canonical identity ignored the observed client: %+v", headers)
	}
}
