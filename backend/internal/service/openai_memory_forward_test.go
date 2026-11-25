package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/MACOS-DO/sub4api/internal/config"
	"github.com/MACOS-DO/sub4api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
)

type memoryHashUpstream struct{ digest [32]byte }

func (u *memoryHashUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	h := sha256.New()
	_, err := io.Copy(h, req.Body)
	req.Body.Close()
	copy(u.digest[:], h.Sum(nil))
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"resp_test","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1}}`))}, nil
}
func (u *memoryHashUpstream) DoWithTLS(r *http.Request, p string, id int64, n int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(r, p, id, n)
}

func TestMemoryLargeHistoryForwardUnchanged(t *testing.T) {
	if !runMemoryTestIsolated(t) {
		return
	}
	for _, tc := range []struct {
		name string
		body []byte
	}{
		{"text_8MiB", memoryHistoryBody()},
		{"image_58MiB", bytes.Replace(largeNativeResponsesBody(58<<20), []byte(`"stream":true`), []byte(`"stream":false`), 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := tc.body
			upstream := &memoryHashUpstream{}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "synthetic-test-key", "base_url": "https://example.com"}, Extra: map[string]any{"use_responses_api": true}}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			_, err := svc.Forward(context.Background(), c, account, body)
			runtime.ReadMemStats(&after)
			if err != nil {
				t.Fatal(err)
			}
			if upstream.digest != sha256.Sum256(body) {
				t.Fatal("normal forwarded request changed")
			}
			allocated := after.TotalAlloc - before.TotalAlloc
			t.Logf("body=%d bytes, full forwarding allocations=%d bytes", len(body), allocated)
			if allocated > 2<<20 {
				t.Fatalf("no-op full forwarding still copies large history: %d", allocated)
			}

		})
	}
}
