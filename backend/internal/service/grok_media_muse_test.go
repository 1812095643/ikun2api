//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type museVideoBillingCache struct {
	GatewayCache
	pending map[string][]byte
	loadErr error
	loads   int
}

func (m *museVideoBillingCache) SetGrokVideoPendingBilling(_ context.Context, key string, body []byte, _ time.Duration) error {
	if m.pending == nil {
		m.pending = make(map[string][]byte)
	}
	m.pending[key] = append([]byte(nil), body...)
	return nil
}

func (m *museVideoBillingCache) GetGrokVideoPendingBilling(_ context.Context, key string) ([]byte, error) {
	m.loads++
	return m.pending[key], m.loadErr
}

func museVideoTestAccount() *Account {
	account := grokMediaContentTestAccount()
	account.Name = "muse"
	account.Credentials["base_url"] = "https://ai.windking.shop/v1"
	return account
}

func museVideoTestPending() GrokVideoPendingBilling {
	return GrokVideoPendingBilling{
		Model: "muse-video", BillingModel: "muse-video", UpstreamModel: "muse-video",
		OriginalModel: "muse-video", VideoResolution: "720p", VideoDurationSeconds: 10,
	}
}

func TestForwardMuseVideoCompletedUsesCreateSnapshot(t *testing.T) {
	for _, endpoint := range []GrokMediaEndpoint{GrokMediaEndpointVideoStatus, GrokMediaEndpointVideoContent} {
		t.Run(string(endpoint), func(t *testing.T) {
			statusBody := `{"status":"completed","video":{"url":"http://87.83.206.209:18610/v1/media/video.mp4"}}`
			upstream := &grokMediaContentUpstreamStub{responses: []*http.Response{
				grokMediaContentStatusResponse(statusBody),
				{StatusCode: 200, Header: http.Header{"Content-Type": []string{"video/mp4"}}, Body: io.NopCloser(strings.NewReader("video-payload"))},
			}}
			cache := &museVideoBillingCache{}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream, cache: cache}
			c, recorder := grokMediaContentTestContext(http.MethodGet, "https://gateway.example/v1/videos/task-1", nil)
			c.Set("api_key", &APIKey{ID: 20, UserID: 10})
			require.NoError(t, svc.StoreGrokVideoPendingBilling(context.Background(), "task-1", 10, 20, museVideoTestPending()))

			result, err := svc.ForwardGrokMedia(context.Background(), c, museVideoTestAccount(), endpoint, "task-1", nil, "")

			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, 1, result.VideoCount)
			require.Equal(t, "muse-video", result.Model)
			require.Equal(t, "muse-video", result.BillingModel)
			require.Equal(t, "muse-video", result.UpstreamModel)
			require.Equal(t, "720p", result.VideoResolution)
			require.Equal(t, 10, result.VideoDurationSeconds)
			require.Equal(t, "task-1", result.ResponseID)
			if endpoint == GrokMediaEndpointVideoStatus {
				require.Equal(t, "completed", gjson.Get(recorder.Body.String(), "status").String())
				require.Len(t, upstream.requests, 1)
			} else {
				require.Equal(t, "video-payload", recorder.Body.String())
				require.Len(t, upstream.requests, 2)
				require.Equal(t, "https://ai.windking.shop/v1/videos/task-1/content", upstream.requests[1].URL.String())
			}
			for _, request := range upstream.requests {
				require.Equal(t, http.MethodGet, request.Method)
				require.Equal(t, "Bearer upstream-key", request.Header.Get("Authorization"))
			}
		})
	}
}

func TestMuseVideoCompletedProviderScope(t *testing.T) {
	for _, tc := range []struct {
		name, baseURL, platform, accountType string
		allowed                              bool
	}{
		{"Muse", "https://ai.windking.shop", PlatformGrok, AccountTypeAPIKey, true},
		{"Muse v1", "https://ai.windking.shop/v1", PlatformGrok, AccountTypeAPIKey, true},
		{"Muse default port", "https://ai.windking.shop:443/v1", PlatformGrok, AccountTypeAPIKey, true},
		{"official API", "https://api.x.ai/v1", PlatformGrok, AccountTypeAPIKey, false},
		{"official CLI", "https://cli-chat-proxy.grok.com", PlatformGrok, AccountTypeAPIKey, false},
		{"other supplier named muse", "https://other.example/v1", PlatformGrok, AccountTypeAPIKey, false},
		{"deceptive subdomain", "https://ai.windking.shop.attacker.invalid/v1", PlatformGrok, AccountTypeAPIKey, false},
		{"deceptive userinfo", "https://ai.windking.shop@attacker.invalid/v1", PlatformGrok, AccountTypeAPIKey, false},
		{"unexpected port", "https://ai.windking.shop:18610/v1", PlatformGrok, AccountTypeAPIKey, false},
		{"insecure origin", "http://ai.windking.shop/v1", PlatformGrok, AccountTypeAPIKey, false},
		{"OAuth", "https://ai.windking.shop/v1", PlatformGrok, AccountTypeOAuth, false},
		{"other platform", "https://ai.windking.shop/v1", PlatformOpenAI, AccountTypeAPIKey, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := &museVideoBillingCache{}
			svc := &OpenAIGatewayService{cache: cache}
			account := museVideoTestAccount()
			account.Credentials["base_url"] = tc.baseURL
			account.Platform, account.Type = tc.platform, tc.accountType
			c, _ := grokMediaContentTestContext(http.MethodGet, "https://gateway.example/v1/videos/task-1", nil)
			c.Set("api_key", &APIKey{ID: 20, UserID: 10})
			require.NoError(t, svc.StoreGrokVideoPendingBilling(t.Context(), "task-1", 10, 20, museVideoTestPending()))
			result := svc.extractMuseVideoCompletedBilling(t.Context(), c, account,
				[]byte(`{"status":"completed","model":"muse-video","video":{"url":"https://media.example/video.mp4","duration":8}}`), "task-1")
			if tc.allowed {
				require.NotNil(t, result)
				require.Equal(t, 8, result.VideoDurationSeconds)
				require.Equal(t, 1, cache.loads)
			} else {
				require.Nil(t, result)
				require.Zero(t, cache.loads, "other suppliers must not enter the compatibility path")
			}
		})
	}
}

func TestMuseVideoCompletedRequiresCreateTimeModel(t *testing.T) {
	for _, tc := range []struct {
		name    string
		pending GrokVideoPendingBilling
		allowed bool
	}{
		{"request model", GrokVideoPendingBilling{Model: "muse-video"}, true},
		{"billing model", GrokVideoPendingBilling{BillingModel: "muse-video"}, true},
		{"account mapping", GrokVideoPendingBilling{Model: "grok-imagine-video", BillingModel: "grok-imagine-video", UpstreamModel: "muse-video"}, true},
		{"client alias", GrokVideoPendingBilling{OriginalModel: "muse-video", Model: "video-alias"}, true},
		{"normalized case", GrokVideoPendingBilling{Model: " MUSE-VIDEO "}, true},
		{"other video model", GrokVideoPendingBilling{Model: "grok-imagine-video"}, false},
		{"different Muse model", GrokVideoPendingBilling{Model: "muse-image"}, false},
		{"near matching model", GrokVideoPendingBilling{Model: "muse-video-other"}, false},
		{"missing create model", GrokVideoPendingBilling{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &OpenAIGatewayService{cache: &museVideoBillingCache{}}
			c, _ := grokMediaContentTestContext(http.MethodGet, "https://gateway.example/v1/videos/task-1", nil)
			c.Set("api_key", &APIKey{ID: 20, UserID: 10})
			require.NoError(t, svc.StoreGrokVideoPendingBilling(t.Context(), "task-1", 10, 20, tc.pending))
			// A response claiming muse-video must not opt a different task into billing.
			body := []byte(`{"status":"completed","model":"muse-video","video":{"url":"https://media.example/video.mp4","duration":8}}`)
			original := append([]byte(nil), body...)
			result := svc.extractMuseVideoCompletedBilling(t.Context(), c, museVideoTestAccount(), body, "task-1")
			require.Equal(t, original, body, "billing must not rewrite the client's completed status")
			require.Equal(t, tc.allowed, result != nil)
		})
	}
}

func TestMuseVideoCompletedRejectsIncompleteOrUnrelatedStates(t *testing.T) {
	for _, body := range []string{
		`{"status":"pending","video":{"url":"https://media.example/video.mp4"}}`,
		`{"status":"failed","video":{"url":"https://media.example/video.mp4"}}`,
		`{"status":"expired","video":{"url":"https://media.example/video.mp4"}}`,
		`{"status":"completed"}`,
		`{"status":"completed","video":{"url":" "}}`,
		`{"status":"completed","video":{"url":123}}`,
		`{"status":"completed","url":"https://media.example/video.mp4"}`,
		`{"id":"other-task","status":"completed","video":{"url":"https://media.example/video.mp4"}}`,
		`{"status":"completed","video":`,
	} {
		t.Run(body, func(t *testing.T) {
			cache := &museVideoBillingCache{}
			svc := &OpenAIGatewayService{cache: cache}
			c, _ := grokMediaContentTestContext(http.MethodGet, "https://gateway.example/v1/videos/task-1", nil)
			c.Set("api_key", &APIKey{ID: 20, UserID: 10})
			require.Nil(t, svc.extractMuseVideoCompletedBilling(t.Context(), c, museVideoTestAccount(), []byte(body), "task-1"))
			require.Zero(t, cache.loads)
		})
	}
}

func TestMuseVideoCompletedSnapshotFailureAndTenantIsolation(t *testing.T) {
	for _, scenario := range []string{"missing snapshot", "cache error", "corrupt snapshot", "other user", "other key", "other task", "missing auth", "nil auth"} {
		t.Run(scenario, func(t *testing.T) {
			cache := &museVideoBillingCache{}
			svc := &OpenAIGatewayService{cache: cache}
			c, _ := grokMediaContentTestContext(http.MethodGet, "https://gateway.example/v1/videos/task-1", nil)
			key := &APIKey{ID: 20, UserID: 10}
			requestID := "task-1"
			c.Set("api_key", key)
			require.NoError(t, svc.StoreGrokVideoPendingBilling(t.Context(), "task-1", 10, 20, museVideoTestPending()))
			switch scenario {
			case "missing snapshot":
				cache.pending = nil
			case "cache error":
				cache.loadErr = errors.New("cache unavailable")
			case "corrupt snapshot":
				cache.pending[grokVideoPendingBillingKey("task-1", 10, 20)] = []byte(`{"model":`)
			case "other user":
				key.UserID++
			case "other key":
				key.ID++
			case "other task":
				requestID = "other-task"
			case "missing auth":
				c.Set("api_key", "invalid-auth")
			case "nil auth":
				c.Set("api_key", (*APIKey)(nil))
			}
			body := []byte(`{"status":"completed","model":"muse-video","video":{"url":"https://media.example/video.mp4","duration":8}}`)
			require.Nil(t, svc.extractMuseVideoCompletedBilling(t.Context(), c, museVideoTestAccount(), body, requestID))
		})
	}
}

func TestForwardMuseVideoCompletedDoesNotBillUnsuccessfulContent(t *testing.T) {
	upstream := &grokMediaContentUpstreamStub{responses: []*http.Response{
		grokMediaContentStatusResponse(`{"status":"completed","video":{"url":"http://87.83.206.209:18610/v1/media/video.mp4"}}`),
		{StatusCode: http.StatusRequestedRangeNotSatisfiable, Header: http.Header{"Content-Range": []string{"bytes */100"}}, Body: io.NopCloser(strings.NewReader("bad range"))},
	}}
	cache := &museVideoBillingCache{}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream, cache: cache}
	c, recorder := grokMediaContentTestContext(http.MethodGet, "https://gateway.example/v1/videos/task-1/content", nil)
	c.Set("api_key", &APIKey{ID: 20, UserID: 10})
	require.NoError(t, svc.StoreGrokVideoPendingBilling(t.Context(), "task-1", 10, 20, museVideoTestPending()))
	result, err := svc.ForwardGrokMedia(t.Context(), c, museVideoTestAccount(), GrokMediaEndpointVideoContent, "task-1", nil, "")
	require.NoError(t, err)
	require.Equal(t, http.StatusRequestedRangeNotSatisfiable, recorder.Code)
	require.Zero(t, result.VideoCount)
	require.Zero(t, cache.loads)
}
