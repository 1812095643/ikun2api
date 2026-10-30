package service

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type stubIdentityCache struct {
	fingerprint *Fingerprint
	setCalls    int
	lastSet     *Fingerprint
}

func (s *stubIdentityCache) GetFingerprint(_ context.Context, _ int64) (*Fingerprint, error) {
	if s.fingerprint == nil {
		return nil, nil
	}
	clone := *s.fingerprint
	return &clone, nil
}

func (s *stubIdentityCache) SetFingerprint(_ context.Context, _ int64, fp *Fingerprint) error {
	s.setCalls++
	clone := *fp
	s.lastSet = &clone
	s.fingerprint = &clone
	return nil
}

func (s *stubIdentityCache) GetSessionMaskSeed(_ context.Context, _ int64) (string, error) {
	return "", nil
}

func (s *stubIdentityCache) GetOrCreateSessionMaskSeed(_ context.Context, _ int64, candidate string) (string, error) {
	return candidate, nil
}

func (s *stubIdentityCache) GetClaudeCodeHeaders(_ context.Context, _ int64) (http.Header, error) {
	return nil, nil
}

func (s *stubIdentityCache) UpdateClaudeCodeHeaders(_ context.Context, _ int64, _ http.Header) error {
	return nil
}

func headersWithUA(ua string) http.Header {
	h := http.Header{}
	if ua != "" {
		h.Set("User-Agent", ua)
	}
	return h
}

func TestClaudeIdentityRequestHeadersCannotMutateProfile(t *testing.T) {
	for _, ua := range []string{"", "curl/8.10.1", "claude-cli/2.1.220 (external, cli)", "claude-cli/2.999.999 (external, cli)", "claude-cli/999.0.0-local", "claude-cli/2.1.299 (external, cli)"} {
		t.Run(ua, func(t *testing.T) {
			cache := &stubIdentityCache{}
			svc := newTestIdentityService(cache)
			headers := headersWithUA(ua)
			for _, key := range []string{"Lang", "OS", "Arch", "Runtime", "Runtime-Version", "Package-Version"} {
				headers.Set("X-Stainless-"+key, "untrusted")
			}
			fp, err := svc.GetOrCreateFingerprint(context.Background(), 41, headers)
			require.NoError(t, err)
			want := defaultFingerprint()
			want.ClientID = fp.ClientID
			want.IdentityVersion = 1
			want.UpdatedAt = fp.UpdatedAt
			require.Equal(t, want, *fp)
			require.True(t, ValidClaudeDeviceID(fp.ClientID))
			headers.Set("User-Agent", "claude-cli/2.999.999 (external, cli)")
			repeated, err := svc.GetOrCreateFingerprint(context.Background(), 41, headers)
			require.NoError(t, err)
			require.Equal(t, fp, repeated)
			require.Equal(t, 1, cache.setCalls)
		})
	}
}

func TestClaudeIdentityMigratesWholeProfilePreservingDevice(t *testing.T) {
	for _, ua := range []string{"curl/8.10.1", "claude-cli/2.1.220 (external, cli)", "claude-cli/2.999.999 (external, cli)"} {
		t.Run(ua, func(t *testing.T) {
			id := strings.Repeat("a", 64)
			cache := &stubIdentityCache{fingerprint: &Fingerprint{ClientID: id, UserAgent: ua, StainlessOS: "old-platform", StainlessRuntimeVersion: "old-runtime", UpdatedAt: time.Now().Unix()}}
			svc := newTestIdentityService(cache)
			fp, err := svc.GetOrCreateFingerprint(context.Background(), 41, headersWithUA("claude-cli/2.999.999"))
			require.NoError(t, err)
			want := defaultFingerprint()
			want.ClientID = id
			want.IdentityVersion = 1
			want.UpdatedAt = fp.UpdatedAt
			require.Equal(t, want, *fp)
			require.Equal(t, fp, cache.lastSet)
		})
	}
}
