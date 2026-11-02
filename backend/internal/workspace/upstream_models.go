package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode"
)

const modelCatalogLimit = 5000

type upstreamModelConfig struct {
	Enabled         bool     `json:"enabled"`
	IntervalMinutes int      `json:"interval_minutes"`
	Notify          bool     `json:"notify"`
	Recipients      []string `json:"recipients"`
}

func defaultModelConfig() upstreamModelConfig {
	return upstreamModelConfig{IntervalMinutes: 30, Recipients: []string{}}
}
func normalizeModelConfig(c *upstreamModelConfig) error {
	b := defaultUpstreamBalanceConfig()
	b.IntervalMinutes = c.IntervalMinutes
	b.Recipients = c.Recipients
	if err := normalizeUpstreamBalanceConfig(&b); err != nil {
		return err
	}
	c.Recipients = b.Recipients
	if c.Notify && len(c.Recipients) == 0 {
		return badRequest("启用邮件提醒时请填写收件邮箱。")
	}
	return nil
}

type upstreamModelAccountState struct {
	AccountID      int64      `json:"account_id"`
	Source         string     `json:"source,omitempty"` // stored only; never returned to the browser
	Baseline       []string   `json:"baseline"`
	Current        []string   `json:"current"`
	IgnoredAdded   []string   `json:"ignored_added"`
	IgnoredRemoved []string   `json:"ignored_removed"`
	CheckedAt      *time.Time `json:"checked_at"`
	Error          string     `json:"error,omitempty"`
}
type upstreamModelChange struct {
	AccountID int64    `json:"account_id"`
	Added     []string `json:"added"`
	Removed   []string `json:"removed"`
}
type upstreamModelHistory struct {
	At      time.Time             `json:"at"`
	Action  string                `json:"action"`
	ActorID int64                 `json:"actor_id,omitempty"`
	Changes []upstreamModelChange `json:"changes"`
}
type upstreamModelNotice struct {
	Delivered map[string][]string `json:"delivered"`
	Digest    string              `json:"digest"`
	Sent      []string            `json:"sent"`
	Attempt   *time.Time          `json:"attempt"`
	Error     string              `json:"error"`
}
type upstreamModelSummary struct {
	Enabled   bool       `json:"enabled"`
	Pending   int        `json:"pending"`
	Errors    int        `json:"errors"`
	Queued    bool       `json:"queued"`
	Checking  bool       `json:"checking"`
	CheckedAt *time.Time `json:"checked_at"`
}
type upstreamModelRecord struct {
	Config            upstreamModelConfig         `json:"config"`
	Accounts          []upstreamModelAccountState `json:"accounts"`
	History           []upstreamModelHistory      `json:"history"`
	Revision          int64                       `json:"revision"`
	Summary           upstreamModelSummary        `json:"summary"`
	NotificationError string                      `json:"notification_error"`
	notice            upstreamModelNotice
	next              time.Time
	lease             *time.Time
}

func modelSource(a upstreamBalanceAccount) string {
	p := ""
	if a.proxyID != nil {
		p = fmt.Sprint(*a.proxyID)
	}
	return digestString(fmt.Sprintf("%d:%s:%s:%s", a.ID, a.key, a.Platform, p))
}
func validModelID(s string) bool {
	if s == "" || len(s) > 256 || strings.TrimSpace(s) != s || strings.ContainsAny(s, "*?") {
		return false
	}
	for _, c := range s {
		if unicode.IsControl(c) || unicode.IsSpace(c) {
			return false
		}
	}
	return true
}
func sortedModelSet(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range values {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
func modelDifference(left, right []string) []string {
	seen := map[string]bool{}
	for _, v := range right {
		seen[v] = true
	}
	out := []string{}
	for _, v := range left {
		if !seen[v] {
			out = append(out, v)
		}
	}
	return sortedModelSet(out)
}
func modelIntersection(left, right []string) []string {
	return modelDifference(left, modelDifference(left, right))
}
func pendingModelChange(a upstreamModelAccountState) upstreamModelChange {
	return upstreamModelChange{AccountID: a.AccountID, Added: modelDifference(modelDifference(a.Current, a.Baseline), a.IgnoredAdded), Removed: modelDifference(modelDifference(a.Baseline, a.Current), a.IgnoredRemoved)}
}
func observeModelCatalog(old upstreamModelAccountState, a upstreamBalanceAccount, models []string, err error, now time.Time) upstreamModelAccountState {
	if old.Source != modelSource(a) {
		old = upstreamModelAccountState{AccountID: a.ID, Source: modelSource(a), IgnoredAdded: []string{}, IgnoredRemoved: []string{}}
	}
	if err != nil {
		old.Error = err.Error()
		return old
	} // Failed/partial responses never become removals.
	models = sortedModelSet(models)
	if old.CheckedAt == nil {
		old.Baseline = append([]string{}, models...)
	}
	old.Current = models
	old.CheckedAt = &now
	old.Error = ""
	old.IgnoredAdded = modelIntersection(old.IgnoredAdded, modelDifference(old.Current, old.Baseline))
	old.IgnoredRemoved = modelIntersection(old.IgnoredRemoved, modelDifference(old.Baseline, old.Current))
	return old
}
func modelChanges(states []upstreamModelAccountState) []upstreamModelChange {
	out := []upstreamModelChange{}
	for _, s := range states {
		d := pendingModelChange(s)
		if len(d.Added)+len(d.Removed) > 0 {
			out = append(out, d)
		}
	}
	return out
}
func modelChangesDigest(states []upstreamModelAccountState) string {
	b, _ := json.Marshal(modelChanges(states))
	return digestString(string(b))
}
func addModelHistory(r *upstreamModelRecord, action string, actor int64, changes []upstreamModelChange) {
	r.History = append([]upstreamModelHistory{{At: time.Now().UTC(), Action: action, ActorID: actor, Changes: changes}}, r.History...)
	if len(r.History) > 50 {
		r.History = r.History[:50]
	}
}
func queryUpstreamModels(ctx context.Context, client *http.Client, root, key string) ([]string, error) {
	if key == "" || validateWalletToken(key) != nil {
		return nil, badRequest("账号未配置有效的 API Key。")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, root+"/v1/models", nil)
	if err != nil {
		return nil, badRequest("无法创建模型查询请求。")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "IKUN2api-ModelMonitor/1")
	guarded := *client
	guarded.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	res, err := guarded.Do(req)
	if err != nil {
		return nil, badRequest("模型查询连接失败或超时，请检查地址和代理。")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, badRequest(fmt.Sprintf("模型目录返回 HTTP %d，已保留上次结果。", res.StatusCode))
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, (2<<20)+1))
	if err != nil || len(raw) > 2<<20 {
		return nil, badRequest("模型目录无法读取或超过大小限制。")
	}
	var payload struct {
		Data *[]struct {
			ID string `json:"id"`
		} `json:"data"`
		HasMore   bool            `json:"has_more"`
		Next      string          `json:"next_page_token"`
		NextCamel string          `json:"nextPageToken"`
		Error     json.RawMessage `json:"error"`
		Success   *bool           `json:"success"`
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	if dec.Decode(&payload) != nil || dec.Decode(new(any)) != io.EOF || payload.Data == nil || payload.HasMore || payload.Next != "" || payload.NextCamel != "" || (payload.Success != nil && !*payload.Success) || (len(payload.Error) > 0 && string(payload.Error) != "null") {
		return nil, badRequest("上游未返回完整的 /v1/models 目录，已保留上次结果。")
	}
	if len(*payload.Data) == 0 || len(*payload.Data) > modelCatalogLimit {
		return nil, badRequest("模型目录为空或超过数量限制，已保留上次结果。")
	}
	out := []string{}
	for _, m := range *payload.Data {
		if !validModelID(m.ID) {
			return nil, badRequest("模型目录含无效名称，已保留上次结果。")
		}
		out = append(out, m.ID)
	}
	return sortedModelSet(out), nil
}

// Only selected identity mappings change. Custom aliases and wildcard rules are
// never silently removed; empty mappings are unrestricted and need explicit consent.
func applyModelMapping(raw any, state upstreamModelAccountState, change upstreamModelChange, initialize bool) (map[string]string, error) {
	mapping := map[string]string{}
	switch v := raw.(type) {
	case nil:
	case map[string]string:
		for k, vv := range v {
			mapping[k] = vv
		}
	case map[string]any:
		for k, vv := range v {
			s, ok := vv.(string)
			if !ok {
				return nil, badRequest("现有模型映射格式不支持，请到账号管理检查。")
			}
			mapping[k] = s
		}
	default:
		return nil, badRequest("现有模型映射格式不支持，请到账号管理检查。")
	}
	if len(mapping) == 0 {
		if !initialize {
			return nil, badRequest("账号当前允许所有模型，请确认按检测基线建立模型名单后再应用。")
		}
		for _, id := range state.Baseline {
			mapping[id] = id
		}
	}
	for _, id := range change.Added {
		if target, exists := mapping[id]; exists && target != id {
			return nil, badRequest("所选模型已有自定义别名，请到账号管理调整。")
		}
		mapping[id] = id
	}
	for _, id := range change.Removed {
		for name, target := range mapping {
			if strings.ContainsAny(name, "*?") {
				return nil, badRequest("账号包含通配符映射，请到账号管理处理下架模型。")
			}
			if target == id && name != id {
				return nil, badRequest("下架模型仍有自定义别名，请到账号管理调整。")
			}
		}
		if target, exists := mapping[id]; exists && target != id {
			return nil, badRequest("所选模型已有自定义别名，请到账号管理调整。")
		}
		delete(mapping, id)
	}
	if len(mapping) == 0 {
		return nil, badRequest("不能清空模型名单；空名单会允许所有模型，请到账号管理停用该账号。")
	}
	return mapping, nil
}
