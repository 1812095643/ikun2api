package adminapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/MACOS-DO/sub4api/internal/monitoring/channels"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/incidents"
	"github.com/MACOS-DO/sub4api/internal/monitoring/store"
)

// fakeFeishu records requests and answers like the Feishu OpenAPI.
type fakeFeishu struct {
	mu      sync.Mutex
	secret  string
	paths   []string
	origins []string
	sent    []string
}

func (f *fakeFeishu) handler(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paths = append(f.paths, r.Method+" "+r.URL.Path)
	f.origins = append(f.origins, r.Header.Get("X-Origin"))
	w.Header().Set("Content-Type", "application/json")
	switch {
	case strings.HasSuffix(r.URL.Path, "tenant_access_token/internal"):
		var in map[string]string
		json.NewDecoder(r.Body).Decode(&in)
		if in["app_secret"] != f.secret {
			fmt.Fprint(w, `{"code":10014,"msg":"app secret invalid"}`)
			return
		}
		fmt.Fprint(w, `{"code":0,"tenant_access_token":"t","expire":7200}`)
	case r.URL.Path == "/open-apis/bot/v3/info":
		fmt.Fprint(w, `{"code":0,"bot":{"app_name":"心链监控"}}`)
	case r.URL.Path == "/open-apis/im/v1/chats":
		if r.URL.Query().Get("page_token") == "" {
			fmt.Fprint(w, `{"code":0,"data":{"items":[{"chat_id":"oc_ops1","name":"运维群"}],"has_more":true,"page_token":"p2"}}`)
		} else {
			fmt.Fprint(w, `{"code":0,"data":{"items":[{"chat_id":"oc_ops2","name":"值班群"},{"chat_id":"bad id","name":"x"}],"has_more":false}}`)
		}
	case r.URL.Path == "/open-apis/im/v1/messages":
		f.sent = append(f.sent, r.URL.Query().Get("receive_id_type"))
		fmt.Fprint(w, `{"code":0,"data":{"message_id":"om_test1"}}`)
	default:
		w.WriteHeader(404)
	}
}

// redirect sends every request to the fake server and remembers the real origin.
type redirect struct{ to *url.URL }

func (r redirect) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("X-Origin", req.URL.Scheme+"://"+req.URL.Host)
	req.URL.Scheme, req.URL.Host = r.to.Scheme, r.to.Host
	return http.DefaultTransport.RoundTrip(req)
}

func channelAPI(t *testing.T, env channels.Settings) (http.Handler, *store.Store, *fakeFeishu, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	fake := &fakeFeishu{secret: "good-secret"}
	srv := httptest.NewServer(http.HandlerFunc(fake.handler))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	keyPath := filepath.Join(dir, "channel.key")
	reg, err := channels.Open(s, keyPath, env)
	if err != nil {
		t.Fatal(err)
	}
	reg.Transport = redirect{u}
	return (&API{Store: s, Manager: &incidents.Manager{Store: s}, Channels: reg}).Handler(), s, fake, keyPath
}

func call(t *testing.T, h http.Handler, method, path, body string) (int, map[string]any) {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("X-Ops-Actor", "admin:3")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var out map[string]any
	json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func TestChannelsSaveVerifiesEncryptsAndMasks(t *testing.T) {
	h, s, fake, keyPath := channelAPI(t, channels.Settings{})
	code, out := call(t, h, "GET", "/v1/channels", "")
	if code != 200 || out["data"].(map[string]any)["source"] != "none" {
		t.Fatal(code, out)
	}
	// Wrong secret: verified against Feishu and rejected, nothing saved.
	code, out = call(t, h, "PUT", "/v1/channels", `{"feishu":{"domain":"lark","app_id":"cli_abcdef123456","app_secret":"wrong"},"email":{}}`)
	if code != 400 || !strings.Contains(out["message"].(string), "飞书验证失败") {
		t.Fatal(code, out)
	}
	code, out = call(t, h, "PUT", "/v1/channels", `{"feishu":{"domain":"lark","app_id":"cli_abcdef123456","app_secret":"good-secret"},"email":{"host":"smtp.qq.com","port":465,"security":"ssl","username":"ops@qq.com","password":"mail-pass","from":"ops@qq.com"}}`)
	if code != 200 {
		t.Fatal(code, out)
	}
	v := out["data"].(map[string]any)
	if v["source"] != "page" || v["feishu_secret_set"] != true || v["email_password_set"] != true || v["updated_by"] != "admin:3" {
		t.Fatal(v)
	}
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), "good-secret") || strings.Contains(string(raw), "mail-pass") {
		t.Fatal("secret returned by API", string(raw))
	}
	// International version talks to open.larksuite.com.
	if fake.origins[len(fake.origins)-1] != "https://open.larksuite.com" {
		t.Fatal(fake.origins)
	}
	// Stored ciphertext only; key file is 0600.
	var st map[string]any
	if err := s.State(context.Background(), "notification_channels", &st); err != nil {
		t.Fatal(err)
	}
	dump, _ := json.Marshal(st)
	if strings.Contains(string(dump), "good-secret") || strings.Contains(string(dump), "mail-pass") {
		t.Fatal("secret stored in plain text", string(dump))
	}
	if info, err := os.Stat(keyPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("key file mode", err)
	}
	// Blank secrets keep the stored ones.
	code, out = call(t, h, "PUT", "/v1/channels", `{"feishu":{"domain":"lark","app_id":"cli_abcdef123456","app_secret":""},"email":{"host":"smtp.qq.com","port":465,"security":"ssl","username":"ops@qq.com","password":"","from":"ops@qq.com"}}`)
	if code != 200 || out["data"].(map[string]any)["email_password_set"] != true {
		t.Fatal(code, out)
	}
	code, out = call(t, h, "GET", "/v1/summary", "")
	tr := out["data"].(map[string]any)["transports"].(map[string]any)
	if code != 200 || tr["feishu"] != true || tr["email"] != true {
		t.Fatal(tr)
	}
}

func TestChannelsEnvFallbackAndValidation(t *testing.T) {
	env := channels.Settings{Feishu: channels.Feishu{Domain: "feishu", AppID: "cli_envapp12345", AppSecret: "good-secret"}}
	h, _, _, _ := channelAPI(t, env)
	code, out := call(t, h, "GET", "/v1/channels", "")
	v := out["data"].(map[string]any)
	if code != 200 || v["source"] != "env" || v["feishu_secret_set"] != true || v["feishu"].(map[string]any)["app_id"] != "cli_envapp12345" {
		t.Fatal(v)
	}
	for body, msg := range map[string]string{
		`{"feishu":{"domain":"feishu","app_id":"not-an-id","app_secret":"x"},"email":{}}`:           "App ID",
		`{"feishu":{},"email":{"host":"smtp.qq.com","port":99999,"security":"ssl","from":"a@b.c"}}`: "端口",
		`{"feishu":{},"email":{"host":"smtp.qq.com","port":465,"security":"plain","from":"a@b.c"}}`: "加密方式",
		`{"feishu":{},"email":{"host":"smtp.qq.com","port":465,"security":"ssl","from":"bad"}}`:     "发件人",
		`{"feishu":{"domain":"evil","app_id":"","app_secret":""},"email":{}}`:                       "国内版",
		`{"feishu":{"domain":"feishu","app_id":"cli_abcdef123456","app_secret":"a b"},"email":{}}`:  "App Secret",
	} {
		code, out = call(t, h, "PUT", "/v1/channels", body)
		if code != 400 || !strings.Contains(out["message"].(string), msg) {
			t.Fatalf("%s → %d %v", body, code, out)
		}
	}
}

func TestChannelsCannotClearChannelInUse(t *testing.T) {
	h, s, _, _ := channelAPI(t, channels.Settings{})
	if code, out := call(t, h, "PUT", "/v1/channels", `{"feishu":{"domain":"feishu","app_id":"cli_abcdef123456","app_secret":"good-secret"},"email":{}}`); code != 200 {
		t.Fatal(out)
	}
	cfg := domain.DefaultConfig()
	cfg.Shadow, cfg.FeishuTargets, cfg.Actor, cfg.RequestID = false, []string{"oc_ops1"}, "admin:3", "c1"
	if err := s.SaveConfig(context.Background(), cfg, 0); err != nil {
		t.Fatal(err)
	}
	code, out := call(t, h, "PUT", "/v1/channels", `{"feishu":{"domain":"feishu","app_id":""},"email":{}}`)
	if code != 400 || !strings.Contains(out["message"].(string), "不能清空") {
		t.Fatal(code, out)
	}
}

func TestFeishuChatsAndTestSend(t *testing.T) {
	h, _, fake, _ := channelAPI(t, channels.Settings{})
	code, out := call(t, h, "POST", "/v1/channels/feishu/chats", `{}`)
	if code != 400 || !strings.Contains(out["message"].(string), "先填写") {
		t.Fatal(code, out)
	}
	if code, out = call(t, h, "PUT", "/v1/channels", `{"feishu":{"domain":"feishu","app_id":"cli_abcdef123456","app_secret":"good-secret"},"email":{}}`); code != 200 {
		t.Fatal(out)
	}
	code, out = call(t, h, "POST", "/v1/channels/feishu/chats", `{}`)
	data := out["data"].(map[string]any)
	chats := data["chats"].([]any)
	if code != 200 || data["bot_name"] != "心链监控" || len(chats) != 2 || chats[1].(map[string]any)["chat_id"] != "oc_ops2" {
		t.Fatal(code, out)
	}
	if fake.origins[len(fake.origins)-1] != "https://open.feishu.cn" {
		t.Fatal(fake.origins)
	}
	code, out = call(t, h, "POST", "/v1/channels/test", `{"channel":"feishu","target":"ou_me"}`)
	if code != 200 || len(fake.sent) != 1 || fake.sent[0] != "open_id" {
		t.Fatal(code, out, fake.sent)
	}
	for _, body := range []string{`{"channel":"feishu","target":"https://hook"}`, `{"channel":"sms","target":"x"}`, `{"channel":"email","target":"a@b.c"}`} {
		if code, out = call(t, h, "POST", "/v1/channels/test", body); code != 400 {
			t.Fatal(body, code, out)
		}
	}
	if len(fake.sent) != 1 {
		t.Fatal("invalid tests must not send", fake.sent)
	}
}

func TestSavingEmailDoesNotReverifyUnchangedFeishu(t *testing.T) {
	h, _, fake, _ := channelAPI(t, channels.Settings{})
	if code, out := call(t, h, "PUT", "/v1/channels", `{"feishu":{"domain":"feishu","app_id":"cli_abcdef123456","app_secret":"good-secret"},"email":{}}`); code != 200 {
		t.Fatal(out)
	}
	before := len(fake.paths)
	if code, out := call(t, h, "PUT", "/v1/channels", `{"feishu":{"domain":"feishu","app_id":"cli_abcdef123456","app_secret":""},"email":{"host":"smtp.qq.com","port":465,"security":"ssl","username":"u@qq.com","password":"pw","from":"u@qq.com"}}`); code != 200 {
		t.Fatal(out)
	}
	if len(fake.paths) != before {
		t.Fatal("unchanged Feishu credentials were re-verified", fake.paths[before:])
	}
}
