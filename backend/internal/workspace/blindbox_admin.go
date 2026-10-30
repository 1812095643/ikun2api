package workspace

import (
	"github.com/MACOS-DO/sub4api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

func (h *Handler) getBlindboxSettings(c *gin.Context) {
	if !c.GetBool("workspace_admin") {
		fail(c, notFound())
		return
	}
	c.Header("Cache-Control", "no-store")
	s, err := loadBlindboxSettings(c.Request.Context(), h.db, false)
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, s)
}
func (h *Handler) saveBlindboxSettings(c *gin.Context) {
	if !c.GetBool("workspace_admin") {
		fail(c, notFound())
		return
	}
	a, err := h.identity(c)
	if err != nil {
		fail(c, err)
		return
	}
	var s blindboxSettings
	if err = decodeCheckinBody(c, &s); err == nil {
		err = normalizeBlindboxSpec(&s.blindboxSpec)
	}
	if err != nil {
		fail(c, err)
		return
	}
	if _, err = h.db.ExecContext(c.Request.Context(), `UPDATE workspace_blindbox_settings SET enabled=$1,price=$2::numeric,minimum=$3::numeric,maximum=$4::numeric,draw_time=$5,updated_by=$6,updated_at=NOW() WHERE id=1`, s.Enabled, s.Price, s.Minimum, s.Maximum, s.DrawTime, a.ID); err != nil {
		fail(c, err)
		return
	}
	response.Success(c, s)
}

func (h *Handler) listBlindboxRounds(c *gin.Context) {
	if !c.GetBool("workspace_admin") {
		fail(c, notFound())
		return
	}
	c.Header("Cache-Control", "no-store")
	ctx := c.Request.Context()
	key, err := h.lotteryKey(ctx)
	if err != nil {
		fail(c, err)
		return
	}
	page, size := pageNumber(c.Query("page"), 1, 100000), pageNumber(c.Query("page_size"), 20, 100)
	var total int
	if err = h.db.QueryRowContext(ctx, `SELECT count(*) FROM workspace_blindbox_rounds`).Scan(&total); err != nil {
		fail(c, err)
		return
	}
	rows, err := h.db.QueryContext(ctx, `SELECT `+blindboxRoundColumns+` FROM workspace_blindbox_rounds ORDER BY business_date DESC LIMIT $1 OFFSET $2`, size, (page-1)*size)
	if err != nil {
		fail(c, err)
		return
	}
	defer rows.Close()
	items := []blindboxRound{}
	for rows.Next() {
		item, readErr := scanBlindboxRound(rows)
		if readErr == nil && !lotterySignatureOK(item.Signature, blindboxRoundSign(key, item)) {
			fail(c, badRequest("场次规则校验未通过。"))
			return
		}
		if readErr != nil {
			fail(c, readErr)
			return
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"items": items, "total": total, "page": page, "page_size": size})
}
