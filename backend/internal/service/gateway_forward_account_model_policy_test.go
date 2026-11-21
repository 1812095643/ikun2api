//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestClaudeForwardChecksAccountModelPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const alias = "claude-approved-alias"
	const permitted = "claude-sonnet-4-6"
	const forbidden = "claude-opus-4-6"
	for _, endpoint := range []string{"messages", "chat/completions", "responses"} {
		for _, requested := range []string{alias, forbidden} {
			t.Run(endpoint+"/"+requested, func(t *testing.T) {
				account := &Account{ID: 58, Platform: PlatformAnthropic, Type: AccountTypeAPIKey,
					Credentials: map[string]any{"api_key": "fixture", "base_url": "https://synthetic.invalid",
						"model_mapping": map[string]any{alias: permitted, forbidden: forbidden}},
					Extra: map[string]any{AccountModelAllowlistExtraKey: GroupModelAllowlist{Enabled: true, Models: []string{alias}}},
				}
				transport := &httpUpstreamRecorder{err: errors.New("local transport fixture")}
				svc := &GatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
				text := `{"model":"MODEL","messages":[{"role":"user","content":"test"}],"max_tokens":8,"stream":true}`
				if endpoint == "responses" {
					text = `{"model":"MODEL","input":"test","max_output_tokens":8,"stream":true}`
				}
				body := []byte(strings.ReplaceAll(text, "MODEL", requested))
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				ctx := context.Background()
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+endpoint, strings.NewReader(string(body)))
				var err error
				switch endpoint {
				case "messages":
					parsed, parseErr := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
					require.NoError(t, parseErr)
					_, err = svc.Forward(ctx, c, account, parsed)
				case "chat/completions":
					_, err = svc.ForwardAsChatCompletions(ctx, c, account, body, nil)
				case "responses":
					_, err = svc.ForwardAsResponses(ctx, c, account, body, nil)
				}
				require.Error(t, err)
				if requested == forbidden {
					require.Equal(t, http.StatusForbidden, rec.Code)
					require.Nil(t, transport.lastReq)
				} else {
					require.NotNil(t, transport.lastReq)
					require.Equal(t, permitted, gjson.GetBytes(transport.lastBody, "model").String())
				}
			})
		}
	}
}
