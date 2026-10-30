package workspace

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/response"
	"github.com/MACOS-DO/sub4api/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

var resetPath = regexp.MustCompile(`^/(summary|posts|posts/[1-9][0-9]*/(reply-context|announcement-window))$`)

func newsPath(path string) (string, bool) {
	switch path {
	case "/latest", "/hot", "/recommended":
		return "/api/v1/news" + path, true
	case "/categories", "/tags", "/sources/rss", "/sources/x":
		return "/api/v1" + path, true
	default:
		return "", false
	}
}

func feedQuery(values url.Values) (url.Values, error) {
	clean := url.Values{}
	for _, key := range []string{"cursor", "limit", "categoryId", "tagId", "sourceId", "sourceType", "category", "keyword", "query", "q"} {
		if value := values.Get(key); value != "" {
			if len(value) > 2048 {
				return nil, badRequest("筛选条件太长，请重新选择。")
			}
			clean.Set(key, value)
		}
	}
	clean.Set("limit", strconv.Itoa(pageNumber(values.Get("limit"), 20, 50)))
	return clean, nil
}

func (h *Handler) newsFeed(c *gin.Context) {
	path, ok := newsPath(c.Param("path"))
	if !ok {
		fail(c, notFound())
		return
	}
	h.publicFeed(c, "https://news.macos.do"+path)
}
func (h *Handler) resetFeed(c *gin.Context) {
	path := c.Param("path")
	if path == "/service-status" {
		h.serveFeed(c, "https://status.openai.com/proxy/status.openai.com")
		return
	}
	if path == "/service-history" {
		start, startErr := time.Parse(time.RFC3339, c.Query("start_at"))
		end, endErr := time.Parse(time.RFC3339, c.Query("end_at"))
		if startErr != nil || endErr != nil || !end.After(start) || end.Sub(start) > 48*time.Hour || end.After(time.Now().Add(10*time.Minute)) {
			fail(c, badRequest("请选择有效的状态时间范围。"))
			return
		}
		query := url.Values{"start_at": {start.UTC().Format(time.RFC3339)}, "end_at": {end.UTC().Format(time.RFC3339)}}
		h.serveFeed(c, "https://status.openai.com/proxy/status.openai.com/component_impacts?"+query.Encode())
		return
	}
	if !resetPath.MatchString(path) {
		fail(c, notFound())
		return
	}
	h.publicFeed(c, "https://reset.macos.do/api/monitor"+path)
}

// 只请求固定公开数据源；用户的令牌、Cookie 和请求头不会转发给外站。
func (h *Handler) publicFeed(c *gin.Context, base string) {
	query, err := feedQuery(c.Request.URL.Query())
	if err != nil {
		fail(c, err)
		return
	}
	target := base + "?" + query.Encode()
	h.serveFeed(c, target)
}

func favoriteUser(c *gin.Context) (int64, bool) {
	u, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || u.UserID <= 0 {
		fail(c, &requestError{401, "请先登录。"})
		return 0, false
	}
	return u.UserID, true
}
func (h *Handler) listFavorites(c *gin.Context) {
	id, ok := favoriteUser(c)
	if !ok {
		return
	}
	rows, err := h.db.QueryContext(c.Request.Context(), "SELECT item FROM workspace_news_favorites WHERE user_id=$1 ORDER BY created_at DESC LIMIT 100", id)
	if err != nil {
		fail(c, err)
		return
	}
	defer rows.Close()
	items := []json.RawMessage{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			fail(c, err)
			return
		}
		items = append(items, json.RawMessage(raw))
	}
	if err = rows.Err(); err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"items": items, "hasMore": false})
}
func (h *Handler) saveFavorite(c *gin.Context) {
	user, ok := favoriteUser(c)
	if !ok {
		return
	}
	id, err := positiveID(c)
	if err != nil {
		fail(c, err)
		return
	}
	var item map[string]any
	if err = body(c, &item, 128<<10); err != nil {
		fail(c, err)
		return
	}
	title, _ := item["title"].(string)
	if strings.TrimSpace(title) == "" {
		fail(c, badRequest("请先加载新闻再收藏。"))
		return
	}
	// 收藏只保存展示字段，不接纳任意用户属性或可执行的 HTML。
	clean := map[string]any{"id": id, "favorited": true}
	for _, key := range []string{"title", "summary", "category", "tags", "publishedAt", "systemPublishedAt", "updatedAt", "xPost", "source", "latestId", "hotId", "recommendationId"} {
		if value, exists := item[key]; exists {
			clean[key] = value
		}
	}
	raw, err := json.Marshal(clean)
	if err != nil {
		fail(c, err)
		return
	}
	tx, err := h.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		fail(c, err)
		return
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(c.Request.Context(), "INSERT INTO workspace_news_favorites(user_id,news_id,item) VALUES($1,$2,$3) ON CONFLICT(user_id,news_id) DO UPDATE SET item=EXCLUDED.item", user, id, string(raw))
	if err == nil {
		_, err = tx.ExecContext(c.Request.Context(), "DELETE FROM workspace_news_favorites WHERE user_id=$1 AND news_id IN (SELECT news_id FROM workspace_news_favorites WHERE user_id=$1 ORDER BY created_at DESC OFFSET 100)", user)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"saved": true})
}
func (h *Handler) removeFavorite(c *gin.Context) {
	user, ok := favoriteUser(c)
	if !ok {
		return
	}
	id, err := positiveID(c)
	if err != nil {
		fail(c, err)
		return
	}
	_, err = h.db.ExecContext(c.Request.Context(), "DELETE FROM workspace_news_favorites WHERE user_id=$1 AND news_id=$2", user, id)
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"removed": true})
}
