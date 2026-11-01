package workspace

import (
	"net/netip"
	"testing"
)

func TestImageRelayRejectsPrivateTargets(t *testing.T) {
	for _, raw := range []string{"http://127.0.0.1/x", "http://10.0.0.1/x", "http://[::1]/x", "http://[::ffff:127.0.0.1]/x", "http://169.254.169.254/x", "http://100.64.0.1/x", "file:///tmp/x", "https://user:pass@example.com/x"} {
		if _, err := imageURL(raw); err == nil {
			t.Errorf("accepted unsafe target %q", raw)
		}
	}
	if _, err := imageURL("https://images.example.com/result.png"); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"192.168.1.1", "172.16.0.1", "fe80::1", "fc00::1", "224.0.0.1", "198.18.0.1"} {
		if publicImageIP(netip.MustParseAddr(raw)) {
			t.Errorf("accepted unsafe address %s", raw)
		}
	}
}
