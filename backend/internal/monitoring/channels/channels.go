// Package channels holds the notification channel credentials (Feishu app,
// SMTP account) that administrators set on the monitor page. Secrets are
// AES-GCM encrypted in the monitor's own SQLite state and never returned by the
// API. Until a page configuration is saved, the env-file values stay in effect.
package channels

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/mail"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/notify"
	"github.com/MACOS-DO/sub4api/internal/monitoring/store"
)

const stateKey = "notification_channels"

type Feishu struct {
	Domain    string `json:"domain"` // "feishu" (China) or "lark"
	AppID     string `json:"app_id"`
	AppSecret string `json:"app_secret,omitempty"`
}

type Email struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Security string `json:"security"` // "ssl" (implicit TLS) or "starttls"
	Username string `json:"username"`
	Password string `json:"password,omitempty"`
	From     string `json:"from"`
}

type Settings struct {
	Feishu Feishu `json:"feishu"`
	Email  Email  `json:"email"`
}

func (f Feishu) Configured() bool { return f.AppID != "" && f.AppSecret != "" }
func (e Email) Configured() bool  { return e.Host != "" && e.Port > 0 && e.From != "" }

// FromEnv reads the legacy monitor.env transport settings.
func FromEnv() Settings {
	s := Settings{Feishu: Feishu{Domain: "feishu", AppID: os.Getenv("OPS_FEISHU_APP_ID"), AppSecret: os.Getenv("OPS_FEISHU_APP_SECRET")}}
	if host, port, err := net.SplitHostPort(os.Getenv("OPS_SMTP_ADDRESS")); err == nil {
		p, _ := strconv.Atoi(port)
		security := "starttls"
		if os.Getenv("OPS_SMTP_IMPLICIT_TLS") == "true" {
			security = "ssl"
		}
		s.Email = Email{Host: host, Port: p, Security: security, Username: os.Getenv("OPS_SMTP_USERNAME"), Password: os.Getenv("OPS_SMTP_PASSWORD"), From: os.Getenv("OPS_SMTP_FROM")}
	}
	return s
}

// stored is the persisted form; secrets are sealed separately.
type stored struct {
	Feishu       Feishu    `json:"feishu"`
	Email        Email     `json:"email"`
	FeishuSecret string    `json:"feishu_secret_sealed,omitempty"`
	EmailSecret  string    `json:"email_secret_sealed,omitempty"`
	UpdatedBy    string    `json:"updated_by"`
	UpdatedAt    time.Time `json:"updated_at"`
	Saved        bool      `json:"saved"`
}

type Registry struct {
	Store     *store.Store
	Env       Settings
	Transport http.RoundTripper // tests only
	key       []byte

	mu      sync.Mutex
	loaded  string // JSON of the settings the cached senders were built from
	current Settings
	feishu  *notify.Feishu
	smtp    *notify.SMTP
}

// Open loads (or creates) the 32-byte key at keyPath with mode 0600.
func Open(s *store.Store, keyPath string, env Settings) (*Registry, error) {
	key, err := loadKey(keyPath)
	if err != nil {
		return nil, err
	}
	return &Registry{Store: s, Env: env, key: key}, nil
}

func loadKey(path string) ([]byte, error) {
	if raw, err := os.ReadFile(path); err == nil {
		key, err := hex.DecodeString(strings.TrimSpace(string(raw)))
		if err != nil || len(key) != 32 {
			return nil, errors.New("channel key file is corrupt")
		}
		return key, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err = f.WriteString(hex.EncodeToString(key)); err != nil {
		return nil, err
	}
	return key, f.Sync()
}

func (r *Registry) seal(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	block, err := aes.NewCipher(r.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plain), nil)), nil
}

func (r *Registry) open(sealed string) (string, error) {
	if sealed == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(r.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(raw) < gcm.NonceSize() {
		return "", errors.New("sealed secret is corrupt")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return "", errors.New("sealed secret cannot be decrypted")
	}
	return string(plain), nil
}

// Load returns effective settings and their source: "page", "env" or "none".
func (r *Registry) Load(ctx context.Context) (Settings, string, error) {
	var st stored
	if err := r.Store.State(ctx, stateKey, &st); err != nil {
		return Settings{}, "", err
	}
	if !st.Saved {
		src := "none"
		if r.Env.Feishu.Configured() || r.Env.Email.Configured() {
			src = "env"
		}
		return r.Env, src, nil
	}
	s := Settings{Feishu: st.Feishu, Email: st.Email}
	var err error
	if s.Feishu.AppSecret, err = r.open(st.FeishuSecret); err != nil {
		return Settings{}, "", err
	}
	if s.Email.Password, err = r.open(st.EmailSecret); err != nil {
		return Settings{}, "", err
	}
	return s, "page", nil
}

// Transports reports which channels have usable credentials.
func (r *Registry) Transports(ctx context.Context) map[string]bool {
	s, _, err := r.Load(ctx)
	if err != nil {
		return map[string]bool{"feishu": false, "email": false}
	}
	return map[string]bool{"feishu": s.Feishu.Configured(), "email": s.Email.Configured()}
}

// View is the masked form for the admin page.
type View struct {
	Source    string    `json:"source"`
	Feishu    Feishu    `json:"feishu"`
	FeishuSet bool      `json:"feishu_secret_set"`
	Email     Email     `json:"email"`
	EmailSet  bool      `json:"email_password_set"`
	UpdatedBy string    `json:"updated_by,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (r *Registry) View(ctx context.Context) (View, error) {
	s, src, err := r.Load(ctx)
	if err != nil {
		return View{}, err
	}
	v := View{Source: src, Feishu: s.Feishu, FeishuSet: s.Feishu.AppSecret != "", Email: s.Email, EmailSet: s.Email.Password != ""}
	v.Feishu.AppSecret, v.Email.Password = "", ""
	if v.Feishu.Domain == "" {
		v.Feishu.Domain = "feishu"
	}
	var st stored
	if err = r.Store.State(ctx, stateKey, &st); err == nil && st.Saved {
		v.UpdatedBy, v.UpdatedAt = st.UpdatedBy, st.UpdatedAt
	}
	return v, nil
}

var appIDPattern = regexp.MustCompile(`^cli_[a-zA-Z0-9]{8,64}$`)

func clean(s string) string { return strings.TrimSpace(s) }

// Merge applies an update on top of the effective settings: a blank secret
// keeps the stored one; a blank App ID / host removes that channel.
func (r *Registry) Merge(ctx context.Context, in Settings) (Settings, error) {
	cur, _, err := r.Load(ctx)
	if err != nil {
		return Settings{}, err
	}
	out := Settings{Feishu: Feishu{Domain: clean(in.Feishu.Domain), AppID: clean(in.Feishu.AppID), AppSecret: clean(in.Feishu.AppSecret)}, Email: Email{Host: clean(in.Email.Host), Port: in.Email.Port, Security: clean(in.Email.Security), Username: clean(in.Email.Username), Password: in.Email.Password, From: clean(in.Email.From)}}
	if out.Feishu.Domain == "" {
		out.Feishu.Domain = "feishu"
	}
	if out.Feishu.AppID == "" {
		out.Feishu = Feishu{Domain: out.Feishu.Domain}
	} else if out.Feishu.AppSecret == "" && out.Feishu.AppID == cur.Feishu.AppID {
		out.Feishu.AppSecret = cur.Feishu.AppSecret
	}
	if out.Email.Host == "" {
		out.Email = Email{}
	} else if out.Email.Password == "" && out.Email.Host == cur.Email.Host && out.Email.Username == cur.Email.Username {
		out.Email.Password = cur.Email.Password
	}
	return out, Validate(out)
}

func Validate(s Settings) error {
	if s.Feishu.Domain != "feishu" && s.Feishu.Domain != "lark" {
		return errors.New("飞书版本只能是国内版或国际版")
	}
	if s.Feishu.AppID != "" {
		if !appIDPattern.MatchString(s.Feishu.AppID) {
			return errors.New("App ID 格式不正确，应以 cli_ 开头")
		}
		if s.Feishu.AppSecret == "" || len(s.Feishu.AppSecret) > 128 || strings.ContainsAny(s.Feishu.AppSecret, " \r\n\t") {
			return errors.New("请填写正确的 App Secret")
		}
	}
	e := s.Email
	if e.Host == "" {
		return nil
	}
	if len(e.Host) > 253 || strings.ContainsAny(e.Host, " /:\r\n@") {
		return errors.New("SMTP 服务器地址不正确")
	}
	if e.Port < 1 || e.Port > 65535 {
		return errors.New("SMTP 端口不正确")
	}
	if e.Security != "ssl" && e.Security != "starttls" {
		return errors.New("加密方式只能是 SSL 或 STARTTLS")
	}
	if a, err := mail.ParseAddress(e.From); err != nil || strings.ContainsAny(e.From, "\r\n") || a.Address == "" {
		return errors.New("发件人邮箱格式不正确")
	}
	if len(e.Username) > 254 || len(e.Password) > 256 || strings.ContainsAny(e.Username+e.Password, "\r\n") {
		return errors.New("SMTP 账号或授权码不正确")
	}
	return nil
}

// Save stores validated settings; secrets are sealed with the registry key.
func (r *Registry) Save(ctx context.Context, s Settings, actor string, now time.Time) error {
	if err := Validate(s); err != nil {
		return err
	}
	st := stored{Feishu: s.Feishu, Email: s.Email, UpdatedBy: actor, UpdatedAt: now, Saved: true}
	var err error
	if st.FeishuSecret, err = r.seal(s.Feishu.AppSecret); err != nil {
		return err
	}
	if st.EmailSecret, err = r.seal(s.Email.Password); err != nil {
		return err
	}
	st.Feishu.AppSecret, st.Email.Password = "", ""
	return r.Store.SaveState(ctx, stateKey, st)
}

// FeishuClient builds a one-off client for settings (used to verify before saving).
func (r *Registry) FeishuClient(s Feishu) *notify.Feishu {
	return notify.NewFeishuForDomain(s.Domain, s.AppID, s.AppSecret, r.Transport)
}

func SMTPFor(e Email) *notify.SMTP {
	return &notify.SMTP{Address: net.JoinHostPort(e.Host, strconv.Itoa(e.Port)), Username: e.Username, Password: e.Password, From: e.From, ImplicitTLS: e.Security == "ssl"}
}

// senders returns cached senders for the current settings, rebuilding them
// (and dropping the cached Feishu token) whenever the settings change.
func (r *Registry) senders(ctx context.Context) (*notify.Feishu, *notify.SMTP, error) {
	s, _, err := r.Load(ctx)
	if err != nil {
		return nil, nil, err
	}
	sig, _ := json.Marshal(s)
	r.mu.Lock()
	defer r.mu.Unlock()
	if string(sig) != r.loaded || r.feishu == nil {
		r.loaded, r.current = string(sig), s
		r.feishu = r.FeishuClient(s.Feishu)
		r.smtp = SMTPFor(s.Email)
	}
	return r.feishu, r.smtp, nil
}

// Sender is a notify.Sender that always uses the latest saved credentials.
type Sender struct {
	Registry *Registry
	Channel  string
}

func (s Sender) Send(ctx context.Context, d domain.Delivery, messageID, base string) (string, error) {
	f, m, err := s.Registry.senders(ctx)
	if err != nil {
		return "", &notify.Failure{Class: "channel_settings_unreadable"}
	}
	switch s.Channel {
	case "feishu":
		return f.Send(ctx, d, messageID, base)
	case "email":
		if m.Address == "" || m.From == "" {
			return "", &notify.Failure{Class: "channel_not_configured", Permanent: true}
		}
		return m.Send(ctx, d, messageID, base)
	}
	return "", &notify.Failure{Class: "channel_not_configured", Permanent: true}
}

// FeishuNow returns a client for the effective Feishu settings.
func (r *Registry) FeishuNow(ctx context.Context) (*notify.Feishu, Settings, error) {
	f, _, err := r.senders(ctx)
	r.mu.Lock()
	cur := r.current
	r.mu.Unlock()
	return f, cur, err
}

// Describe hides internal failure classes behind short Chinese sentences.
func Describe(err error) string {
	var a *notify.AdminError
	if errors.As(err, &a) {
		return a.Message
	}
	var f *notify.Failure
	if errors.As(err, &f) {
		switch f.Class {
		case "recipient_invalid":
			return "收件目标格式不正确"
		case "credentials_missing", "channel_not_configured":
			return "通道尚未配置"
		case "authentication_or_permission":
			return "飞书拒绝发送：请确认应用已发布、开启机器人能力并有发消息权限，机器人已加入该群"
		case "platform_rejected":
			return "飞书拒绝发送：请确认机器人已加入该群，或私聊对象在应用可用范围内"
		case "network_or_timeout", "platform_unavailable":
			return "无法连接飞书开放平台"
		}
		return "发送失败：" + f.Class
	}
	return fmt.Sprint("发送失败：", err)
}
