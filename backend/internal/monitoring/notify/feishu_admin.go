package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
)

// Only these two origins are reachable; the admin page picks one by name.
var feishuOrigins = map[string]string{"feishu": "https://open.feishu.cn", "lark": "https://open.larksuite.com"}

// NewFeishuForDomain builds a sender for "feishu" (China) or "lark" (international).
func NewFeishuForDomain(domainName, appID, secret string, transport http.RoundTripper) *Feishu {
	f := NewFeishuWithTransport(appID, secret, transport)
	if origin, ok := feishuOrigins[domainName]; ok {
		f.base = origin
	}
	return f
}

// AdminError carries the platform's own explanation (for example a missing
// permission scope) back to the administrator. It never contains credentials.
type AdminError struct{ Message string }

func (e *AdminError) Error() string { return e.Message }

func (f *Feishu) adminGet(ctx context.Context, path string, dest any) error {
	token, err := f.accessToken(ctx)
	if err != nil {
		return describe(err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.base+path, nil)
	if err != nil {
		return &AdminError{"请求地址无效"}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := f.client.Do(req)
	if err != nil {
		return &AdminError{"无法连接飞书开放平台，请检查服务器网络"}
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if err != nil {
		return &AdminError{"飞书返回内容读取失败"}
	}
	var head struct {
		Code *int   `json:"code"`
		Msg  string `json:"msg"`
	}
	if json.Unmarshal(raw, &head) != nil || head.Code == nil {
		return &AdminError{fmt.Sprintf("飞书返回无法解析（HTTP %d）", res.StatusCode)}
	}
	if *head.Code != 0 {
		msg := head.Msg
		if r := []rune(msg); len(r) > 300 {
			msg = string(r[:300]) + "…"
		}
		return &AdminError{fmt.Sprintf("飞书拒绝请求（错误码 %d）：%s", *head.Code, msg)}
	}
	return json.Unmarshal(raw, dest)
}

// describe turns a transport failure class into a sentence for the admin page.
func describe(err error) error {
	f, ok := err.(*Failure)
	if !ok {
		return err
	}
	switch f.Class {
	case "credentials_missing":
		return &AdminError{"尚未填写 App ID 或 App Secret"}
	case "authentication_or_permission", "platform_rejected", "invalid_token_response":
		return &AdminError{"App ID 或 App Secret 不正确，或应用尚未发布"}
	case "network_or_timeout", "platform_unavailable":
		return &AdminError{"无法连接飞书开放平台，请检查服务器网络"}
	case "rate_limited":
		return &AdminError{"飞书限流，请稍后再试"}
	}
	return &AdminError{"飞书请求失败：" + f.Class}
}

// BotInfo verifies the credentials and returns the bot's display name.
func (f *Feishu) BotInfo(ctx context.Context) (string, error) {
	var out struct {
		Bot struct {
			Name string `json:"app_name"`
		} `json:"bot"`
	}
	if err := f.adminGet(ctx, "/open-apis/bot/v3/info", &out); err != nil {
		return "", err
	}
	return out.Bot.Name, nil
}

type Chat struct {
	ID   string `json:"chat_id"`
	Name string `json:"name"`
}

// Chats lists group chats the bot has joined (needs the im:chat:readonly scope).
func (f *Feishu) Chats(ctx context.Context) ([]Chat, error) {
	chats := []Chat{}
	token := ""
	for page := 0; page < 5; page++ {
		path := "/open-apis/im/v1/chats?page_size=100"
		if token != "" {
			path += "&page_token=" + url.QueryEscape(token)
		}
		var out struct {
			Data struct {
				Items     []Chat `json:"items"`
				HasMore   bool   `json:"has_more"`
				PageToken string `json:"page_token"`
			} `json:"data"`
		}
		if err := f.adminGet(ctx, path, &out); err != nil {
			return nil, err
		}
		for _, c := range out.Data.Items {
			if platformID.MatchString(c.ID) && strings.HasPrefix(c.ID, "oc_") {
				chats = append(chats, c)
			}
		}
		if !out.Data.HasMore || out.Data.PageToken == "" {
			break
		}
		token = out.Data.PageToken
	}
	return chats, nil
}

// TestDelivery is a synthetic card/email that says it is a test.
func TestDelivery(id, channel, target string, now time.Time) domain.Delivery {
	i := domain.Incident{ID: "test-" + id, Rule: "TEST", RuleVersion: 1, Scope: "通知通道测试", Status: "firing", Severity: "P3", FirstSeen: now, LastSeen: now, Revision: 1,
		Explanation: domain.Explanation{Conclusion: "这是一条测试通知", Confidence: "high", Facts: []string{"由管理员在监控页「通知设置」发起，用于确认通道可以送达"}, Inferences: []string{}, Missing: []string{}, Advice: []string{"收到即说明通道可用，无需处理"}}}
	return domain.Delivery{ID: id, IncidentID: i.ID, Revision: 1, Channel: channel, Target: target, Kind: "firing", State: "pending", NextAttempt: now, Payload: i}
}
