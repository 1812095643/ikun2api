package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/tidwall/gjson"
)

type Failure struct {
	Class      string
	RetryAfter time.Duration
	Permanent  bool
	Uncertain  bool
}

func (e *Failure) Error() string { return FailureText(e.Class) }
func FailureText(class string) string {
	switch class {
	case "busy":
		return "通知正在发送，请稍后重试"
	case "credentials":
		return "凭据过期或不正确，请重新填写"
	case "permission":
		return "机器人没有发送权限，请检查授权和群成员"
	case "platform_restricted":
		return "平台限制主动发送，请检查近期互动或使用备用渠道"
	case "rate_limit":
		return "平台限流，正在等待重试"
	case "uncertain":
		return "未取得明确回执，平台可能已经接收"
	case "target":
		return "接收对象无法识别，请检查对象编号和机器人所属应用"
	case "network":
		return "连接通知平台遇到问题，将按规则重试"
	case "response":
		return "平台回执不完整，请查看渠道配置"
	case "disabled":
		return "渠道或接收对象已停用"
	case "stale":
		return "事件已变化，已跳过旧通知"
	default:
		return "平台暂时无法接收通知"
	}
}

type Message struct {
	EmailVariables         map[string]string `json:"-"`
	Title, Text, DetailURL string
	Event                  domain.Incident
	Kind                   string
	MentionAll             bool
}
type Result struct{ MessageID string }
type EmailSender func(context.Context, string, Message) error
type tokenEntry struct {
	value   string
	expires time.Time
}
type Sender struct {
	Client *http.Client
	Email  EmailSender
	mu     sync.Mutex
	tokens map[string]tokenEntry
}

func (s *Sender) Send(ctx context.Context, c domain.Channel, secrets map[string]string, t domain.Target, m Message, previous string) (Result, error) {
	if c.Kind == "email" {
		if s.Email == nil {
			return Result{}, &Failure{Class: "credentials", Permanent: true}
		}
		if err := s.Email(ctx, t.Address, m); err != nil {
			return Result{}, err
		}
		return Result{}, nil
	}
	for attempt := 0; attempt < 2; attempt++ {
		r, err := s.send(ctx, c, secrets, t, m, previous)
		var f *Failure
		if attempt == 0 && errors.As(err, &f) && f.Class == "credentials" && (c.Kind == "feishu_app" || c.Kind == "lark_app" || c.Kind == "dingtalk" || c.Kind == "qq") {
			s.mu.Lock()
			delete(s.tokens, fmt.Sprintf("%s:%d", c.ID, c.Revision))
			s.mu.Unlock()
			continue
		}
		return r, err
	}
	return Result{}, &Failure{Class: "credentials", Permanent: true}
}

func (s *Sender) send(ctx context.Context, c domain.Channel, secrets map[string]string, t domain.Target, m Message, previous string) (Result, error) {
	text := m.Title + "\n" + m.Text
	if m.DetailURL != "" {
		text += "\n" + m.DetailURL
	}
	method, endpoint := http.MethodPost, ""
	headers := map[string]string{}
	var body any
	switch c.Kind {
	case "telegram":
		endpoint = "https://api.telegram.org/bot" + secrets["bot_token"] + "/sendMessage"
		payload := map[string]any{"chat_id": t.Address, "text": text, "link_preview_options": map[string]bool{"is_disabled": true}}
		if id := t.Settings["thread_id"]; id != "" {
			n, e := strconv.ParseInt(id, 10, 64)
			if e != nil || n < 1 {
				return Result{}, &Failure{Class: "target", Permanent: true}
			}
			payload["message_thread_id"] = n
		}
		if previous != "" {
			id, e := strconv.ParseInt(previous, 10, 64)
			if e != nil {
				return Result{}, &Failure{Class: "target", Permanent: true}
			}
			payload["message_id"] = id
			delete(payload, "message_thread_id")
			endpoint = "https://api.telegram.org/bot" + secrets["bot_token"] + "/editMessageText"
		}
		body = payload
	case "feishu_webhook", "lark_webhook":
		endpoint = secrets["webhook"]
		payload := map[string]any{"msg_type": "interactive", "card": feishuCard(m)}
		if secret := secrets["signing_secret"]; secret != "" {
			stamp := strconv.FormatInt(time.Now().Unix(), 10)
			mac := hmac.New(sha256.New, []byte(stamp+"\n"+secret))
			payload["timestamp"] = stamp
			payload["sign"] = base64.StdEncoding.EncodeToString(mac.Sum(nil))
		}
		body = payload
	case "feishu_app", "lark_app":
		base := "https://open.feishu.cn"
		if c.Kind == "lark_app" {
			base = "https://open.larksuite.com"
		}
		token, err := s.accessToken(ctx, c, secrets)
		if err != nil {
			return Result{}, err
		}
		headers["Authorization"] = "Bearer " + token
		card, _ := json.Marshal(feishuCard(m))
		if previous != "" {
			method = http.MethodPatch
			endpoint = base + "/open-apis/im/v1/messages/" + url.PathEscape(previous)
			body = map[string]any{"content": string(card)}
		} else {
			targetType := t.Settings["type"]
			if targetType == "" {
				targetType = "chat_id"
			}
			if targetType != "chat_id" && targetType != "open_id" && targetType != "user_id" {
				return Result{}, &Failure{Class: "target", Permanent: true}
			}
			endpoint = base + "/open-apis/im/v1/messages?receive_id_type=" + targetType
			body = map[string]any{"receive_id": t.Address, "msg_type": "interactive", "content": string(card)}
		}
	case "wecom":
		endpoint = secrets["webhook"]
		mentions := strings.FieldsFunc(t.Settings["mentions"], func(r rune) bool { return r == ',' || r == ' ' })
		if m.MentionAll {
			mentions = append(mentions, "@all")
		}
		body = map[string]any{"msgtype": "text", "text": map[string]any{"content": text, "mentioned_list": mentions}}
	case "dingtalk":
		token, err := s.accessToken(ctx, c, secrets)
		if err != nil {
			return Result{}, err
		}
		headers["x-acs-dingtalk-access-token"] = token
		payload := map[string]any{"robotCode": c.Settings["robot_code"], "msgKey": "sampleText"}
		param, _ := json.Marshal(map[string]any{"content": text})
		payload["msgParam"] = string(param)
		if t.Settings["type"] == "group" {
			endpoint = "https://api.dingtalk.com/v1.0/robot/groupMessages/send"
			payload["openConversationId"] = t.Address
		} else {
			endpoint = "https://api.dingtalk.com/v1.0/robot/oToMessages/batchSend"
			payload["userIds"] = []string{t.Address}
		}
		body = payload
	case "qq":
		token, err := s.accessToken(ctx, c, secrets)
		if err != nil {
			return Result{}, err
		}
		headers["Authorization"] = "QQBot " + token
		headers["X-Union-Appid"] = c.Settings["app_id"]
		segment := "users"
		if t.Settings["type"] == "group" {
			segment = "groups"
		}
		endpoint = "https://api.sgroup.qq.com/v2/" + segment + "/" + url.PathEscape(t.Address) + "/messages"
		body = map[string]any{"content": text, "msg_type": 0}
	case "webhook":
		endpoint = secrets["webhook"]
		body = map[string]any{"schema_version": 1, "event_type": m.Kind, "event": m.Event, "title": m.Title, "text": m.Text, "detail_url": m.DetailURL}
	default:
		return Result{}, &Failure{Class: "credentials", Permanent: true}
	}
	if err := ValidateEndpoint(c.Kind, endpoint); err != nil {
		return Result{}, err
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return Result{}, &Failure{Class: "response", Permanent: true}
	}
	if c.Kind == "webhook" && secrets["signing_secret"] != "" {
		stamp := strconv.FormatInt(time.Now().Unix(), 10)
		mac := hmac.New(sha256.New, []byte(secrets["signing_secret"]))
		_, _ = mac.Write(append([]byte(stamp+"."), raw...))
		headers["X-IKUN-Timestamp"] = stamp
		headers["X-IKUN-Signature"] = "sha256=" + hex.EncodeToString(mac.Sum(nil))
	}
	response, err := s.call(ctx, method, endpoint, headers, raw, c.Kind, previous != "")
	if err != nil {
		return Result{}, err
	}
	id := ""
	switch c.Kind {
	case "telegram":
		id = response.Get("result.message_id").String()
	case "feishu_app", "lark_app":
		id = response.Get("data.message_id").String()
	case "qq":
		id = response.Get("id").String()
	case "dingtalk":
		id = response.Get("processQueryKey").String()
	}
	if id == "" {
		id = previous
	}
	return Result{MessageID: id}, nil
}

func ValidateEndpoint(kind, raw string) error {
	if !domain.ValidHTTPS(raw) {
		return &Failure{Class: "credentials", Permanent: true}
	}
	u, _ := url.Parse(raw)
	host := strings.ToLower(u.Hostname())
	if u.Port() != "" && u.Port() != "443" && kind != "webhook" {
		return &Failure{Class: "credentials", Permanent: true}
	}
	ok := false
	switch kind {
	case "telegram":
		ok = host == "api.telegram.org"
	case "feishu_webhook":
		ok = host == "open.feishu.cn" && strings.HasPrefix(u.Path, "/open-apis/bot/v2/hook/")
	case "lark_webhook":
		ok = host == "open.larksuite.com" && strings.HasPrefix(u.Path, "/open-apis/bot/v2/hook/")
	case "feishu_app":
		ok = host == "open.feishu.cn"
	case "lark_app":
		ok = host == "open.larksuite.com"
	case "wecom":
		ok = host == "qyapi.weixin.qq.com" && u.Path == "/cgi-bin/webhook/send" && u.Query().Get("key") != ""
	case "dingtalk":
		ok = host == "api.dingtalk.com"
	case "qq":
		ok = host == "api.sgroup.qq.com"
	case "webhook":
		ok = true
	}
	if !ok {
		return &Failure{Class: "credentials", Permanent: true}
	}
	return nil
}

func (s *Sender) call(ctx context.Context, method, endpoint string, headers map[string]string, body []byte, kind string, editing bool) (gjson.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return gjson.Result{}, &Failure{Class: "credentials", Permanent: true}
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if s.Client == nil {
		return gjson.Result{}, &Failure{Class: "network", Permanent: true}
	}
	var written atomic.Bool
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{WroteRequest: func(info httptrace.WroteRequestInfo) {
		if info.Err == nil {
			written.Store(true)
		}
	}}))
	response, err := s.Client.Do(req)
	if err != nil {
		var ne net.Error
		if written.Load() || errors.Is(err, context.DeadlineExceeded) || errors.As(err, &ne) && ne.Timeout() {
			return gjson.Result{}, &Failure{Class: "uncertain", Uncertain: true}
		}
		return gjson.Result{}, &Failure{Class: "network"}
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return gjson.Result{}, &Failure{Class: "uncertain", Uncertain: true}
	}
	r := gjson.ParseBytes(raw)
	if response.StatusCode == 429 {
		return r, &Failure{Class: "rate_limit", RetryAfter: retryAfter(response.Header, r)}
	}
	if response.StatusCode == 401 {
		return r, &Failure{Class: "credentials", Permanent: true}
	}
	if response.StatusCode == 403 {
		reason := classify(kind, r.Get("code").String(), r, response.StatusCode)
		if reason.Class == "credentials" || reason.Class == "platform_restricted" {
			return r, reason
		}
		return r, &Failure{Class: "permission", Permanent: true}
	}
	if response.StatusCode >= 500 {
		return r, &Failure{Class: "platform"}
	}
	if kind == "webhook" {
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			return r, nil
		}
		return r, &Failure{Class: "response", Permanent: response.StatusCode < 500}
	}
	if !gjson.ValidBytes(raw) {
		return r, &Failure{Class: "response", Permanent: true}
	}
	code := ""
	for _, key := range []string{"code", "errcode", "error_code"} {
		v := r.Get(key)
		if v.Exists() && v.String() != "0" && v.String() != "" {
			code = v.String()
			break
		}
	}
	if kind == "telegram" && r.Get("ok").Bool() && response.StatusCode < 300 {
		return r, nil
	}
	if kind == "telegram" && editing && strings.Contains(r.Get("description").String(), "message is not modified") {
		return r, nil
	}
	if code != "" || response.StatusCode >= 400 || kind == "telegram" && !r.Get("ok").Bool() {
		return r, classify(kind, code, r, response.StatusCode)
	}
	switch kind {
	case "feishu_app", "lark_app", "feishu_webhook", "lark_webhook":
		if !r.Get("code").Exists() {
			return r, &Failure{Class: "response", Permanent: true}
		}
	case "wecom":
		if !r.Get("errcode").Exists() {
			return r, &Failure{Class: "response", Permanent: true}
		}
	case "dingtalk":
		if len(r.Get("invalidUserIdList").Array()) > 0 || !r.Get("processQueryKey").Exists() {
			return r, &Failure{Class: "target", Permanent: true}
		}
	case "qq":
		if !r.Get("id").Exists() {
			return r, &Failure{Class: "response", Permanent: true}
		}
	}
	return r, nil
}

func classify(kind, code string, r gjson.Result, status int) *Failure {
	lower := strings.ToLower(r.Get("message").String() + " " + r.Get("msg").String() + " " + r.Get("errmsg").String() + " " + r.Get("description").String())
	if code == "429" || code == "99991400" || code == "99991401" || code == "45009" || strings.Contains(lower, "rate limit") || strings.Contains(lower, "throttl") {
		return &Failure{Class: "rate_limit", RetryAfter: retryAfter(nil, r)}
	}
	if code == "11244" || code == "99991663" || code == "99991661" || code == "40014" || code == "42001" || strings.Contains(lower, "token") && (strings.Contains(lower, "expired") || strings.Contains(lower, "invalid")) {
		return &Failure{Class: "credentials", Permanent: true}
	}
	if kind == "qq" && (strings.Contains(lower, "主动") || strings.Contains(lower, "proactive") || strings.Contains(lower, "interaction") || strings.Contains(lower, "msg_id") || strings.Contains(lower, "expire")) {
		return &Failure{Class: "platform_restricted", Permanent: true}
	}
	if strings.Contains(lower, "permission") || strings.Contains(lower, "forbidden") || strings.Contains(lower, "not in") || strings.Contains(lower, "权限") {
		return &Failure{Class: "permission", Permanent: true}
	}
	if status >= 400 && status < 500 || strings.Contains(lower, "invalid") || strings.Contains(lower, "not found") {
		return &Failure{Class: "target", Permanent: true}
	}
	return &Failure{Class: "response", Permanent: true}
}

func retryAfter(header http.Header, r gjson.Result) time.Duration {
	seconds := r.Get("parameters.retry_after").Int()
	if seconds < 1 && header != nil {
		seconds, _ = strconv.ParseInt(header.Get("Retry-After"), 10, 64)
		if seconds < 1 {
			if at, e := http.ParseTime(header.Get("Retry-After")); e == nil {
				seconds = int64(time.Until(at).Seconds())
			}
		}
	}
	if seconds < 1 {
		seconds = 60
	}
	if seconds > 86400 {
		seconds = 86400
	}
	return time.Duration(seconds) * time.Second
}

func feishuCard(m Message) map[string]any {
	color := "orange"
	if m.Event.Severity == "P0" {
		color = "red"
	}
	if m.Event.Status == "resolved" {
		color = "green"
	}
	text := m.Text
	if m.MentionAll {
		text += "\n<at id=all></at>"
	}
	elements := []any{map[string]any{"tag": "markdown", "content": text}}
	if m.DetailURL != "" {
		elements = append(elements, map[string]any{"tag": "action", "actions": []any{map[string]any{"tag": "button", "text": map[string]any{"tag": "plain_text", "content": "查看事件"}, "url": m.DetailURL, "type": "default"}}})
	}
	return map[string]any{"config": map[string]bool{"wide_screen_mode": true}, "header": map[string]any{"template": color, "title": map[string]string{"tag": "plain_text", "content": m.Title}}, "elements": elements}
}

func (s *Sender) accessToken(ctx context.Context, c domain.Channel, secrets map[string]string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tokens == nil {
		s.tokens = map[string]tokenEntry{}
	}
	key := fmt.Sprintf("%s:%d", c.ID, c.Revision)
	if cached, ok := s.tokens[key]; ok && time.Now().Before(cached.expires) {
		return cached.value, nil
	}
	endpoint := ""
	body := map[string]string{}
	switch c.Kind {
	case "feishu_app", "lark_app":
		base := "https://open.feishu.cn"
		if c.Kind == "lark_app" {
			base = "https://open.larksuite.com"
		}
		endpoint = base + "/open-apis/auth/v3/tenant_access_token/internal"
		body = map[string]string{"app_id": c.Settings["app_id"], "app_secret": secrets["app_secret"]}
	case "dingtalk":
		endpoint = "https://api.dingtalk.com/v1.0/oauth2/accessToken"
		body = map[string]string{"appKey": c.Settings["app_id"], "appSecret": secrets["app_secret"]}
	case "qq":
		endpoint = "https://bots.qq.com/app/getAppAccessToken"
		body = map[string]string{"appId": c.Settings["app_id"], "clientSecret": secrets["app_secret"]}
	}
	raw, _ := json.Marshal(body)
	// 令牌接口不发送用户消息，按自身回执字段判断成功。
	r, err := s.call(ctx, http.MethodPost, endpoint, nil, raw, "webhook", false)
	if err != nil {
		var f *Failure
		if errors.As(err, &f) && f.Uncertain {
			return "", &Failure{Class: "network"}
		}
		return "", err
	}
	token := r.Get("tenant_access_token").String()
	expires := r.Get("expire").Int()
	if c.Kind == "dingtalk" {
		token = r.Get("accessToken").String()
		expires = r.Get("expireIn").Int()
	}
	if c.Kind == "qq" {
		token = r.Get("access_token").String()
		expires = r.Get("expires_in").Int()
	}
	if token == "" {
		return "", &Failure{Class: "credentials", Permanent: true}
	}
	if expires < 120 {
		expires = 120
	}
	if expires > 86400 {
		expires = 86400
	}
	if len(s.tokens) > 100 {
		s.tokens = map[string]tokenEntry{}
	}
	s.tokens[key] = tokenEntry{token, time.Now().Add(time.Duration(expires-60) * time.Second)}
	return token, nil
}
