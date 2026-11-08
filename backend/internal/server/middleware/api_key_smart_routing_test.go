package middleware

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/pkg/httputil"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type smartRouteTestKeys struct {
	gets   int
	key    *service.APIKey
	groups map[int64]*service.Group
}

func (s *smartRouteTestKeys) GetByKey(context.Context, string) (*service.APIKey, error) {
	s.gets++
	return s.key, nil
}
func (s *smartRouteTestKeys) BindAPIKeyRequestGroup(_ context.Context, k *service.APIKey, id int64) (*service.APIKey, error) {
	group := s.groups[id]
	if group == nil {
		return nil, service.ErrGroupNotFound
	}
	copy := *k
	copy.Group = group
	copy.GroupID = &id
	return &copy, nil
}
func smartRouteTestServer(t *testing.T, handler gin.HandlerFunc, matcher SmartRouteMatcher, ingress ...gin.HandlerFunc) (*gin.Engine, *smartRouteTestKeys) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	groups := map[int64]*service.Group{1: {ID: 1, Name: "first", Status: service.StatusActive, RateMultiplier: 2}, 2: {ID: 2, Name: "second", Status: service.StatusActive, RateMultiplier: 3}}
	first := int64(1)
	keys := &smartRouteTestKeys{key: &service.APIKey{ID: 99, UserID: 7, User: &service.User{ID: 7, Status: service.StatusActive}, Name: "routing", Key: "local-test-key", GroupID: &first, Group: groups[1], RouteGroupIDs: []int64{1, 2}, Status: service.StatusActive, FundingSource: "universal_subscription"}, groups: groups}
	engine := gin.New()
	engine.Use(ingress...)
	engine.Use(APIKeySmartRouting(engine, keys))
	engine.Use(func(c *gin.Context) {
		loaded, lookupErr := lookupAPIKeyForRequest(c, keys, smartRouteCredential(c))
		require.NoError(t, lookupErr)
		key, err := bindSmartRouteGroup(c, keys, loaded)
		require.NoError(t, err)
		if _, _, ok := validateAPIKeyGroupAvailable(key); !ok {
			MarkIngressRejected(c, IngressRejectGroupDisabled)
			c.AbortWithStatus(403)
			return
		}
		c.Set(string(ContextKeyAPIKey), key)
		c.Next()
	})
	engine.Use(APIKeySmartRouteGuard(matcher))
	engine.Any("/v1/messages", handler)
	engine.GET("/v1/models", handler)
	return engine, keys
}
func smartRouteRequest(engine *gin.Engine, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(`{"model":"claude-test","messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("Authorization", "Bearer local-test-key")
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, req)
	return response
}
func TestSmartRoutingOrderedFallbackReplaysBodyAndBindsBillingIdentity(t *testing.T) {
	var groups []int64
	var bodies []string
	var chargedGroup int64
	var chargedRate float64
	engine, keys := smartRouteTestServer(t, func(c *gin.Context) {
		key, _ := GetAPIKeyFromContext(c)
		groups = append(groups, *key.GroupID)
		body, _ := io.ReadAll(c.Request.Body)
		bodies = append(bodies, string(body))
		if *key.GroupID == 1 {
			c.Set("ops_routing_capacity_limited", true)
			c.JSON(503, gin.H{"error": "No available accounts"})
			return
		}
		chargedGroup = *key.GroupID
		chargedRate = key.Group.RateMultiplier
		require.Equal(t, "universal_subscription", key.FundingSource)
		c.JSON(200, gin.H{"ok": true})
	}, nil)
	response := smartRouteRequest(engine, "POST", "/v1/messages")
	require.Equal(t, 200, response.Code)
	require.Equal(t, []int64{1, 2}, groups)
	require.Len(t, bodies, 2)
	require.Equal(t, bodies[0], bodies[1])
	require.Equal(t, int64(2), chargedGroup)
	require.Equal(t, 3.0, chargedRate)
	require.Equal(t, int64(1), *keys.key.GroupID, "auth-cache key must not mutate")
	require.NotContains(t, response.Body.String(), "No available")
	require.Equal(t, 1, keys.gets, "one authenticated lookup per inbound request")
}
func TestSmartRoutingSkipsProtocolMismatchAndRevokedGroup(t *testing.T) {
	for _, revoked := range []bool{false, true} {
		t.Run(fmt.Sprint(revoked), func(t *testing.T) {
			attempts := []int64{}
			engine, keys := smartRouteTestServer(t, func(c *gin.Context) {
				key, _ := GetAPIKeyFromContext(c)
				attempts = append(attempts, *key.GroupID)
				c.JSON(200, gin.H{"ok": true})
			}, func(_ *gin.Context, key *service.APIKey) (bool, error) { return *key.GroupID == 2, nil })
			if revoked {
				keys.groups[1].Status = "disabled"
			}
			require.Equal(t, 200, smartRouteRequest(engine, "POST", "/v1/messages").Code)
			require.Equal(t, []int64{2}, attempts)
		})
	}
}
func TestSmartRoutingNeverRetriesUpstreamErrors(t *testing.T) {
	for _, status := range []int{400, 401, 403, 429, 500, 502, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			engine, _ := smartRouteTestServer(t, func(c *gin.Context) {
				calls++
				c.Set("ops_account_id", int64(10))
				c.JSON(status, gin.H{"error": "original upstream response"})
			}, nil)
			response := smartRouteRequest(engine, "POST", "/v1/messages")
			require.Equal(t, status, response.Code)
			require.Equal(t, 1, calls)
			require.Contains(t, response.Body.String(), "original upstream response")
		})
	}
}
func TestSmartRoutingStreamsWithoutBufferingOrSecondDispatch(t *testing.T) {
	calls := 0
	outer := httptest.NewRecorder()
	engine, _ := smartRouteTestServer(t, func(c *gin.Context) {
		calls++
		c.Header("Content-Type", "text/event-stream")
		c.Status(200)
		_, err := c.Writer.WriteString("data: first\n\n")
		require.NoError(t, err)
		c.Writer.Flush()
		require.Contains(t, outer.Body.String(), "data: first", "first event must already be visible")
		_, _ = c.Writer.WriteString("data: last\n\n")
	}, nil)
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"claude-test"}`))
	req.Header.Set("Authorization", "Bearer local-test-key")
	engine.ServeHTTP(outer, req)
	require.Equal(t, 1, calls)
	require.Equal(t, "text/event-stream", outer.Header().Get("Content-Type"))
	require.Equal(t, "data: first\n\ndata: last\n\n", outer.Body.String())
}
func TestSmartRoutingModelDiscoveryUnionsAuthorizedCatalogs(t *testing.T) {
	engine, _ := smartRouteTestServer(t, func(c *gin.Context) {
		key, _ := GetAPIKeyFromContext(c)
		c.JSON(200, gin.H{"object": "list", "data": []gin.H{{"id": "shared"}, {"id": fmt.Sprintf("group-%d", *key.GroupID)}}})
	}, nil)
	response := smartRouteRequest(engine, "GET", "/v1/models")
	require.Equal(t, 200, response.Code)
	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	require.Len(t, result.Data, 3)
	require.Equal(t, []string{"shared", "group-1", "group-2"}, []string{result.Data[0].ID, result.Data[1].ID, result.Data[2].ID})
}
func TestSmartRoutingBoundsBufferedErrorAndDoesNotReplayCommittedOutput(t *testing.T) {
	calls := 0
	body := strings.Repeat("x", (2<<20)+1)
	engine, _ := smartRouteTestServer(t, func(c *gin.Context) {
		calls++
		c.Set("ops_routing_capacity_limited", true)
		c.Data(503, "text/plain", []byte(body))
	}, nil)
	response := smartRouteRequest(engine, "POST", "/v1/messages")
	require.Equal(t, 1, calls)
	require.Equal(t, body, response.Body.String())
}
func TestSmartRoutingDoesNotBypassNonGroupAuthenticationFailure(t *testing.T) {
	calls := 0
	engine, _ := smartRouteTestServer(t, func(c *gin.Context) {
		calls++
		MarkIngressRejected(c, IngressRejectAPIKeyDisabled)
		c.AbortWithStatus(401)
	}, nil)
	require.Equal(t, 401, smartRouteRequest(engine, "POST", "/v1/messages").Code)
	require.Equal(t, 1, calls)
}

func TestSmartRoutingCompressedRequestIsDecodedSeparatelyForEachGroup(t *testing.T) {
	payload := []byte(`{"model":"claude-test","messages":[{"role":"user","content":"hello"}]}`)
	var compressed bytes.Buffer
	zipper := gzip.NewWriter(&compressed)
	_, err := zipper.Write(payload)
	require.NoError(t, err)
	require.NoError(t, zipper.Close())
	var bodies []string
	engine, _ := smartRouteTestServer(t, func(c *gin.Context) {
		key, _ := GetAPIKeyFromContext(c)
		body, err := httputil.ReadRequestBodyWithPrealloc(c.Request)
		require.NoError(t, err)
		bodies = append(bodies, string(body))
		if *key.GroupID == 1 {
			if len(body) > 0 {
				body[0] = 'X'
			}
			c.Set("ops_routing_capacity_limited", true)
			c.JSON(503, gin.H{"error": "No available accounts"})
			return
		}
		c.JSON(200, gin.H{"ok": true})
	}, nil)
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(compressed.Bytes()))
	req.Header.Set("Authorization", "Bearer local-test-key")
	req.Header.Set("Content-Encoding", "gzip")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, req)
	require.Equal(t, 200, response.Code)
	require.Equal(t, []string{string(payload), string(payload)}, bodies)
}

func TestSmartRoutingReleasesEachGroupBodyBudget(t *testing.T) {
	budget := httputil.NewBodyMemoryBudget(400 << 10)
	var attempts []int64
	engine, _ := smartRouteTestServer(t, func(c *gin.Context) {
		key, _ := GetAPIKeyFromContext(c)
		attempts = append(attempts, *key.GroupID)
		if err := httputil.ReserveRequestBodyMemory(c.Request.Context(), 256<<10); err != nil {
			c.AbortWithStatus(503)
			return
		}
		if *key.GroupID == 1 {
			c.Set("ops_routing_capacity_limited", true)
			c.AbortWithStatus(503)
			return
		}
		c.JSON(200, gin.H{"ok": true})
	}, nil, RequestBodyLimit(1<<20, budget))
	response := smartRouteRequest(engine, "POST", "/v1/messages")
	require.Equal(t, 200, response.Code, response.Body.String())
	require.Equal(t, []int64{1, 2}, attempts)
	ctx, release := httputil.WithRequestBodyMemoryBudget(context.Background(), budget)
	defer release()
	require.NoError(t, httputil.ReserveRequestBodyMemory(ctx, 400<<10), "all reservations end with the request")
}

func TestSmartRoutingBodyCapacityFailureIsRetryableOverload(t *testing.T) {
	budget := httputil.NewBodyMemoryBudget(1)
	calls := 0
	engine, _ := smartRouteTestServer(t, func(c *gin.Context) { calls++; c.Status(200) }, nil, RequestBodyLimit(1<<20, budget))
	response := smartRouteRequest(engine, "POST", "/v1/messages")
	require.Equal(t, 503, response.Code)
	require.Equal(t, "1", response.Header().Get("Retry-After"))
	require.Zero(t, calls)
}
