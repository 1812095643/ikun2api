package workspace

import (
	"encoding/json"

	"github.com/MACOS-DO/sub4api/internal/pkg/response"
	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
)

type upstreamBalanceMonitoring struct {
	Enabled         bool     `json:"monitor_enabled"`
	IntervalMinutes int      `json:"interval_minutes"`
	Threshold       float64  `json:"threshold"`
	Recipients      []string `json:"recipients"`
}

func defaultUpstreamBalanceMonitoring() upstreamBalanceMonitoring {
	c := defaultUpstreamBalanceConfig()
	return upstreamBalanceMonitoring{true, c.IntervalMinutes, c.Threshold, c.Recipients}
}

func normalizeUpstreamBalanceMonitoring(m *upstreamBalanceMonitoring) error {
	c := defaultUpstreamBalanceConfig()
	c.MonitorEnabled, c.IntervalMinutes, c.Threshold, c.Recipients = m.Enabled, m.IntervalMinutes, m.Threshold, m.Recipients
	if err := normalizeUpstreamBalanceConfig(&c); err != nil {
		return err
	}
	m.Recipients = c.Recipients
	return nil
}

func (h *Handler) saveUpstreamBalanceMonitoring(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req struct {
		upstreamBalanceMonitoring
		All bool     `json:"all"`
		IDs []string `json:"ids"`
	}
	if err := body(c, &req, 400<<10); err != nil {
		fail(c, err)
		return
	}
	if err := normalizeUpstreamBalanceMonitoring(&req.upstreamBalanceMonitoring); err != nil {
		fail(c, err)
		return
	}
	if (!req.All && len(req.IDs) == 0) || len(req.IDs) > 5000 || (req.All && len(req.IDs) > 0) {
		fail(c, badRequest("请选择全部上游或需要设置的上游。"))
		return
	}
	ctx := c.Request.Context()
	_, records, err := h.prepareUpstreamBalances(ctx)
	if err != nil {
		fail(c, err)
		return
	}
	ids := req.IDs
	if req.All {
		for _, record := range records {
			ids = append(ids, record.ID)
		}
	} else {
		for _, id := range ids {
			if _, err = findUpstreamBalanceRecord(records, id); err != nil {
				fail(c, err)
				return
			}
		}
	}
	raw, err := json.Marshal(req.upstreamBalanceMonitoring)
	if err != nil {
		fail(c, err)
		return
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		fail(c, err)
		return
	}
	defer tx.Rollback()
	// 仅合并监控字段，不覆盖钱包凭据、监控 Key、供应商设置或正在展示的余额。
	result, err := tx.ExecContext(ctx, `UPDATE workspace_upstream_balances
	 SET config=config || $2::jsonb,revision=revision+1,alert='{}',next_check_at=NOW(),updated_at=NOW()
	 WHERE id=ANY($1) AND NOT config @> $2::jsonb`, pq.Array(ids), string(raw))
	if err != nil {
		fail(c, err)
		return
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO workspace_runtime(key,value) VALUES('upstream_balance_monitoring',$1::jsonb)
	 ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value,updated_at=NOW()`, string(raw)); err != nil {
		fail(c, err)
		return
	}
	if err = tx.Commit(); err != nil {
		fail(c, err)
		return
	}
	affected, _ := result.RowsAffected()
	h.wakeUpstreamBalances()
	response.Success(c, gin.H{"saved": true, "affected": affected})
}
