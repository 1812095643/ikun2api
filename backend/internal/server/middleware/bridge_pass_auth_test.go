package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/config"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestBridgePassUsesAuthenticatedAPIKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	authorizations := []string{"", "Basic cHJveHk6dGVzdA=="}
	interop := os.Getenv("IKUN_BRIDGE_INTEROP_ADDRESS_FILE")
	if interop != "" {
		authorizations = authorizations[1:]
	}
	for _, authorization := range authorizations {
		t.Run(authorization, func(t *testing.T) {
			cfg := &config.Config{RunMode: config.RunModeSimple}
			cfg.JWT.Secret = "bridge-real-auth-site-secret"
			group := &service.Group{ID: 17, Platform: service.PlatformAnthropic, Status: service.StatusActive, Hydrated: true, BridgeRequired: true}
			key := &service.APIKey{
				ID: 23, UserID: 31, Key: "bridge-real-auth-key", Status: service.StatusActive,
				GroupID: &group.ID, Group: group,
				User: &service.User{ID: 31, Status: service.StatusActive, Role: service.RoleUser, Balance: 10},
			}
			keys := service.NewAPIKeyService(bridgePassAuthRepository{key: key}, nil, nil, nil, nil, nil, cfg)
			auth := gin.HandlerFunc(NewAPIKeyAuthMiddleware(keys, nil, cfg))
			router := gin.New()
			router.POST("/v1/ikun/bridge/session", auth, IssueBridgePass(service.NewBridgePassSigner(cfg.JWT.Secret)))
			var accepted atomic.Int32
			var validateClient atomic.Bool
			for _, path := range []string{"/v1/models", "/v1/messages"} {
				router.Any(path, APIKeySmartRouting(router, keys, cfg), auth, BridgeGroupAccess(), func(c *gin.Context) {
					require.Empty(t, c.GetHeader(service.BridgePassHeader))
					if validateClient.Load() && c.Request.Method == http.MethodPost {
						var body map[string]any
						require.NoError(t, json.NewDecoder(c.Request.Body).Decode(&body))
						require.NotContains(t, body, "metadata", "客户端身份元数据必须已经在 Bridge 移除")
						valid := service.NewClaudeCodeValidator().Validate(c.Request, body)
						ctx := service.SetClaudeDesktopClient(service.SetClaudeCodeClient(c.Request.Context(), valid), service.IsClaudeDesktopUserAgent(c.Request.UserAgent()))
						if !(&service.Group{Platform: service.PlatformAnthropic, ClaudeCodeOnly: true, AllowClaudeDesktop: true}).AllowsClaudeClient(ctx) {
							c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "claude_client_required"})
							return
						}
					}
					accepted.Add(1)
					c.JSON(http.StatusOK, gin.H{"data": []string{}})
				})
			}
			request := func(method, path, pass string) *httptest.ResponseRecorder {
				req := httptest.NewRequest(method, path, strings.NewReader(`{"model":"claude-test"}`))
				req.Header.Set("X-Api-Key", key.Key)
				if authorization != "" {
					req.Header.Set("Authorization", authorization)
				}
				if pass != "" {
					req.Header.Set(service.BridgePassHeader, pass)
				}
				response := httptest.NewRecorder()
				router.ServeHTTP(response, req)
				return response
			}
			issued := request(http.MethodPost, "/v1/ikun/bridge/session", "")
			require.Equal(t, http.StatusOK, issued.Code)
			var session struct {
				Ticket string `json:"ticket"`
			}
			require.NoError(t, json.Unmarshal(issued.Body.Bytes(), &session))
			invalidKey := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			invalidKey.Header.Set("X-Api-Key", key.Key)
			invalidKey.Header.Set("Authorization", "Bearer wrong-key")
			invalidKey.Header.Set(service.BridgePassHeader, session.Ticket)
			invalidResponse := httptest.NewRecorder()
			router.ServeHTTP(invalidResponse, invalidKey)
			require.Equal(t, http.StatusUnauthorized, invalidResponse.Code)
			for _, endpoint := range []struct{ method, path string }{{http.MethodGet, "/v1/models"}, {http.MethodPost, "/v1/messages"}} {
				denied := request(endpoint.method, endpoint.path, "")
				require.Equal(t, http.StatusForbidden, denied.Code)
				require.Contains(t, denied.Body.String(), service.BridgeRequiredCode)
				require.Equal(t, http.StatusForbidden, request(endpoint.method, endpoint.path, strings.Repeat("A", 66)).Code)
				allowed := request(endpoint.method, endpoint.path, session.Ticket)
				require.Equal(t, http.StatusOK, allowed.Code, allowed.Body.String())
			}
			require.EqualValues(t, 2, accepted.Load())
			if interop != "" {
				validateClient.Store(true)
				done := make(chan struct{}, 1)
				router.POST("/_fixture/done", func(c *gin.Context) {
					done <- struct{}{}
					c.Status(http.StatusNoContent)
				})
				// 显式联调模式只监听本机：使用真实鉴权、缓存快照和 HMAC，接收 Rust Bridge 的实际请求。
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					req.Header.Set("Authorization", authorization)
					router.ServeHTTP(w, req)
				}))
				defer server.Close()
				require.NoError(t, os.WriteFile(interop, []byte(server.URL), 0600))
				select {
				case <-done:
					require.EqualValues(t, 4, accepted.Load())
				case <-time.After(60 * time.Second):
					t.Fatal("Rust Bridge 联调未在限定时间内完成")
				}
			}
		})
	}
}

type bridgePassAuthRepository struct {
	service.APIKeyRepository
	key *service.APIKey
}

func (r bridgePassAuthRepository) GetByKeyForAuth(_ context.Context, credential string) (*service.APIKey, error) {
	if credential != r.key.Key {
		return nil, service.ErrAPIKeyNotFound
	}
	key := *r.key
	key.Key = ""
	return &key, nil
}

func (bridgePassAuthRepository) UpdateLastUsed(context.Context, int64, time.Time) error {
	return nil
}
