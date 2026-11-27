package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/config"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestBridgePassRouteAdmission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	cfg.JWT.Secret = "isolated-bridge-site-key"
	signer := service.NewBridgePassSigner(cfg.JWT.Secret)
	paid := 0
	var chosen int64
	protected := &service.Group{ID: 1, BridgeRequired: true, Platform: service.PlatformAnthropic, Status: service.StatusActive}
	open := &service.Group{ID: 2, Platform: service.PlatformAnthropic, Status: service.StatusActive}
	key := &service.APIKey{ID: 7, GroupID: &protected.ID, Group: protected, User: &service.User{ID: 5, Status: service.StatusActive}, Key: "test-key", Status: service.StatusActive}
	keys := &smartRouteTestKeys{key: key, groups: map[int64]*service.Group{1: protected, 2: open}}
	engine := gin.New()
	auth := func(c *gin.Context) {
		if smartRouteCredential(c) != key.Key {
			c.AbortWithStatus(401)
			return
		}
		loaded, err := lookupAPIKeyForRequest(c, keys, key.Key)
		require.NoError(t, err)
		selected, err := bindSmartRouteGroup(c, keys, loaded)
		require.NoError(t, err)
		c.Set(string(ContextKeyAPIKey), selected)
		c.Next()
	}
	engine.POST("/v1/ikun/bridge/session", auth, IssueBridgePass(signer))
	check := func(c *gin.Context) {
		require.Empty(t, c.Request.Header.Values(service.BridgePassHeader))
		selected, _ := GetAPIKeyFromContext(c)
		chosen = selected.Group.ID
		paid++
		c.JSON(200, gin.H{"data": []struct{}{}})
	}
	for _, path := range []string{"/v1/messages", "/v1/messages/count_tokens", "/messages/count_tokens", "/v1/models"} {
		engine.Any(path, APIKeySmartRouting(engine, keys, cfg), auth, BridgeGroupAccess(), check)
	}
	request := func(method, path, credential string, passes ...string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(`{"model":"test"}`))
		r.Header.Set("X-Api-Key", credential)
		for _, pass := range passes {
			r.Header.Add(service.BridgePassHeader, pass)
		}
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, r)
		return w
	}
	require.Equal(t, 401, request("POST", "/v1/ikun/bridge/session", "wrong-key").Code)
	issued := request("POST", "/v1/ikun/bridge/session", key.Key)
	require.Equal(t, 200, issued.Code)
	require.Equal(t, "no-store", issued.Header().Get("Cache-Control"))
	var session struct {
		Ticket    string `json:"ticket"`
		ExpiresIn int    `json:"expires_in"`
	}
	require.NoError(t, json.Unmarshal(issued.Body.Bytes(), &session))
	require.Equal(t, 300, session.ExpiresIn)
	for _, path := range []string{"/v1/messages", "/v1/messages/count_tokens", "/messages/count_tokens", "/v1/models"} {
		r := request(http.MethodGet, path, key.Key)
		require.Equal(t, 403, r.Code, path)
		require.Contains(t, r.Body.String(), service.BridgeRequiredCode)
	}
	expired, _ := signer.Issue(key.ID, key.Key, time.Now().Add(-service.BridgePassTTL))
	wrong, _ := signer.Issue(key.ID+1, key.Key, time.Now())
	for _, pass := range []string{expired, wrong, "garbage"} {
		r := request("POST", "/v1/messages", key.Key, pass)
		require.Equal(t, 403, r.Code)
		if pass == expired {
			require.Contains(t, r.Body.String(), service.BridgeExpiredCode)
		}
	}
	require.Equal(t, 403, request("POST", "/v1/messages", key.Key, session.Ticket, session.Ticket).Code)
	require.Zero(t, paid, "no denied or mint request may reach model handling")
	require.Equal(t, 200, request("POST", "/v1/messages", key.Key, session.Ticket).Code)
	require.EqualValues(t, 1, chosen)

	key.RouteGroupIDs = []int64{1, 2}
	require.Equal(t, 200, request("POST", "/v1/messages", key.Key).Code)
	require.EqualValues(t, 2, chosen, "smart routing may use the unprotected candidate")
	open.BridgeRequired = true
	require.Equal(t, 403, request("POST", "/v1/messages", key.Key).Code)
	require.Equal(t, 2, paid)
	protected.BridgeRequired = false
	require.Equal(t, 200, request("POST", "/v1/messages", key.Key).Code)
	require.EqualValues(t, 1, chosen, "turning the switch off restores direct use")
	protected.BridgeRequired = true
	require.Equal(t, 200, request("POST", "/v1/messages", key.Key, session.Ticket).Code)
	require.EqualValues(t, 1, chosen, "有效票据必须保留到智能路由选中的受保护分组")
}
