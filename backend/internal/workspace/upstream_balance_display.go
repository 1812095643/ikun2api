package workspace

import (
	"strconv"

	"github.com/MACOS-DO/sub4api/internal/pkg/response"
	"github.com/MACOS-DO/sub4api/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

// Display preferences are scoped to the authenticated administrator, separate
// from credentials and monitoring settings. Hiding a row never disables a task.
type upstreamBalanceDisplay struct {
	Order         []string `json:"order"`
	Hidden        []string `json:"hidden"`
	HiddenColumns []string `json:"hidden_columns"`
	Compact       bool     `json:"compact"`
	SortBy        string   `json:"sort_by"`
}

func defaultUpstreamBalanceDisplay() upstreamBalanceDisplay {
	return upstreamBalanceDisplay{Order: []string{}, Hidden: []string{}, HiddenColumns: []string{}, Compact: true, SortBy: "manual"}
}

func upstreamBalanceDisplayKey(c *gin.Context) (string, error) {
	if !c.GetBool("workspace_admin") {
		return "", notFound()
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		return "", &requestError{401, "请先登录。"}
	}
	return "upstream_balance_display:" + strconv.FormatInt(subject.UserID, 10), nil
}

func normalizeUpstreamBalanceDisplay(d *upstreamBalanceDisplay) error {
	if len(d.Order) > 5000 || len(d.Hidden) > 5000 || len(d.HiddenColumns) > 4 {
		return badRequest("列表设置过多，请刷新后重试。")
	}
	for _, list := range []*[]string{&d.Order, &d.Hidden} {
		result := []string{}
		seen := map[string]bool{}
		for _, id := range *list {
			if len(id) != 64 {
				return badRequest("上游编号无效。")
			}
			for _, ch := range id {
				if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
					return badRequest("上游编号无效。")
				}
			}
			if !seen[id] {
				result = append(result, id)
				seen[id] = true
			}
		}
		*list = result
	}
	columns := []string{}
	seen := map[string]bool{}
	for _, key := range d.HiddenColumns {
		switch key {
		case "comparison", "monitor", "models", "last_checked_at":
		default:
			return badRequest("显示列无效。")
		}
		if !seen[key] {
			columns = append(columns, key)
			seen[key] = true
		}
	}
	d.HiddenColumns = columns
	switch d.SortBy {
	case "manual", "name", "balance_asc", "balance_desc", "recent":
	default:
		return badRequest("请选择有效的排序方式。")
	}
	return nil
}

func (h *Handler) saveUpstreamBalanceDisplay(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	key, err := upstreamBalanceDisplayKey(c)
	if err != nil {
		fail(c, err)
		return
	}
	d := defaultUpstreamBalanceDisplay()
	if err = body(c, &d, 1<<20); err == nil {
		err = normalizeUpstreamBalanceDisplay(&d)
	}
	if err != nil {
		fail(c, err)
		return
	}
	if err = h.setRuntime(c.Request.Context(), key, d); err != nil {
		fail(c, err)
		return
	}
	response.Success(c, d)
}
