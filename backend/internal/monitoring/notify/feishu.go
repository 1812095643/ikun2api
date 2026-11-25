package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var platformID = regexp.MustCompile(`^(oc_|ou_|om_)[a-zA-Z0-9_\-]{1,128}$`)

type Feishu struct {
	appID, secret, base, token string
	expires                    time.Time
	client                     *http.Client
	mu                         sync.Mutex
}

func NewFeishu(appID, secret string) *Feishu {
	return NewFeishuWithTransport(appID, secret, &http.Transport{Proxy: nil})
}

// NewFeishuWithTransport supports instrumented transports and isolated protocol
// tests. The API origin, timeout and redirect policy remain fixed; no management
// setting can change the origin or supply a transport.
func NewFeishuWithTransport(appID, secret string, transport http.RoundTripper) *Feishu {
	if transport == nil {
		transport = &http.Transport{Proxy: nil}
	}
	return &Feishu{appID: appID, secret: secret, base: "https://open.feishu.cn", client: &http.Client{Timeout: 8 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

type apiResponse struct {
	Code   *int   `json:"code"`
	Token  string `json:"tenant_access_token"`
	Expire int    `json:"expire"`
	Data   struct {
		MessageID string `json:"message_id"`
	} `json:"data"`
}

func (f *Feishu) call(ctx context.Context, method, path, token string, body any) (apiResponse, error) {
	var out apiResponse
	b, err := json.Marshal(body)
	if err != nil || len(b) > 30*1024 {
		return out, &Failure{Class: "payload_invalid", Permanent: true}
	}
	req, err := http.NewRequestWithContext(ctx, method, f.base+path, bytes.NewReader(b))
	if err != nil {
		return out, &Failure{Class: "endpoint_invalid", Permanent: true}
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := f.client.Do(req)
	if err != nil {
		return out, &Failure{Class: "network_or_timeout"}
	}
	defer res.Body.Close()
	decode := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&out)
	if res.StatusCode == 429 || out.Code != nil && *out.Code == 99991400 {
		retry := time.Second * 30
		if n, e := strconv.Atoi(res.Header.Get("Retry-After")); e == nil && n > 0 {
			retry = time.Duration(n) * time.Second
		} else if at, e := http.ParseTime(res.Header.Get("Retry-After")); e == nil && time.Until(at) > 0 {
			retry = time.Until(at)
		}
		return out, &Failure{Class: "rate_limited", RetryAfter: retry}
	}
	if res.StatusCode >= 500 {
		return out, &Failure{Class: "platform_unavailable"}
	}
	if res.StatusCode == 401 || res.StatusCode == 403 {
		return out, &Failure{Class: "authentication_or_permission", Permanent: true}
	}
	if res.StatusCode == 404 && method == "PATCH" {
		return out, &Failure{Class: "message_missing", MessageMissing: true}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return out, &Failure{Class: "platform_rejected", Permanent: true}
	}
	if decode != nil || out.Code == nil {
		return out, &Failure{Class: "invalid_platform_response"}
	}
	if *out.Code != 0 {
		switch *out.Code {
		case 99991661, 99991663, 99991668:
			return out, &Failure{Class: "token_expired"}
		case 230011:
			return out, &Failure{Class: "message_missing", MessageMissing: method == "PATCH", Permanent: method != "PATCH"}
		default:
			return out, &Failure{Class: "platform_rejected", Permanent: true}
		}
	}
	return out, nil
}
func (f *Feishu) accessToken(ctx context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.token != "" && time.Now().Before(f.expires) {
		return f.token, nil
	}
	if f.appID == "" || f.secret == "" {
		return "", &Failure{Class: "credentials_missing", Permanent: true}
	}
	out, err := f.call(ctx, "POST", "/open-apis/auth/v3/tenant_access_token/internal", "", map[string]string{"app_id": f.appID, "app_secret": f.secret})
	if err != nil {
		return "", err
	}
	if out.Token == "" || out.Expire <= 0 {
		return "", &Failure{Class: "invalid_token_response"}
	}
	f.token = out.Token
	ttl := time.Duration(out.Expire) * time.Second
	f.expires = time.Now().Add(ttl - ttl/10)
	return f.token, nil
}
func (f *Feishu) Send(ctx context.Context, d domain.Delivery, messageID, base string) (string, error) {
	if !platformID.MatchString(d.Target) || !(strings.HasPrefix(d.Target, "oc_") || strings.HasPrefix(d.Target, "ou_")) {
		return "", &Failure{Class: "recipient_invalid", Permanent: true}
	}
	card, err := Card(d.Payload, base)
	if err != nil {
		return "", &Failure{Class: "payload_invalid", Permanent: true}
	}
	token, err := f.accessToken(ctx)
	if err != nil {
		return "", err
	}
	method := "POST"
	kind := "chat_id"
	if strings.HasPrefix(d.Target, "ou_") {
		kind = "open_id"
	}
	path := "/open-apis/im/v1/messages?receive_id_type=" + kind
	uuid := d.ID
	if len(uuid) > 50 {
		uuid = uuid[:50]
	}
	body := map[string]any{"receive_id": d.Target, "msg_type": "interactive", "content": string(card), "uuid": uuid}
	if messageID != "" {
		if !platformID.MatchString(messageID) || !strings.HasPrefix(messageID, "om_") {
			return "", &Failure{Class: "message_id_invalid", Permanent: true}
		}
		method = "PATCH"
		path = "/open-apis/im/v1/messages/" + url.PathEscape(messageID)
		body = map[string]any{"content": string(card)}
	}
	out, err := f.call(ctx, method, path, token, body)
	if failure, ok := err.(*Failure); ok && failure.Class == "token_expired" {
		f.mu.Lock()
		f.token = ""
		f.expires = time.Time{}
		f.mu.Unlock()
	}
	if err != nil {
		return "", err
	}
	if messageID != "" {
		return messageID, nil
	}
	if out.Data.MessageID == "" {
		return "", &Failure{Class: "missing_acceptance_id"}
	}
	return out.Data.MessageID, nil
}

// Reply is an outbound-only thread adapter. The current worker updates the
// original incident card; replies are available for later structured reports.
// It does not subscribe to messages or accept instructions from a chat.
func (f *Feishu) Reply(ctx context.Context, d domain.Delivery, parentID, base string) (string, error) {
	if !platformID.MatchString(parentID) || !strings.HasPrefix(parentID, "om_") {
		return "", &Failure{Class: "message_id_invalid", Permanent: true}
	}
	card, err := Card(d.Payload, base)
	if err != nil {
		return "", &Failure{Class: "payload_invalid", Permanent: true}
	}
	token, err := f.accessToken(ctx)
	if err != nil {
		return "", err
	}
	uuid := d.ID
	if len(uuid) > 50 {
		uuid = uuid[:50]
	}
	out, err := f.call(ctx, "POST", "/open-apis/im/v1/messages/"+url.PathEscape(parentID)+"/reply", token, map[string]any{"content": string(card), "msg_type": "interactive", "reply_in_thread": true, "uuid": uuid})
	if failure, ok := err.(*Failure); ok && failure.Class == "token_expired" {
		f.mu.Lock()
		f.token = ""
		f.expires = time.Time{}
		f.mu.Unlock()
	}
	if err != nil {
		return "", err
	}
	if out.Data.MessageID == "" {
		return "", &Failure{Class: "missing_acceptance_id"}
	}
	return out.Data.MessageID, nil
}
