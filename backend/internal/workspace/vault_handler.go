package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/response"
	"github.com/MACOS-DO/sub4api/internal/server/middleware"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

func (h *Handler) ConfigureAccountTools(gateway *service.GatewayService, usage *service.AccountUsageService, totp *service.TotpService, users *service.UserService) {
	h.warmupGateway = gateway
	h.warmupUsage = usage
	h.vaultTOTP = totp
	h.vaultUsers = users
}
func (h *Handler) accountToolsAdmin(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if c.GetString("auth_method") != service.AuditAuthMethodJWT && c.GetString("auth_method") != service.AuditAuthMethodPasskey {
		c.AbortWithStatusJSON(403, gin.H{"code": 403, "message": "请使用管理员网页登录访问。"})
		return
	}
	if !c.GetBool("workspace_admin") {
		c.AbortWithStatus(403)
		return
	}
	if c.GetHeader("Sec-Fetch-Site") == "cross-site" {
		c.AbortWithStatus(403)
		return
	}
	if raw := c.GetHeader("Origin"); raw != "" {
		u, err := url.Parse(raw)
		if err != nil || u.Host != c.Request.Host {
			c.AbortWithStatus(403)
			return
		}
	}
	c.Next()
}
func (h *Handler) vaultSensitive(c *gin.Context) {
	if h.vaultTOTP == nil || h.vaultUsers == nil {
		c.AbortWithStatusJSON(503, gin.H{"code": 503, "message": "资料库安全验证暂不可用。"})
		return
	}
	if !middleware.EnforceStepUpAlways(c, h.vaultTOTP, h.vaultUsers) {
		c.Abort()
		return
	}
	c.Next()
}
func (h *Handler) registerAccountTools(manage *gin.RouterGroup) {
	v := manage.Group("/account-vault", h.accountToolsAdmin)
	v.GET("", h.listVault)
	v.POST("/preview", h.vaultSensitive, h.previewVault)
	v.POST("/import", h.vaultSensitive, h.importVault)
	v.POST("/delete", h.vaultSensitive, h.deleteVault)
	v.POST("/:id/reveal", h.vaultSensitive, h.revealVault)
	v.PUT("/:id", h.vaultSensitive, h.updateVault)
	w := manage.Group("/account-warmup", h.accountToolsAdmin)
	w.GET("", h.getWarmup)
	w.PUT("/settings", h.saveWarmupSettings)
	w.POST("/rules", h.saveWarmupRule)
	w.PUT("/rules/:id", h.saveWarmupRule)
	w.DELETE("/rules/:id", h.deleteWarmupRule)
	w.POST("/rules/:id/preview", h.previewWarmupRule)
	w.POST("/rules/:id/run", h.queueWarmupRule)
	w.PUT("/accounts", h.saveWarmupOverrides)
	w.POST("/cancel", h.cancelWarmupRuns)
	w.GET("/runs", h.listWarmupRuns)
}

type vaultRecord struct {
	ID             string             `json:"id"`
	Platform       string             `json:"platform"`
	Title          string             `json:"title"`
	Tags           string             `json:"tags"`
	Status         string             `json:"status"`
	AccountID      *int64             `json:"account_id"`
	Email          string             `json:"email"`
	AuxiliaryEmail string             `json:"auxiliary_email"`
	Fields         map[string]bool    `json:"fields"`
	Version        int64              `json:"version"`
	UpdatedAt      time.Time          `json:"updated_at"`
	OAuthAccount   *vaultOAuthAccount `json:"oauth_account"`
	OAuthMatch     string             `json:"oauth_match"`
}

func (h *Handler) listVault(c *gin.Context) {
	ctx := c.Request.Context()
	v, err := h.vaultKey()
	if err != nil {
		fail(c, &requestError{503, "资料库密钥不可用，请检查密钥文件或恢复原密钥。"})
		return
	}
	page, size := pageNumber(c.Query("page"), 1, 100000), pageNumber(c.Query("page_size"), 20, 100)
	platform, q := c.Query("platform"), strings.TrimSpace(c.Query("query"))
	if len(q) > 320 {
		fail(c, badRequest("搜索内容过长。"))
		return
	}
	hashes := []string{}
	if email, err := vaultEmail(q); err == nil {
		if platform != "" {
			hashes = append(hashes, v.index(platform, email))
		} else {
			pr, e := h.db.QueryContext(ctx, "SELECT DISTINCT platform FROM workspace_account_vault")
			if e != nil {
				fail(c, e)
				return
			}
			for pr.Next() {
				var name string
				if e = pr.Scan(&name); e != nil {
					pr.Close()
					fail(c, e)
					return
				}
				hashes = append(hashes, v.index(name, email))
			}
			e = pr.Err()
			pr.Close()
			if e != nil {
				fail(c, e)
				return
			}
		}
	}
	status := c.Query("status")
	where := "($1='' OR platform=$1) AND ($2='' OR title ILIKE '%'||$2||'%' OR tags ILIKE '%'||$2||'%' OR email_hash=ANY($3::text[])) AND ($4='' OR status=$4)"
	var count int
	if err = h.db.QueryRowContext(ctx, "SELECT count(*) FROM workspace_account_vault WHERE "+where, platform, q, pq.Array(hashes), status).Scan(&count); err != nil {
		fail(c, err)
		return
	}
	rows, err := h.db.QueryContext(ctx, "SELECT id,platform,title,tags,status,account_id,secret_blob,key_id,version,updated_at FROM workspace_account_vault WHERE "+where+" ORDER BY updated_at DESC,id LIMIT $5 OFFSET $6", platform, q, pq.Array(hashes), status, size, (page-1)*size)
	if err != nil {
		fail(c, err)
		return
	}
	defer rows.Close()
	items := []vaultRecord{}
	for rows.Next() {
		var item vaultRecord
		var blob, k string
		if err = rows.Scan(&item.ID, &item.Platform, &item.Title, &item.Tags, &item.Status, &item.AccountID, &blob, &k, &item.Version, &item.UpdatedAt); err != nil {
			fail(c, err)
			return
		}
		s, e := v.open(item.ID, k, blob)
		if e != nil {
			fail(c, e)
			return
		}
		item.Email = s.Email
		item.AuxiliaryEmail = s.AuxiliaryEmail
		item.Fields = map[string]bool{"password": s.Password != "", "auxiliary_password": s.AuxiliaryPassword != "", "session_key": s.SessionKey != "", "api_key": s.APIKey != "", "mail_url": s.MailURL != "", "auxiliary_url": s.AuxiliaryURL != "", "notes": s.Notes != ""}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		fail(c, err)
		return
	}
	if err = rows.Close(); err != nil {
		fail(c, err)
		return
	}
	if err = h.matchVaultOAuthAccounts(ctx, items); err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"items": items, "total": count, "page": page, "page_size": size})
}
func (h *Handler) previewVault(c *gin.Context) {
	var p struct {
		Raw string `json:"raw"`
	}
	if err := body(c, &p, 3<<20); err != nil {
		fail(c, err)
		return
	}
	rows := parseVaultImport(p.Raw)
	v, err := h.vaultKey()
	if err != nil {
		fail(c, err)
		return
	}
	seen := map[string]bool{}
	for i := range rows {
		row := &rows[i]
		if row.Error != "" {
			continue
		}
		e := &row.Entry
		hash := v.index(e.Platform, e.Email)
		if seen[hash] {
			row.Error = "本批次存在重复邮箱，请保留一条或先合并。"
			continue
		}
		seen[hash] = true
		err = h.db.QueryRowContext(c.Request.Context(), "SELECT id,version FROM workspace_account_vault WHERE platform=$1 AND email_hash=$2", e.Platform, hash).Scan(&e.ExistingID, &e.ExpectedVersion)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			fail(c, err)
			return
		}
		e.Action = "insert"
		if e.ExistingID != "" {
			e.Action = "skip"
		}
	}
	response.Success(c, gin.H{"rows": rows})
}
func vaultMerge(old, new vaultSecret) vaultSecret {
	a, _ := json.Marshal(old)
	b, _ := json.Marshal(new)
	var x, y map[string]string
	_ = json.Unmarshal(a, &x)
	_ = json.Unmarshal(b, &y)
	for k, v := range y {
		if x[k] == "" {
			x[k] = v
		}
	}
	raw, _ := json.Marshal(x)
	var out vaultSecret
	_ = json.Unmarshal(raw, &out)
	return out
}
func (h *Handler) importVault(c *gin.Context) {
	var p struct {
		BatchID string       `json:"batch_id"`
		Items   []vaultEntry `json:"items"`
	}
	if err := body(c, &p, 4<<20); err != nil {
		fail(c, err)
		return
	}
	if _, err := uuid.Parse(p.BatchID); err != nil || len(p.Items) == 0 || len(p.Items) > 500 {
		fail(c, badRequest("请检查导入批次及数量。"))
		return
	}
	v, err := h.vaultKey()
	if err != nil {
		fail(c, err)
		return
	}
	raw, _ := json.Marshal(p.Items)
	digest := v.index("batch", string(raw))
	for i := range p.Items {
		if err = validateVaultEntry(&p.Items[i]); err != nil {
			fail(c, err)
			return
		}
	}
	tx, err := h.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		fail(c, err)
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(c.Request.Context(), "SELECT pg_advisory_xact_lock(748315263)"); err != nil {
		fail(c, err)
		return
	}
	var priorDigest string
	var result []byte
	err = tx.QueryRowContext(c.Request.Context(), "SELECT digest,result FROM workspace_vault_batches WHERE id=$1", p.BatchID).Scan(&priorDigest, &result)
	if err == nil {
		if priorDigest != digest {
			fail(c, badRequest("导入批次已用于其他内容，请重新预览。"))
			return
		}
		var out map[string]int
		_ = json.Unmarshal(result, &out)
		response.Success(c, out)
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		fail(c, err)
		return
	}
	counts := map[string]int{"inserted": 0, "updated": 0, "skipped": 0}
	seen := map[string]bool{}
	who, err := h.identity(c)
	if err != nil {
		fail(c, err)
		return
	}
	for _, e := range p.Items {
		hash := v.index(e.Platform, e.Email)
		if seen[hash] {
			fail(c, badRequest("批次内邮箱重复。"))
			return
		}
		seen[hash] = true
		var id, blob, k string
		var version int64
		err = tx.QueryRowContext(c.Request.Context(), "SELECT id,secret_blob,key_id,version FROM workspace_account_vault WHERE platform=$1 AND email_hash=$2 FOR UPDATE", e.Platform, hash).Scan(&id, &blob, &k, &version)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			fail(c, err)
			return
		}
		if id != "" {
			if e.Action == "skip" || e.Action == "" {
				counts["skipped"]++
				continue
			}
			if id != e.ExistingID || version != e.ExpectedVersion {
				fail(c, &requestError{409, "资料已变化，请重新预览后导入。"})
				return
			}
			if e.Action != "fill" && e.Action != "replace" {
				fail(c, badRequest("重复项处理方式无效。"))
				return
			}
			old, x := v.open(id, k, blob)
			if x != nil {
				fail(c, x)
				return
			}
			if e.Action == "fill" {
				e.vaultSecret = vaultMerge(old, e.vaultSecret)
			} else {
				e.vaultSecret = vaultMerge(e.vaultSecret, old)
			}
			blob, err = v.seal(id, e.vaultSecret)
			if err != nil {
				fail(c, err)
				return
			}
			// Missing imported metadata never erases existing metadata.
			_, err = tx.ExecContext(c.Request.Context(), "UPDATE workspace_account_vault SET secret_blob=$2,key_id=$3,version=version+1,updated_at=NOW() WHERE id=$1", id, blob, v.keyID)
			counts["updated"]++
		} else {
			if e.ExistingID != "" {
				fail(c, &requestError{409, "原资料已删除，请重新预览。"})
				return
			}
			id = uuid.NewString()
			blob, err = v.seal(id, e.vaultSecret)
			if err != nil {
				fail(c, err)
				return
			}
			_, err = tx.ExecContext(c.Request.Context(), "INSERT INTO workspace_account_vault(id,platform,title,tags,status,account_id,email_hash,secret_blob,key_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)", id, e.Platform, e.Title, e.Tags, e.Status, e.AccountID, hash, blob, v.keyID)
			counts["inserted"]++
		}
		if err != nil {
			fail(c, err)
			return
		}
		if _, err = tx.ExecContext(c.Request.Context(), "INSERT INTO workspace_vault_audit(actor_id,record_id,action) VALUES($1,$2,'import')", who.ID, id); err != nil {
			fail(c, err)
			return
		}
	}
	result, _ = json.Marshal(counts)
	if _, err = tx.ExecContext(c.Request.Context(), "INSERT INTO workspace_vault_batches(id,digest,result) VALUES($1,$2,$3)", p.BatchID, digest, result); err != nil {
		fail(c, err)
		return
	}
	if err = tx.Commit(); err != nil {
		fail(c, err)
		return
	}
	response.Success(c, counts)
}
func (h *Handler) revealVault(c *gin.Context) {
	var p struct {
		Field string `json:"field"`
	}
	if err := body(c, &p, 1024); err != nil {
		fail(c, err)
		return
	}
	allowed := map[string]bool{"all": true, "password": true, "auxiliary_password": true, "session_key": true, "api_key": true, "mail_url": true, "auxiliary_url": true, "notes": true}
	if !allowed[p.Field] {
		fail(c, badRequest("字段无效。"))
		return
	}
	id := c.Param("id")
	var blob, k string
	var version int64
	if err := h.db.QueryRowContext(c.Request.Context(), "SELECT secret_blob,key_id,version FROM workspace_account_vault WHERE id=$1", id).Scan(&blob, &k, &version); err != nil {
		fail(c, err)
		return
	}
	v, err := h.vaultKey()
	if err != nil {
		fail(c, err)
		return
	}
	s, err := v.open(id, k, blob)
	if err != nil {
		fail(c, err)
		return
	}
	who, err := h.identity(c)
	if err != nil {
		fail(c, err)
		return
	}
	if _, err = h.db.ExecContext(c.Request.Context(), "INSERT INTO workspace_vault_audit(actor_id,record_id,action,field) VALUES($1,$2,'reveal',$3)", who.ID, id, p.Field); err != nil {
		fail(c, err)
		return
	}
	if p.Field == "all" {
		response.Success(c, gin.H{"secret": s, "version": version})
		return
	}
	raw, _ := json.Marshal(s)
	var fields map[string]string
	_ = json.Unmarshal(raw, &fields)
	response.Success(c, gin.H{"value": fields[p.Field]})
}
func (h *Handler) updateVault(c *gin.Context) {
	var e vaultEntry
	if err := body(c, &e, 64<<10); err != nil {
		fail(c, err)
		return
	}
	if err := validateVaultEntry(&e); err != nil {
		fail(c, err)
		return
	}
	v, err := h.vaultKey()
	if err != nil {
		fail(c, err)
		return
	}
	blob, err := v.seal(c.Param("id"), e.vaultSecret)
	if err != nil {
		fail(c, err)
		return
	}
	who, err := h.identity(c)
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
	res, err := tx.ExecContext(c.Request.Context(), "UPDATE workspace_account_vault SET platform=$2,title=$3,tags=$4,status=$5,account_id=$6,email_hash=$7,secret_blob=$8,key_id=$9,version=version+1,updated_at=NOW() WHERE id=$1 AND version=$10", c.Param("id"), e.Platform, e.Title, e.Tags, e.Status, e.AccountID, v.index(e.Platform, e.Email), blob, v.keyID, e.ExpectedVersion)
	if err != nil {
		fail(c, badRequest("无法保存，请检查邮箱是否重复。"))
		return
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		fail(c, &requestError{409, "资料已变化，请重新打开编辑。"})
		return
	}
	if _, err = tx.ExecContext(c.Request.Context(), "INSERT INTO workspace_vault_audit(actor_id,record_id,action) VALUES($1,$2,'update')", who.ID, c.Param("id")); err != nil {
		fail(c, err)
		return
	}
	if err = tx.Commit(); err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"saved": true})
}
func (h *Handler) deleteVault(c *gin.Context) {
	var p struct {
		IDs     []string `json:"ids"`
		Confirm bool     `json:"confirm"`
	}
	if err := body(c, &p, 64<<10); err != nil {
		fail(c, err)
		return
	}
	if !p.Confirm || len(p.IDs) == 0 || len(p.IDs) > 500 {
		fail(c, badRequest("请确认要删除的资料。"))
		return
	}
	who, err := h.identity(c)
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
	n := int64(0)
	for _, id := range p.IDs {
		res, err := tx.ExecContext(c.Request.Context(), "DELETE FROM workspace_account_vault WHERE id=$1", id)
		if err != nil {
			fail(c, err)
			return
		}
		count, _ := res.RowsAffected()
		n += count
		if count > 0 {
			if _, err = tx.ExecContext(c.Request.Context(), "INSERT INTO workspace_vault_audit(actor_id,record_id,action) VALUES($1,$2,'delete')", who.ID, id); err != nil {
				fail(c, err)
				return
			}
		}
	}
	if err = tx.Commit(); err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": n})
}

type warmupExecutor interface {
	RefreshWarmupQuota(context.Context, *service.AccountUsageService, int64) (*service.WarmupQuota, error)
	RunAccountWarmup(context.Context, int64, string, string, int, func() error) (service.WarmupAttemptResult, error)
}
