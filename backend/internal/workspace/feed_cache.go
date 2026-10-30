package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const feedFreshFor = 60 * time.Second
const feedKeepFor = 7 * 24 * time.Hour

type cachedFeed struct {
	data      json.RawMessage
	fetchedAt time.Time
	retryAt   time.Time
}

type feedSnapshot struct {
	Data      json.RawMessage `json:"data"`
	FetchedAt time.Time       `json:"fetched_at"`
}

func feedSnapshotKey(target string) string {
	digest := sha256.Sum256([]byte(target))
	return "public_feed:v1:" + hex.EncodeToString(digest[:])
}

// 仅持久化固定首页和筛选字典，避免用户查询和分页无限增长。
func persistFeed(target string) bool {
	u, err := url.Parse(target)
	if err != nil || strings.Contains(u.Path, "/posts/") {
		return false
	}
	q := u.Query()
	if u.Hostname() == "status.openai.com" && u.Path == "/proxy/status.openai.com" && len(q) == 0 {
		return true
	}
	return len(q) == 1 && (q.Get("limit") == "20" || q.Get("limit") == "30" || q.Get("limit") == "50")
}

func (h *Handler) rememberFeed(target string, value cachedFeed) {
	h.feedMu.Lock()
	defer h.feedMu.Unlock()
	if h.feedCache == nil {
		h.feedCache = make(map[string]cachedFeed)
	}
	if _, exists := h.feedCache[target]; !exists && len(h.feedCache) >= 128 {
		var oldest string
		var at time.Time
		for key, item := range h.feedCache {
			if oldest == "" || item.fetchedAt.Before(at) {
				oldest, at = key, item.fetchedAt
			}
		}
		delete(h.feedCache, oldest)
	}
	h.feedCache[target] = value
}

func (h *Handler) cachedPublicFeed(ctx context.Context, target string) cachedFeed {
	h.feedMu.Lock()
	value, exists := h.feedCache[target]
	h.feedMu.Unlock()
	if exists || h.db == nil || !persistFeed(target) {
		return value
	}
	var snapshot feedSnapshot
	if h.getRuntime(ctx, feedSnapshotKey(target), &snapshot) == nil &&
		len(snapshot.Data) > 0 && len(snapshot.Data) <= 8<<20 && json.Valid(snapshot.Data) &&
		!snapshot.FetchedAt.IsZero() && time.Since(snapshot.FetchedAt) >= 0 && time.Since(snapshot.FetchedAt) < feedKeepFor {
		value = cachedFeed{data: snapshot.Data, fetchedAt: snapshot.FetchedAt}
		h.rememberFeed(target, value)
	}
	return value
}

func writeFeed(c *gin.Context, value cachedFeed, state string) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Workspace-Cache", state)
	c.Header("X-Workspace-Fetched-At", value.fetchedAt.UTC().Format(time.RFC3339))
	c.Data(http.StatusOK, "application/json; charset=utf-8", value.data)
}

func (h *Handler) serveFeed(c *gin.Context, target string) {
	value := h.cachedPublicFeed(c.Request.Context(), target)
	if len(value.data) > 0 && time.Since(value.fetchedAt) < feedKeepFor {
		if time.Since(value.fetchedAt) < feedFreshFor {
			writeFeed(c, value, "hit")
			return
		}
		// 外部源故障时先展示真实历史快照；更新时间保持原值，不伪装成刚刚同步。
		writeFeed(c, value, "stale")
		if time.Now().After(value.retryAt) {
			h.feedRequests.DoChan(target, func() (any, error) { return h.refreshPublicFeed(target) })
		}
		return
	}
	result := h.feedRequests.DoChan(target, func() (any, error) { return h.refreshPublicFeed(target) })
	select {
	case <-c.Request.Context().Done():
		return
	case outcome := <-result:
		if outcome.Err != nil {
			c.Header("Retry-After", "60")
			fail(c, &requestError{http.StatusServiceUnavailable, "暂时无法更新，请稍后重试。"})
			return
		}
		writeFeed(c, outcome.Val.(cachedFeed), "live")
	}
}

func (h *Handler) refreshPublicFeed(target string) (cachedFeed, error) {
	parent := h.background
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, 12*time.Second)
	defer cancel()
	previous := h.cachedPublicFeed(ctx, target)
	if time.Now().Before(previous.retryAt) {
		return previous, fmt.Errorf("feed retry deferred")
	}
	if len(previous.data) > 0 && time.Since(previous.fetchedAt) < feedFreshFor {
		return previous, nil
	}
	data, err := h.fetchPublicFeed(ctx, target)
	if err != nil {
		previous.retryAt = time.Now().Add(time.Minute)
		h.rememberFeed(target, previous)
		u, _ := url.Parse(target)
		slog.Warn("公开资讯同步未完成", "host", u.Hostname(), "path", u.Path, "reason", err.Error())
		return previous, err
	}
	value := cachedFeed{data: data, fetchedAt: time.Now()}
	h.rememberFeed(target, value)
	if h.db != nil && persistFeed(target) {
		if err = h.setRuntime(ctx, feedSnapshotKey(target), feedSnapshot{Data: data, FetchedAt: value.fetchedAt}); err != nil {
			slog.Warn("公开资讯快照保存未完成")
		}
	}
	return value, nil
}

func (h *Handler) fetchPublicFeed(ctx context.Context, target string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("request construction failed")
	}
	req.Header.Set("User-Agent", "IKUN2api/1.1")
	req.Header.Set("Accept", "application/json")
	r, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connection unavailable")
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream HTTP %d", r.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, (8<<20)+1))
	var object map[string]json.RawMessage
	if err != nil || len(raw) > 8<<20 || json.Unmarshal(raw, &object) != nil || object == nil {
		return nil, fmt.Errorf("unreadable upstream payload")
	}
	if code, ok := object["code"]; ok && string(code) != "0" {
		return nil, fmt.Errorf("upstream application error")
	}
	return raw, nil
}
