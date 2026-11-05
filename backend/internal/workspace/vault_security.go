package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/MACOS-DO/sub4api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

const vaultSecurityKey = "account_vault_security"

type vaultSecuritySettings struct {
	TwoFactorEnabled bool `json:"two_factor_enabled"`
}

func (h *Handler) vaultSecuritySettings(ctx context.Context) (vaultSecuritySettings, error) {
	settings := vaultSecuritySettings{TwoFactorEnabled: true}
	if h.db == nil {
		return settings, errors.New("vault settings unavailable")
	}
	var raw []byte
	err := h.db.QueryRowContext(ctx, "SELECT value FROM workspace_runtime WHERE key=$1", vaultSecurityKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return settings, nil
	}
	if err != nil {
		return settings, err
	}
	var stored struct {
		Enabled *bool `json:"two_factor_enabled"`
	}
	if err := json.Unmarshal(raw, &stored); err != nil {
		return settings, err
	}
	if stored.Enabled == nil {
		return settings, errors.New("invalid vault security settings")
	}
	settings.TwoFactorEnabled = *stored.Enabled
	return settings, nil
}

func (h *Handler) getVaultSecurity(c *gin.Context) {
	settings, err := h.vaultSecuritySettings(c.Request.Context())
	if err != nil {
		fail(c, &requestError{503, "资料库保护设置读取失败，请重试。"})
		return
	}
	response.Success(c, settings)
}

func (h *Handler) saveVaultSecurity(c *gin.Context) {
	var p struct {
		Enabled *bool `json:"two_factor_enabled"`
	}
	if err := body(c, &p, 1024); err != nil {
		fail(c, err)
		return
	}
	if p.Enabled == nil {
		fail(c, badRequest("请选择是否开启两步验证保护。"))
		return
	}
	// Serialize policy changes. Disabling existing protection still requires the
	// existing verified admin session; re-enabling does not expose any secrets.
	tx, err := h.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		fail(c, err)
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(c.Request.Context(), "SELECT pg_advisory_xact_lock(748315264)"); err != nil {
		fail(c, err)
		return
	}
	var raw []byte
	err = tx.QueryRowContext(c.Request.Context(), "SELECT value FROM workspace_runtime WHERE key=$1", vaultSecurityKey).Scan(&raw)
	current := vaultSecuritySettings{TwoFactorEnabled: true}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		fail(c, err)
		return
	}
	if err == nil {
		var stored struct {
			Enabled *bool `json:"two_factor_enabled"`
		}
		if json.Unmarshal(raw, &stored) != nil || stored.Enabled == nil {
			fail(c, &requestError{503, "资料库保护设置无效，请检查配置。"})
			return
		}
		current.TwoFactorEnabled = *stored.Enabled
	}
	if current.TwoFactorEnabled && !*p.Enabled && !h.enforceVaultStepUp(c) {
		return
	}
	who, err := h.identity(c)
	if err != nil {
		fail(c, err)
		return
	}
	next := vaultSecuritySettings{TwoFactorEnabled: *p.Enabled}
	encoded, _ := json.Marshal(next)
	if _, err = tx.ExecContext(c.Request.Context(), "INSERT INTO workspace_runtime(key,value) VALUES($1,$2) ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value,updated_at=NOW()", vaultSecurityKey, string(encoded)); err != nil {
		fail(c, err)
		return
	}
	action := "security_enable"
	if !next.TwoFactorEnabled {
		action = "security_disable"
	}
	if _, err = tx.ExecContext(c.Request.Context(), "INSERT INTO workspace_vault_audit(actor_id,record_id,action) VALUES($1,$2,$3)", who.ID, "settings", action); err != nil {
		fail(c, err)
		return
	}
	if err = tx.Commit(); err != nil {
		fail(c, err)
		return
	}
	response.Success(c, next)
}
