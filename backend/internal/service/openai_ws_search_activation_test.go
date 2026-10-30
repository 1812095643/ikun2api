package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/config"
	coderws "github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestHostedSearchPhysicalSocketInitialization(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		blocked   bool
	}{
		{"empty_prewarm", `{"type":"response.create","generate":false,"input":[]}`, true},
		{"live", `{"type":"response.create","tools":[{"type":"web_search","external_web_access":true}]}`, false},
		{"cached", `{"type":"response.create","tools":[{"type":"web_search","external_web_access":false}]}`, false},
		{"lite", `{"type":"response.create","input":[{"type":"additional_tools","tools":[{"type":"web_search"}]}]}`, false},
		{"preview", `{"type":"response.create","tools":[{"type":"web_search_preview_2025_03_11"}]}`, false},
		{"function", `{"type":"response.create","tools":[{"type":"function","name":"web_search"}]}`, true},
	} {
		for _, format := range []string{"map", "raw", "bytes"} {
			t.Run(tc.name+"/"+format, func(t *testing.T) {
				conn := newOpenAIWSConn("fixture", 42, &openAIWSCaptureConn{}, nil)
				defer conn.close()
				lease := &openAIWSConnLease{conn: conn}
				require.False(t, lease.initializedWithoutWebSearch(), "handshake-only connection remains usable")
				require.NoError(t, lease.WriteJSON(json.RawMessage(`{"type":"response.cancel"}`), time.Second))
				require.False(t, lease.initializedWithoutWebSearch())
				var payload any
				switch format {
				case "map":
					require.NoError(t, json.Unmarshal([]byte(tc.raw), &payload))
				case "raw":
					payload = json.RawMessage(tc.raw)
				case "bytes":
					payload = []byte(tc.raw)
				}
				require.NoError(t, lease.WriteJSON(payload, time.Second))
				lease = &openAIWSConnLease{conn: conn}
				require.Equal(t, tc.blocked, lease.initializedWithoutWebSearch())
				require.NoError(t, lease.WriteJSON(json.RawMessage(`{"type":"response.create","tools":[{"type":"web_search"}]}`), time.Second))
				require.Equal(t, tc.blocked, lease.initializedWithoutWebSearch(), "later creates must not overwrite capability")
			})
		}
	}
	capture := &openAIWSCaptureConn{closed: true}
	conn := newOpenAIWSConn("failed", 42, capture, nil)
	defer conn.close()
	lease := &openAIWSConnLease{conn: conn}
	require.Error(t, lease.WriteJSON(map[string]any{"type": "response.create"}, time.Second))
	require.False(t, conn.responseCreateSent, "failed writes do not initialize socket capability")
}

type openAIWSSearchLocalDialer struct {
	url    string
	dialer openAIWSClientDialer
	dials  atomic.Int64
}

func (d *openAIWSSearchLocalDialer) Dial(ctx context.Context, _ string, headers http.Header, _ string) (openAIWSClientConn, int, http.Header, error) {
	d.dials.Add(1)
	return d.dialer.Dial(ctx, d.url, headers, "")
}

type openAIWSSearchSeen struct {
	conn int64
	body []byte
}
type openAIWSSearchFixture struct {
	svc     *OpenAIGatewayService
	account *Account
	cfg     *config.Config
	dialer  *openAIWSSearchLocalDialer
	seenMu  sync.Mutex
	seen    []openAIWSSearchSeen
}

func newOpenAIWSSearchFixture(t *testing.T, enabled bool, reply func(*coderws.Conn, int64, []byte)) *openAIWSSearchFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	f := &openAIWSSearchFixture{}
	var next atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		id := next.Add(1)
		for {
			_, raw, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			f.seenMu.Lock()
			f.seen = append(f.seen, openAIWSSearchSeen{id, append([]byte(nil), raw...)})
			f.seenMu.Unlock()
			reply(conn, id, raw)
		}
	}))
	t.Cleanup(server.Close)
	f.cfg = newOpenAIWSV2TestConfig()
	f.cfg.Security.URLAllowlist.Enabled = false
	f.cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	f.cfg.Gateway.OpenAIWS.SearchInitEnabled = enabled
	f.cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 2
	f.cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 2
	f.cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
	f.cfg.Gateway.OpenAIWS.QueueLimitPerConn = 8
	f.cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 3
	f.cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 7
	f.cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3
	f.dialer = &openAIWSSearchLocalDialer{url: "ws" + strings.TrimPrefix(server.URL, "http"), dialer: newDefaultOpenAIWSClientDialer()}
	pool := newOpenAIWSConnPool(f.cfg)
	pool.setClientDialerForTest(f.dialer)
	t.Cleanup(pool.Close)
	f.svc = &OpenAIGatewayService{cfg: f.cfg, httpUpstream: &httpUpstreamRecorder{}, cache: &stubGatewayCache{}, openaiWSResolver: NewOpenAIWSProtocolResolver(f.cfg), toolCorrector: NewCodexToolCorrector(), openaiWSPool: pool, openaiWSPassthroughDialer: f.dialer}
	f.account = &Account{ID: 707, Name: "local-search-fixture", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 2, Credentials: map[string]any{"access_token": "fixture-access-token"}, Extra: map[string]any{"responses_websockets_v2_enabled": true}}
	return f
}
func searchFixtureEmit(conn *coderws.Conn, raw string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = conn.Write(ctx, coderws.MessageText, []byte(raw))
}
func (f *openAIWSSearchFixture) forward(t *testing.T, raw string) (*OpenAIForwardResult, error, *httptest.ResponseRecorder) {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(raw))
	c.Request.Header.Set("session_id", "local-search-thread")
	result, err := f.svc.Forward(context.Background(), c, f.account, []byte(raw))
	return result, err, rec
}
func (f *openAIWSSearchFixture) frames() []openAIWSSearchSeen {
	f.seenMu.Lock()
	defer f.seenMu.Unlock()
	return append([]openAIWSSearchSeen(nil), f.seen...)
}

func TestOpenAIWSSearchInitializationNativeHTTP(t *testing.T) {
	for _, lite := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("lite_%v/stream_%v", lite, stream), func(t *testing.T) {
				var business atomic.Int64
				f := newOpenAIWSSearchFixture(t, true, func(conn *coderws.Conn, _ int64, raw []byte) {
					if gjson.GetBytes(raw, "generate").Type == gjson.False {
						require.True(t, openAIWSWebSearchDeclared(raw))
						require.Equal(t, "none", gjson.GetBytes(raw, "tool_choice").String())
						require.NotContains(t, string(raw), "original question")
						require.NotContains(t, string(raw), "business instructions")
						require.False(t, gjson.GetBytes(raw, "previous_response_id").Exists())
						searchFixtureEmit(conn, `{"type":"response.completed","response":{"id":"resp_hidden_init","status":"completed","output":[],"usage":{"input_tokens":99,"output_tokens":0}}}`)
						return
					}
					n := business.Add(1)
					if n == 1 {
						searchFixtureEmit(conn, `{"type":"response.completed","response":{"id":"resp_first","status":"completed","model":"gpt-5.5","output":[{"type":"function_call","id":"fc_read","call_id":"call_read","name":"read","arguments":"{}"}],"usage":{"input_tokens":2,"output_tokens":1}}}`)
					} else {
						searchFixtureEmit(conn, `{"type":"response.completed","response":{"id":"resp_search","status":"completed","model":"gpt-5.5","output":[],"usage":{"input_tokens":3,"output_tokens":1}}}`)
					}
				})
				f.cfg.Gateway.OpenAIWS.PrewarmGenerateEnabled = true
				carrier := ""
				if lite {
					carrier = `,{"type":"additional_tools","role":"developer","tools":[{"type":"function","name":"read","parameters":{"type":"object"}}]}`
				}
				first := fmt.Sprintf(`{"model":"gpt-5.5","stream":%v,"instructions":"business instructions","tool_choice":"auto","input":[{"role":"user","content":"original question"}%s],"tools":[{"type":"function","name":"read","parameters":{"type":"object"}}]}`, stream, carrier)
				result, err, rec := f.forward(t, first)
				require.NoError(t, err)
				require.Equal(t, "resp_first", result.ResponseID)
				require.NotContains(t, rec.Body.String(), "resp_hidden_init")
				require.Equal(t, 2, result.Usage.InputTokens)
				second := fmt.Sprintf(`{"model":"gpt-5.5","stream":%v,"previous_response_id":"resp_first","input":[{"type":"function_call_output","call_id":"call_read","output":"actual result"}],"tools":[{"type":"web_search","external_web_access":true}]}`, stream)
				result, err, _ = f.forward(t, second)
				require.NoError(t, err)
				require.Equal(t, "resp_search", result.ResponseID)
				frames := f.frames()
				require.Len(t, frames, 3)
				require.Equal(t, frames[0].conn, frames[1].conn)
				require.Equal(t, frames[1].conn, frames[2].conn)
				require.False(t, openAIWSWebSearchDeclared(frames[1].body))
				require.Equal(t, "resp_first", gjson.GetBytes(frames[2].body, "previous_response_id").String())
				require.Contains(t, string(frames[2].body), "actual result")
				_, bound := f.svc.getOpenAIWSStateStore().GetResponseConn("resp_hidden_init")
				require.False(t, bound)
			})
		}
	}
}

func TestOpenAIWSSearchInitializationExistingSearchAndAPIKey(t *testing.T) {
	for _, apikey := range []bool{false, true} {
		t.Run(fmt.Sprint(apikey), func(t *testing.T) {
			f := newOpenAIWSSearchFixture(t, true, func(conn *coderws.Conn, _ int64, raw []byte) {
				require.NotEqual(t, gjson.False, gjson.GetBytes(raw, "generate").Type)
				searchFixtureEmit(conn, `{"type":"response.completed","response":{"id":"resp_direct","status":"completed","output":[]}}`)
			})
			tools := `,"tools":[{"type":"web_search","external_web_access":true}]`
			if apikey {
				f.account.Type = AccountTypeAPIKey
				f.account.Credentials = map[string]any{"api_key": "sk-fixture"}
				tools = ""
			}
			_, err, _ := f.forward(t, `{"model":"gpt-5.5","input":[]`+tools+`}`)
			require.NoError(t, err)
			require.Len(t, f.frames(), 1)
		})
	}
}

func TestOpenAIWSSearchInitializationFallbackAndActivation(t *testing.T) {
	var business atomic.Int64
	f := newOpenAIWSSearchFixture(t, true, func(conn *coderws.Conn, _ int64, raw []byte) {
		if gjson.GetBytes(raw, "generate").Type == gjson.False {
			searchFixtureEmit(conn, `{"type":"error","status":400,"error":{"type":"invalid_request_error","code":"unsupported_parameter","param":"tools","message":"fixture initializer rejected"}}`)
			return
		}
		n := business.Add(1)
		searchFixtureEmit(conn, fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_%d","status":"completed","output":[]}}`, n))
	})
	f.cfg.Gateway.OpenAIWS.PrewarmGenerateEnabled = true
	_, err, rec := f.forward(t, `{"model":"gpt-5.5","stream":true,"input":"original request"}`)
	require.NoError(t, err)
	require.NotContains(t, rec.Body.String(), "initializer rejected")
	_, err, _ = f.forward(t, `{"model":"gpt-5.5","stream":true,"input":"complete new request","tools":[{"type":"web_search","external_web_access":true}]}`)
	require.NoError(t, err)
	frames := f.frames()
	require.Len(t, frames, 3)
	require.NotEqual(t, frames[0].conn, frames[1].conn)
	require.NotEqual(t, frames[1].conn, frames[2].conn)
	require.Equal(t, int64(2), business.Load())
}

func TestOpenAIWSSearchInitializationSemanticErrors(t *testing.T) {
	for _, status := range []int{401, 403, 429, 503} {
		for _, terminal := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/terminal_%v", status, terminal), func(t *testing.T) {
				f := newOpenAIWSSearchFixture(t, true, func(conn *coderws.Conn, _ int64, raw []byte) {
					require.Equal(t, gjson.False, gjson.GetBytes(raw, "generate").Type)
					if terminal {
						searchFixtureEmit(conn, fmt.Sprintf(`{"type":"response.failed","response":{"status":"failed","error":{"status_code":%d,"type":"fixture_error","message":"fixture failure","headers":{"Retry-After":"7"}}}}`, status))
					} else {
						searchFixtureEmit(conn, fmt.Sprintf(`{"type":"error","status":%d,"headers":{"Retry-After":"7"},"error":{"type":"fixture_error","message":"fixture failure"}}`, status))
					}
				})
				_, err, rec := f.forward(t, `{"model":"gpt-5.5","stream":true,"input":[]}`)
				require.Error(t, err)
				require.Equal(t, status, rec.Code)
				require.Equal(t, "7", rec.Header().Get("Retry-After"))
				require.Contains(t, rec.Body.String(), "fixture failure")
				require.Len(t, f.frames(), 1)
			})
		}
	}
}

func TestOpenAIWSSearchActivationRejectsUnknownHTTPHistory(t *testing.T) {
	f := newOpenAIWSSearchFixture(t, false, func(conn *coderws.Conn, _ int64, raw []byte) {
		searchFixtureEmit(conn, `{"type":"response.completed","response":{"id":"resp_anchor","status":"completed","output":[]}}`)
	})
	_, err, _ := f.forward(t, `{"model":"gpt-5.5","input":"first"}`)
	require.NoError(t, err)
	f.svc.openaiWSHostedReplayMu.Lock()
	f.svc.openaiWSHostedReplay = nil
	f.svc.openaiWSHostedReplayMu.Unlock()
	_, err, rec := f.forward(t, `{"model":"gpt-5.5","previous_response_id":"resp_anchor","input":"search delta","tools":[{"type":"web_search"}]}`)
	require.Error(t, err)
	require.Equal(t, 409, rec.Code)
	require.Len(t, f.frames(), 1)
	_, err, _ = f.forward(t, `{"model":"gpt-5.5","input":"complete ordinary turn"}`)
	require.NoError(t, err)
	frames := f.frames()
	require.Len(t, frames, 2)
	require.Equal(t, frames[0].conn, frames[1].conn, "local history rejection must not discard the unused healthy socket")
}

func TestOpenAIWSSearchInitializationCancellationAndUnexpectedOutput(t *testing.T) {
	for _, mode := range []string{"cancel", "timeout", "output", "failed"} {
		t.Run(mode, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Gateway.OpenAIWS.SearchInitEnabled = true
			cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 10
			capture := &openAIWSCaptureConn{events: [][]byte{[]byte(`{"type":"response.completed","response":{"id":"resp_hidden","status":"completed","output":[]}}`)}}
			if mode == "cancel" || mode == "timeout" {
				capture.readDelays = []time.Duration{10 * time.Second}
			}
			if mode == "output" {
				capture.events = [][]byte{[]byte(`{"type":"response.output_text.delta","delta":"must not escape"}`)}
			}
			if mode == "failed" {
				capture.events = [][]byte{[]byte(`{"type":"response.incomplete","response":{"status":"incomplete"}}`)}
			}
			conn := newOpenAIWSConn("local", 1, capture, nil)
			defer conn.close()
			lease := &openAIWSConnLease{conn: conn}
			ctx := context.Background()
			if mode == "cancel" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				time.AfterFunc(30*time.Millisecond, cancel)
				defer cancel()
			}
			fallback, err := (&OpenAIGatewayService{cfg: cfg}).initializeOpenAIWSSearch(ctx, lease, map[string]any{"type": "response.create", "model": "gpt-5.5", "input": []any{}}, &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth})
			require.Error(t, err)
			require.Equal(t, mode == "timeout", fallback)
			if mode == "cancel" {
				require.ErrorIs(t, err, context.Canceled)
			}
		})
	}
}

func TestOpenAIWSSearchInitializationNativeIngress(t *testing.T) {
	for _, hostedTool := range []string{"web_search", "image_generation"} {
		for _, initialize := range []bool{true, false} {
			for _, passthrough := range []bool{true, false} {

				t.Run(fmt.Sprintf("%s/initialize_%v/passthrough_%v", hostedTool, initialize, passthrough), func(t *testing.T) {
					var business atomic.Int64
					f := newOpenAIWSSearchFixture(t, initialize, func(conn *coderws.Conn, _ int64, raw []byte) {
						if gjson.GetBytes(raw, "generate").Type == gjson.False {
							searchFixtureEmit(conn, `{"type":"response.completed","response":{"id":"resp_hidden","status":"completed","output":[]}}`)
							return
						}
						n := business.Add(1)
						if n == 1 {
							searchFixtureEmit(conn, `{"type":"response.completed","response":{"id":"resp_ingress_first","status":"completed","output":[{"type":"message","id":"msg_real","role":"assistant","content":[{"type":"output_text","text":"preserve real answer"}]},{"type":"function_call","id":"fc_read","call_id":"call_read","name":"read","arguments":"{}"}]}}`)
						} else {
							if !initialize || hostedTool == "image_generation" {
								require.False(t, gjson.GetBytes(raw, "previous_response_id").Exists())
								require.Contains(t, string(raw), "original question")
								require.Contains(t, string(raw), "preserve real answer")
								require.Contains(t, string(raw), "actual result")
							}
							searchFixtureEmit(conn, `{"type":"response.completed","response":{"id":"resp_ingress_search","status":"completed","output":[]}}`)
						}
					})
					if passthrough {
						f.cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
						f.account.Extra["openai_ws_mode"] = "passthrough"
						f.cfg.Gateway.OpenAIWS.IngressModeDefault = "passthrough"
					}
					done := make(chan error, 1)
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						conn, err := coderws.Accept(w, r, nil)
						if err != nil {
							done <- err
							return
						}
						defer conn.CloseNow()
						_, first, err := conn.Read(r.Context())
						if err != nil {
							done <- err
							return
						}
						c, _ := gin.CreateTestContext(httptest.NewRecorder())
						c.Request = r
						c.Request.Header.Set("session_id", "local-search-thread")
						done <- f.svc.ProxyResponsesWebSocketFromClient(r.Context(), c, conn, f.account, "fixture-access-token", first, nil)
					}))
					defer server.Close()
					ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
					defer cancel()
					client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
					require.NoError(t, err)
					defer client.CloseNow()
					first := json.RawMessage(`{"type":"response.create","model":"gpt-5.5","store":false,"client_metadata":{"session_id":"search-session","thread_id":"search-thread","x-codex-window-id":"search-same-window","x-codex-turn-metadata":"{\"session_id\":\"search-session\",\"thread_id\":\"search-thread\",\"window_id\":\"search-same-window\"}"},"input":[{"role":"user","content":"original question"}],"tools":[{"type":"function","name":"read","parameters":{"type":"object"}}]}`)
					require.NoError(t, wsjson.Write(ctx, client, first))
					for {
						_, raw, err := client.Read(ctx)
						require.NoError(t, err)
						require.NotContains(t, string(raw), "resp_hidden")
						if isOpenAIWSTerminalEvent(gjson.GetBytes(raw, "type").String()) {
							break
						}
					}
					second := json.RawMessage(`{"type":"response.create","model":"gpt-5.5","store":false,"client_metadata":{"session_id":"search-session","thread_id":"search-thread","x-codex-window-id":"search-same-window","x-codex-turn-metadata":"{\"session_id\":\"search-session\",\"thread_id\":\"search-thread\",\"window_id\":\"search-same-window\"}"},"previous_response_id":"resp_ingress_first","input":[{"type":"function_call_output","call_id":"call_read","output":"actual result"}],"tools":[{"type":"web_search","external_web_access":true}]}`)
					if hostedTool == "image_generation" {
						second = json.RawMessage(strings.Replace(string(second), `"type":"web_search","external_web_access":true`, `"type":"image_generation","quality":"high"`, 1))
					}
					require.NoError(t, wsjson.Write(ctx, client, second))
					for {
						_, raw, err := client.Read(ctx)
						require.NoError(t, err)
						if isOpenAIWSTerminalEvent(gjson.GetBytes(raw, "type").String()) {
							require.Contains(t, string(raw), "resp_ingress_search")
							break
						}
					}
					require.NoError(t, client.CloseNow())
					select {
					case err := <-done:
						if err != nil {
							var closeErr *OpenAIWSClientCloseError
							require.False(t, errors.As(err, &closeErr), err)
						}
					case <-time.After(3 * time.Second):
						t.Fatal("ingress did not stop")
					}
					frames := f.frames()
					if initialize {
						require.Len(t, frames, 3)
						if hostedTool == "web_search" {
							require.Equal(t, frames[0].conn, frames[2].conn)
						} else {
							require.NotEqual(t, frames[0].conn, frames[2].conn)
						}
					} else {
						require.Len(t, frames, 2)
						require.NotEqual(t, frames[0].conn, frames[1].conn)
					}
				})
			}
		}
	}
}
