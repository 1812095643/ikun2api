package workspace

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/MACOS-DO/sub4api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

func (h *Handler) listUpstreamBalances(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	_, records, err := h.prepareUpstreamBalances(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"items": records, "total": len(records)})
}
func findUpstreamBalanceRecord(records []upstreamBalanceRecord, id string) (upstreamBalanceRecord, error) {
	for _, r := range records {
		if r.ID == id {
			return r, nil
		}
	}
	return upstreamBalanceRecord{}, notFound()
}
func (h *Handler) saveUpstreamBalance(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req struct {
		Name             string                `json:"name"`
		Config           upstreamBalanceConfig `json:"config"`
		WalletToken      *string               `json:"wallet_token"`
		ClearWalletToken bool                  `json:"clear_wallet_token"`
	}
	if err := body(c, &req, 32<<10); err != nil {
		fail(c, err)
		return
	}
	name, err := trimText(req.Name, 100)
	if err != nil || name == "" || strings.ContainsAny(name, "\r\n\x00") {
		fail(c, badRequest("供应商名称不能为空，最多 100 字。"))
		return
	}
	if err = normalizeUpstreamBalanceConfig(&req.Config); err != nil {
		fail(c, err)
		return
	}
	_, records, err := h.prepareUpstreamBalances(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	r, err := findUpstreamBalanceRecord(records, c.Param("id"))
	if err != nil {
		fail(c, err)
		return
	}
	if req.Config.PreferredAccountID != 0 {
		found := false
		for _, a := range r.Accounts {
			if a.ID == req.Config.PreferredAccountID {
				found = true
			}
		}
		if !found {
			fail(c, badRequest("监控 Key 必须属于当前上游。"))
			return
		}
	}
	secret := r.secret
	if req.ClearWalletToken {
		secret = ""
	}
	if req.WalletToken != nil && strings.TrimSpace(*req.WalletToken) != "" {
		token := strings.TrimSpace(*req.WalletToken)
		if err = validateWalletToken(token); err != nil {
			fail(c, err)
			return
		}
		if h.encryptor == nil {
			fail(c, badRequest("服务器未配置凭据加密。"))
			return
		}
		secret, err = h.encryptor.Encrypt(token)
		if err != nil {
			fail(c, err)
			return
		}
	}
	config, _ := json.Marshal(req.Config)
	// A revision fences off in-flight requests using an old token or old source.
	result, err := h.db.ExecContext(c.Request.Context(), `UPDATE workspace_upstream_balances SET name=$2,config=$3,secret_blob=$4,revision=revision+1,
  snapshot='{}',alert='{}',last_checked_at=NULL,next_check_at=NOW(),updated_at=NOW() WHERE id=$1 AND revision=$5`, r.ID, name, string(config), secret, r.revision)
	if err != nil {
		fail(c, err)
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		fail(c, &requestError{409, "配置已发生变化，请刷新后重新保存。"})
		return
	}
	response.Success(c, gin.H{"saved": true})
	h.wakeUpstreamBalances()
}
func (h *Handler) queueUpstreamBalances(ctx context.Context, id string) (int, error) {
	_, records, err := h.prepareUpstreamBalances(ctx)
	if err != nil {
		return 0, err
	}
	if id != "" {
		if _, err = findUpstreamBalanceRecord(records, id); err != nil {
			return 0, err
		}
	}
	count := 0
	for _, r := range records {
		if id != "" && r.ID != id {
			continue
		}
		result, e := h.db.ExecContext(ctx, `UPDATE workspace_upstream_balances SET refresh_requested=TRUE,requested_at=NOW() WHERE id=$1 AND refresh_requested=FALSE
   AND (lease_until IS NULL OR lease_until<=NOW()) AND (requested_at IS NULL OR requested_at<NOW()-INTERVAL '30 seconds')`, r.ID)
		if e != nil {
			return count, e
		}
		n, _ := result.RowsAffected()
		count += int(n)
	}
	h.wakeUpstreamBalances()
	return count, nil
}
func (h *Handler) refreshUpstreamBalances(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	count, err := h.queueUpstreamBalances(c.Request.Context(), c.Param("id"))
	if err != nil {
		fail(c, err)
		return
	}
	response.Accepted(c, gin.H{"queued": count})
}
func (h *Handler) wakeUpstreamBalances() {
	if h.balanceWake != nil {
		select {
		case h.balanceWake <- struct{}{}:
		default:
		}
	}
}
