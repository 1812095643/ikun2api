package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/config"
	"github.com/MACOS-DO/sub4api/internal/pkg/httputil"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func newCodexSyncTestService(t *testing.T, conns ...openAIWSClientConn) (*OpenAIGatewayService, *Account, *openAIWSQueueDialer) {
	t.Helper()
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.IngressModeDefault = "ctx_pool"
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
	cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
	cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3
	dialer := &openAIWSQueueDialer{conns: conns}
	pool := newOpenAIWSConnPool(cfg)
	pool.setClientDialerForTest(dialer)
	t.Cleanup(pool.Close)
	svc := &OpenAIGatewayService{cfg: cfg, cache: &stubGatewayCache{}, httpUpstream: &httpUpstreamRecorder{}, openaiWSPool: pool, openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), toolCorrector: NewCodexToolCorrector()}
	account := &Account{ID: 812, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{"access_token": "local-test-token"}, Extra: map[string]any{"responses_websockets_v2_enabled": true}}
	return svc, account, dialer
}

func openCodexSyncIngress(t *testing.T, svc *OpenAIGatewayService, account *Account, hooks *OpenAIWSIngressHooks) (*coderws.Conn, <-chan error) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	finished := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			finished <- err
			return
		}
		defer conn.CloseNow()
		_, first, err := conn.Read(r.Context())
		if err != nil {
			finished <- err
			return
		}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = r
		c.Request.Header.Set("session-id", "019ca836-59bd-73bc-bd01-26c5fcb19542")
		c.Request.Header.Set("x-codex-window-id", "019ca836-59bd-73bc-bd01-26c5fcb19542:0")
		finished <- svc.ProxyResponsesWebSocketFromClient(r.Context(), c, conn, account, "local-test-token", first, hooks)
	}))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { client.CloseNow() })
	return client, finished
}

func codexSyncWrite(t *testing.T, conn *coderws.Conn, body string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, conn.Write(ctx, coderws.MessageText, []byte(body)))
}

func codexSyncRead(t *testing.T, conn *coderws.Conn) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, body, err := conn.Read(ctx)
	require.NoError(t, err)
	return body
}

func finishCodexSyncIngress(t *testing.T, client *coderws.Conn, finished <-chan error) {
	t.Helper()
	require.NoError(t, client.Close(coderws.StatusNormalClosure, "done"))
	select {
	case err := <-finished:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("本地 WS 测试未结束")
	}
}

func TestOpenAICodexSyncIngressRepairsKnownToolsAndRotatesExpiredLease(t *testing.T) {
	first := &openAIWSCaptureConn{events: [][]byte{
		[]byte(`{"type":"response.output_item.done","item":{"type":"function_call","id":"fc_item_a","call_id":"call_a","name":"read_data","arguments":"{}"}}`),
		[]byte(`{"type":"response.completed","response":{"id":"resp_first","status":"completed","usage":{"input_tokens":1,"output_tokens":1}}}`),
	}}
	second := &openAIWSCaptureConn{events: [][]byte{[]byte(`{"type":"response.completed","response":{"id":"resp_second","status":"completed","usage":{"input_tokens":1,"output_tokens":1}}}`)}}
	svc, account, dialer := newCodexSyncTestService(t, first, second)
	expired := make(chan struct{}, 1)
	hooks := &OpenAIWSIngressHooks{AfterTurn: func(turn int, _ *OpenAIForwardResult, err error) {
		if turn != 1 || err != nil {
			return
		}
		ap := svc.openaiWSPool.getOrCreateAccountPool(account.ID)
		ap.mu.Lock()
		for _, conn := range ap.conns {
			conn.createdAtNano.Store(time.Now().Add(-55 * time.Minute).UnixNano())
		}
		ap.mu.Unlock()
		expired <- struct{}{}
	}}
	client, done := openCodexSyncIngress(t, svc, account, hooks)
	codexSyncWrite(t, client, `{"type":"response.create","model":"gpt-5.4","client_metadata":{"x-codex-window-id":"019ca836-59bd-73bc-bd01-26c5fcb19542:0"},"input":[{"role":"user","content":"first"}]}`)
	require.Equal(t, "response.output_item.done", gjson.GetBytes(codexSyncRead(t, client), "type").String())
	require.Equal(t, "resp_first", gjson.GetBytes(codexSyncRead(t, client), "response.id").String())
	select {
	case <-expired:
	case <-time.After(5 * time.Second):
		t.Fatal("第一轮未完成")
	}
	codexSyncWrite(t, client, `{"type":"response.create","model":"gpt-5.4","client_metadata":{"x-codex-window-id":"019ca836-59bd-73bc-bd01-26c5fcb19542:0"},"previous_response_id":"resp_first","input":[{"role":"user","content":"continue"}]}`)
	require.Equal(t, "resp_second", gjson.GetBytes(codexSyncRead(t, client), "response.id").String())
	finishCodexSyncIngress(t, client, done)
	require.Equal(t, 2, dialer.DialCount())
	require.Len(t, second.writes, 1)
	require.Empty(t, second.writes[0]["previous_response_id"])
	input := second.writes[0]["input"].([]any)
	require.Len(t, input, 4)
	require.Equal(t, "aborted", input[2].(map[string]any)["output"])
	require.Equal(t, "call_a", input[2].(map[string]any)["call_id"])
}

func TestOpenAICodexSyncIngressConnectionLimitFailureRecovery(t *testing.T) {
	for _, shape := range []string{"error", "response.failed"} {
		for _, afterOutput := range []bool{false, true} {
			t.Run(shape+"/"+testBoolName(afterOutput), func(t *testing.T) {
				failure := `{"type":"error","error":{"code":"websocket_connection_limit_reached","message":"connection has reached maximum duration"}}`
				if shape == "response.failed" {
					failure = `{"type":"response.failed","response":{"status":"failed","error":{"code":"websocket_connection_limit_reached","message":"connection has reached maximum duration"}}}`
				}
				events := [][]byte{}
				if afterOutput {
					events = append(events, []byte(`{"type":"response.output_text.delta","delta":"partial"}`))
				}
				events = append(events, []byte(failure))
				first := &openAIWSCaptureConn{events: events}
				second := &openAIWSCaptureConn{events: [][]byte{[]byte(`{"type":"response.completed","response":{"id":"resp_recovered","status":"completed","usage":{"input_tokens":1,"output_tokens":1}}}`)}}
				svc, account, dialer := newCodexSyncTestService(t, first, second)
				client, done := openCodexSyncIngress(t, svc, account, nil)
				codexSyncWrite(t, client, `{"type":"response.create","model":"gpt-5.4","stream":true,"input":[{"role":"user","content":"first"}]}`)
				if afterOutput {
					require.Equal(t, "partial", gjson.GetBytes(codexSyncRead(t, client), "delta").String())
					require.Contains(t, string(codexSyncRead(t, client)), "websocket_connection_limit_reached")
					require.Equal(t, 1, dialer.DialCount())
					client.CloseNow()
				} else {
					require.Equal(t, "resp_recovered", gjson.GetBytes(codexSyncRead(t, client), "response.id").String())
					finishCodexSyncIngress(t, client, done)
					require.Equal(t, 2, dialer.DialCount())
				}
			})
		}
	}
}

func TestOpenAICodexSyncOAuthHTTPKeepsRealResultsAndFiltersImages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"model":"gpt-5.4","stream":true,"input":[{"type":"function_call","call_id":"call_a","name":"read_data","arguments":"{}"},{"type":"function_call_output","call_id":"call_a","output":"actual result"},{"type":"custom_tool_call","call_id":"call_b","name":"edit_data","input":"patch"}],"tools":[{"type":"web_search","search_content_types":["image","text"],"image_settings":{"max_results":3}}]}`
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_http\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n"))}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream, toolCorrector: NewCodexToolCorrector()}
	account := &Account{ID: 815, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Credentials: map[string]any{"access_token": "local-test-token"}}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	_, err := svc.Forward(context.Background(), c, account, []byte(body))
	require.NoError(t, err)
	require.Len(t, upstream.bodies, 1)
	require.Equal(t, `["text"]`, gjson.GetBytes(upstream.lastBody, "tools.0.search_content_types").Raw)
	require.False(t, gjson.GetBytes(upstream.lastBody, "tools.0.image_settings").Exists())
	require.Contains(t, string(upstream.lastBody), "actual result")
	require.Equal(t, "aborted", gjson.GetBytes(upstream.lastBody, `input.#(type=="custom_tool_call_output").output`).String())
}

func TestOpenAICodexSyncLiteImageSearchTypes(t *testing.T) {
	body := []byte(`{"input":[{"type":"additional_tools","tools":[{"type":"web_search","search_content_types":["image","text"]}]},{"type":"message","role":"user","content":"find images"}]}`)
	updated, changed, err := normalizeOpenAIOAuthImageSearchBody(body)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, `["text"]`, gjson.GetBytes(updated, "input.0.tools.0.search_content_types").Raw)
	var fields map[string]any
	require.NoError(t, decodeOpenAIJSONUseNumber(body, &fields))
	require.True(t, normalizeOpenAIOAuthImageSearchFields(fields))
	input := fields["input"].([]any)
	tools := input[0].(map[string]any)["tools"].([]any)
	require.Equal(t, []any{"text"}, tools[0].(map[string]any)["search_content_types"])
}

func TestOpenAICodexSyncUnknownAnchorAcrossTransports(t *testing.T) {
	for _, transport := range []string{"http", "websocket"} {
		for _, passthrough := range []bool{false, true} {
			// 兼容透传固定走 HTTP；WS 测试使用原生管理链路。
			if transport == "websocket" && passthrough {
				continue
			}
			for _, lite := range []bool{false, true} {
				for _, accountType := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
					name := transport + "/passthrough_" + testBoolName(passthrough) + "/lite_" + testBoolName(lite) + "/" + accountType
					t.Run(name, func(t *testing.T) {
						event := `{"type":"response.completed","response":{"id":"resp_synced","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1}}}`
						capture := &openAIWSCaptureConn{events: [][]byte{[]byte(event)}}
						svc, account, _ := newCodexSyncTestService(t, capture)
						svc.cfg.Gateway.OpenAIWS.Enabled = transport == "websocket"
						account.Type = accountType
						account.Credentials["api_key"] = "local-test-key"
						account.Extra["openai_passthrough"] = passthrough
						upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + event + "\n\n"))}}
						svc.httpUpstream = upstream
						body := []byte(`{"model":"gpt-5.6-terra","stream":true,"previous_response_id":"resp_unknown","input":[{"type":"custom_tool_call","call_id":"call_missing","name":"edit_data","input":"patch"},{"type":"custom_tool_call","call_id":"call_real","name":"read_data","input":"read"},{"type":"custom_tool_call_output","call_id":"call_real","output":"actual result"}],"tools":[{"type":"web_search_2025_08_26","search_content_types":["image"],"image_settings":{"max_results":3},"search_context_size":"high"}]}`)
						c, _ := gin.CreateTestContext(httptest.NewRecorder())
						c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))
						c.Request.Header.Set("User-Agent", "codex_cli_rs/0.163.0")
						if lite {
							c.Request.Header.Set(responsesLiteHeader, "true")
						}
						if transport == "http" {
							var compressed []byte
							if passthrough {
								encoder, err := zstd.NewWriter(nil)
								require.NoError(t, err)
								compressed = encoder.EncodeAll(body, nil)
								require.NoError(t, encoder.Close())
								c.Request.Header.Set("Content-Encoding", "zstd")
							} else {
								var buffer bytes.Buffer
								encoder := gzip.NewWriter(&buffer)
								_, err := encoder.Write(body)
								require.NoError(t, err)
								require.NoError(t, encoder.Close())
								compressed = buffer.Bytes()
								c.Request.Header.Set("Content-Encoding", "gzip")
							}
							c.Request.Body = io.NopCloser(bytes.NewReader(compressed))
							c.Request.ContentLength = int64(len(compressed))
							decoded, err := httputil.ReadRequestBodyWithPrealloc(c.Request)
							require.NoError(t, err)
							require.Empty(t, c.Request.Header.Get("Content-Encoding"))
							body = decoded
						}
						result, err := svc.Forward(context.Background(), c, account, body)
						require.NoError(t, err)
						require.NotNil(t, result)
						require.Equal(t, transport == "websocket", result.OpenAIWSMode)
						outbound := upstream.lastBody
						if transport == "websocket" {
							require.Len(t, capture.writes, 1)
							outbound, err = json.Marshal(capture.writes[0])
							require.NoError(t, err)
						} else {
							require.Len(t, upstream.bodies, 1)
						}
						if transport == "http" && !passthrough && accountType == AccountTypeOAuth {
							// 管理 HTTP 入口原本采用完整历史，原有去锚点策略继续保留。
							require.False(t, gjson.GetBytes(outbound, "previous_response_id").Exists())
						} else {
							require.Equal(t, "resp_unknown", gjson.GetBytes(outbound, "previous_response_id").String())
						}
						var outputs []gjson.Result
						calls := map[string]bool{}
						for _, item := range gjson.GetBytes(outbound, "input").Array() {
							switch item.Get("type").String() {
							case "custom_tool_call_output", "function_call_output":
								outputs = append(outputs, item)
							case "custom_tool_call", "function_call":
								calls[item.Get("call_id").String()] = true
							}
						}
						require.NotEmpty(t, outputs)
						require.Equal(t, "actual result", outputs[len(outputs)-1].Get("output").String())
						for _, output := range outputs {
							require.True(t, calls[output.Get("call_id").String()], "出站结果必须与出站调用保持同一 ID")
						}
						if accountType == AccountTypeOAuth {
							require.Len(t, outputs, 2)
							require.Contains(t, string(outbound), `"output":"aborted"`)
							require.NotContains(t, string(outbound), `"image_settings"`)
							require.Contains(t, string(outbound), `"search_content_types":["text"]`)
						} else {
							require.Len(t, outputs, 1)
							require.NotContains(t, string(outbound), "aborted")
							require.Contains(t, string(outbound), `"search_content_types":["image"]`)
						}
						require.Contains(t, string(outbound), `"search_context_size":"high"`)
					})
				}
			}
		}
	}
}

func TestOpenAICodexSyncIngressRepairsVisibleCallWithUnknownAnchor(t *testing.T) {
	capture := &openAIWSCaptureConn{events: [][]byte{[]byte(`{"type":"response.completed","response":{"id":"resp_ingress","status":"completed","usage":{"input_tokens":1,"output_tokens":1}}}`)}}
	svc, account, _ := newCodexSyncTestService(t, capture)
	account.Extra["openai_ws_allow_store_recovery"] = true
	client, done := openCodexSyncIngress(t, svc, account, nil)
	codexSyncWrite(t, client, `{"type":"response.create","model":"gpt-5.4","store":true,"previous_response_id":"resp_unknown","input":[{"type":"custom_tool_call","call_id":"call_missing","name":"edit_data","input":"patch"},{"role":"user","content":"continue"}],"tools":[{"type":"web_search_2025_08_26","search_content_types":["image"],"image_settings":{}}]}`)
	require.Equal(t, "resp_ingress", gjson.GetBytes(codexSyncRead(t, client), "response.id").String())
	finishCodexSyncIngress(t, client, done)
	require.Len(t, capture.writes, 1)
	outbound, err := json.Marshal(capture.writes[0])
	require.NoError(t, err)
	require.Equal(t, "resp_unknown", gjson.GetBytes(outbound, "previous_response_id").String())
	require.Equal(t, "aborted", gjson.GetBytes(outbound, `input.#(type=="custom_tool_call_output").output`).String())
	require.Equal(t, `["text"]`, gjson.GetBytes(outbound, "tools.0.search_content_types").Raw)
	require.False(t, gjson.GetBytes(outbound, "tools.0.image_settings").Exists())
}

func TestForwardOpenAIWSV2LifetimeFailureRespectsOutputBoundary(t *testing.T) {
	for _, afterOutput := range []bool{false, true} {
		t.Run(testBoolName(afterOutput), func(t *testing.T) {
			events := [][]byte{}
			if afterOutput {
				events = append(events, []byte(`{"type":"response.output_text.delta","delta":"partial"}`))
			}
			events = append(events, []byte(`{"type":"response.failed","response":{"id":"resp_expired","status":"failed","error":{"code":"websocket_connection_limit_reached","message":"connection has reached maximum duration"}}}`))
			conn := &openAIWSCaptureConn{events: events}
			svc, account, dialer := newCodexSyncTestService(t, conn)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			payload := map[string]any{"model": "gpt-5.4", "stream": true, "input": []any{map[string]any{"role": "user", "content": "hello"}}}
			tried := false
			result, err := svc.forwardOpenAIWSV2(context.Background(), c, account, payload, "", "thread-local", "local-test-token", OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2}, true, true, "gpt-5.4", "gpt-5.4", time.Now(), 1, "", &tried)
			require.Equal(t, 1, dialer.DialCount())
			if !afterOutput {
				var fallback *openAIWSFallbackError
				require.ErrorAs(t, err, &fallback)
				require.Equal(t, "ws_connection_limit_reached", fallback.Reason)
				require.Nil(t, result)
				require.False(t, c.Writer.Written())
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, "response.failed", result.UpstreamTerminalEvent)
				require.Contains(t, rec.Body.String(), "partial")
				require.Contains(t, rec.Body.String(), "websocket_connection_limit_reached")
			}
		})
	}
}

func TestOpenAIWSLifetimePrewarmFailedEvent(t *testing.T) {
	conn := &openAIWSCaptureConn{events: [][]byte{[]byte(`{"type":"response.failed","response":{"error":{"code":"websocket_connection_limit_reached","message":"connection has reached maximum duration"}}}`)}}
	svc, account, _ := newCodexSyncTestService(t, conn)
	svc.cfg.Gateway.OpenAIWS.PrewarmGenerateEnabled = true
	lease, err := svc.openaiWSPool.Acquire(context.Background(), openAIWSAcquireRequest{Account: account, WSURL: "wss://example.com/responses", Headers: http.Header{}})
	require.NoError(t, err)
	defer lease.Release()
	err = svc.performOpenAIWSGeneratePrewarm(context.Background(), lease, OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2}, map[string]any{"model": "gpt-5.4"}, "", map[string]any{"model": "gpt-5.4"}, account, nil, 0)
	var fallback *openAIWSFallbackError
	require.True(t, errors.As(err, &fallback))
	require.Equal(t, "prewarm_ws_connection_limit_reached", fallback.Reason)
	require.True(t, lease.conn.isClosed())
}
