package workspace

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

type navigationStatus struct {
	Tickets       int                 `json:"tickets"`
	CheckinNeeded bool                `json:"checkin_needed"`
	PendingWins   int                 `json:"pending_wins"`
	UnopenedBoxes int                 `json:"unopened_boxes"`
	Activities    []string            `json:"activities"`
	News          []string            `json:"news,omitempty"`
	Reset         []string            `json:"reset,omitempty"`
	Seen          map[string][]string `json:"seen"`
}

const navigationTicketsSQL = `SELECT count(*) FROM workspace_ticket_messages m JOIN workspace_tickets t ON t.id=m.ticket_id
	WHERE ($1 OR t.user_id=$2) AND m.kind='message' AND m.author_role<>$3
	AND m.created_at>COALESCE(CASE WHEN $1 THEN t.admin_last_read_at ELSE t.user_last_read_at END,'epoch'::timestamptz)`

func navigationActivityKey(id int64, status string) string {
	return status + ":" + strconv.FormatInt(id, 10)
}

func (h *Handler) navigation(c *gin.Context) {
	a, err := h.identity(c)
	if err != nil {
		fail(c, err)
		return
	}
	ctx := c.Request.Context()
	result := navigationStatus{Activities: []string{}, Seen: map[string][]string{}}
	if err = h.db.QueryRowContext(ctx, navigationTicketsSQL, a.Admin, a.ID, actorRole(a)).Scan(&result.Tickets); err != nil {
		fail(c, err)
		return
	}
	if !a.Admin {
		err = h.db.QueryRowContext(ctx, `SELECT s.enabled AND NOT EXISTS(SELECT 1 FROM workspace_checkins WHERE user_id=$1 AND checkin_day=(statement_timestamp() AT TIME ZONE 'Asia/Shanghai')::date),
			(SELECT count(*) FROM workspace_lottery_winners WHERE user_id=$1 AND claimed_at IS NULL),
			(SELECT count(*) FROM workspace_blindbox_entries e JOIN workspace_blindbox_rounds r ON r.id=e.round_id WHERE e.user_id=$1 AND r.status='settled' AND e.opened_at IS NULL)
			FROM workspace_checkin_settings s WHERE s.id=1`, a.ID).Scan(&result.CheckinNeeded, &result.PendingWins, &result.UnopenedBoxes)
		if err != nil {
			fail(c, err)
			return
		}
		// 资讯由所有用户共享已有缓存及单次刷新；提醒请求不会为每个用户单独访问外站。
		result.News = h.navigationFeed(ctx, "https://news.macos.do/api/v1/news/latest?limit=30")
		result.Reset = h.navigationFeed(ctx, "https://reset.macos.do/api/monitor/posts?limit=20")
	}
	rows, err := h.db.QueryContext(ctx, `SELECT l.id,l.status FROM workspace_lotteries l WHERE l.status IN ('open','drawn')
		AND ($1 OR (l.status='open' AND (l.spec->>'start_at')::timestamptz<=statement_timestamp()) OR EXISTS(SELECT 1 FROM workspace_lottery_entries e WHERE e.lottery_id=l.id AND e.user_id=$2)) ORDER BY l.id DESC LIMIT 100`, a.Admin, a.ID)
	if err != nil {
		fail(c, err)
		return
	}
	for rows.Next() {
		var id int64
		var status string
		if err = rows.Scan(&id, &status); err != nil {
			break
		}
		result.Activities = append(result.Activities, navigationActivityKey(id, status))
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		fail(c, err)
		return
	}
	result.Seen, err = h.navigationReads(ctx, a.ID)
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, result)
}

func (h *Handler) navigationReads(ctx context.Context, userID int64) (map[string][]string, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT section,seen_keys FROM workspace_navigation_reads WHERE user_id=$1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string][]string{}
	for rows.Next() {
		var section string
		var raw []byte
		var keys []string
		if err = rows.Scan(&section, &raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &keys); err != nil {
			return nil, err
		}
		seen[section] = keys
	}
	return seen, rows.Err()
}

func (h *Handler) navigationFeed(ctx context.Context, target string) []string {
	value := h.cachedPublicFeed(ctx, target)
	if time.Since(value.fetchedAt) >= feedFreshFor && time.Now().After(value.retryAt) {
		h.feedRequests.DoChan(target, func() (any, error) { return h.refreshPublicFeed(target) })
	}
	return navigationFeedKeys(value.data)
}

func navigationFeedKeys(raw []byte) []string {
	var envelope struct {
		Data  json.RawMessage `json:"data"`
		Items []struct {
			ID json.Number `json:"id"`
		} `json:"items"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return nil
	}
	if len(envelope.Data) > 0 {
		return navigationFeedKeys(envelope.Data)
	}
	keys := []string{}
	for _, item := range envelope.Items {
		if id, err := item.ID.Int64(); err == nil && id > 0 {
			keys = append(keys, strconv.FormatInt(id, 10))
		}
		if len(keys) >= 50 {
			break
		}
	}
	return keys
}

type navigationReadPayload struct {
	Section string   `json:"section"`
	Keys    []string `json:"keys"`
}

func validateNavigationRead(p navigationReadPayload, admin bool) error {
	switch p.Section {
	case "news", "reset", "models", "lottery", "pages":
	case "admin_lottery":
		if !admin {
			return notFound()
		}
	default:
		return badRequest("提醒类别无法识别。")
	}
	if len(p.Keys) == 0 || len(p.Keys) > 100 {
		return badRequest("每次最多标记 100 条内容。")
	}
	for _, key := range p.Keys {
		if strings.TrimSpace(key) == "" || len(key) > 256 || strings.ContainsAny(key, "\x00\r\n") {
			return badRequest("内容编号无法识别。")
		}
	}
	return nil
}

func mergeNavigationKeys(previous, next []string) []string {
	// 保留最近查看的键，限定每个分类的存储量；重复访问不会无限增长。
	combined := append(append([]string{}, previous...), next...)
	seen := map[string]bool{}
	result := []string{}
	for i := len(combined) - 1; i >= 0 && len(result) < 4096; i-- {
		if !seen[combined[i]] {
			seen[combined[i]] = true
			result = append(result, combined[i])
		}
	}
	for left, right := 0, len(result)-1; left < right; left, right = left+1, right-1 {
		result[left], result[right] = result[right], result[left]
	}
	return result
}

func (h *Handler) readNavigation(c *gin.Context) {
	a, err := h.identity(c)
	if err != nil {
		fail(c, err)
		return
	}
	var payload navigationReadPayload
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&payload); err != nil {
		fail(c, badRequest("提醒内容无法读取。"))
		return
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		fail(c, badRequest("请只提交一次已读记录。"))
		return
	}
	// 用户标记只影响自己的提醒，不改工单、公告、签到或奖金的真实状态。
	if err = validateNavigationRead(payload, a.Admin); err != nil {
		fail(c, err)
		return
	}
	keys, err := h.saveNavigationRead(c.Request.Context(), a.ID, payload)
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"keys": keys})
}

func (h *Handler) saveNavigationRead(ctx context.Context, userID int64, payload navigationReadPayload) ([]string, error) {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO workspace_navigation_reads(user_id,section) VALUES($1,$2) ON CONFLICT DO NOTHING`, userID, payload.Section); err != nil {
		return nil, err
	}
	var raw []byte
	if err = tx.QueryRowContext(ctx, `SELECT seen_keys FROM workspace_navigation_reads WHERE user_id=$1 AND section=$2 FOR UPDATE`, userID, payload.Section).Scan(&raw); err != nil {
		return nil, err
	}
	var previous []string
	if err = json.Unmarshal(raw, &previous); err != nil {
		return nil, err
	}
	keys := mergeNavigationKeys(previous, payload.Keys)
	raw, err = json.Marshal(keys)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE workspace_navigation_reads SET seen_keys=$3::jsonb,updated_at=clock_timestamp() WHERE user_id=$1 AND section=$2`, userID, payload.Section, string(raw)); err != nil {
		return nil, err
	}
	return keys, tx.Commit()
}
