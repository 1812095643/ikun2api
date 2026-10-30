package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/mail"
	"sort"
	"strings"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/response"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
)

type loginSecret struct {
	Email     string `json:"email"`
	Password  string `json:"password,omitempty"`
	MFASecret string `json:"mfa_secret,omitempty"`
	MFACode   string `json:"mfa_code,omitempty"`
	InboxURL  string `json:"inbox_url,omitempty"`
	AuthMode  string `json:"auth_mode"`
}
type importedAccount struct {
	Email       string     `json:"email"`
	AuthMode    string     `json:"auth_mode"`
	HasPassword bool       `json:"has_password"`
	HasMFA      bool       `json:"has_mfa"`
	HasInbox    bool       `json:"has_inbox"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	LastUsedAt  *time.Time `json:"last_used_at"`
}
type reloginRun struct {
	ID           int64          `json:"id"`
	Email        string         `json:"email"`
	AccountID    *int64         `json:"account_id"`
	Trigger      string         `json:"trigger"`
	Status       string         `json:"status"`
	StartedAt    time.Time      `json:"started_at"`
	FinishedAt   *time.Time     `json:"finished_at"`
	ErrorMessage string         `json:"error_message"`
	PlanType     string         `json:"plan_type"`
	Applied      bool           `json:"applied"`
	ApplyAction  string         `json:"apply_action"`
	Challenge    map[string]any `json:"challenge,omitempty"`
	Blob         string         `json:"-"`
}
type accountSummary struct {
	ID           int64     `json:"id"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	Status       string    `json:"status"`
	Schedulable  bool      `json:"schedulable"`
	PlanType     string    `json:"plan_type"`
	ErrorMessage string    `json:"error_message"`
	HasImported  bool      `json:"has_imported"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func normalizeEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || len(email) > 320 {
		return "", badRequest("请填写完整的账号邮箱。")
	}
	return email, nil
}
func accountEmail(a service.Account) string {
	return strings.ToLower(strings.TrimSpace(a.GetCredential("email")))
}

func (h *Handler) allOpenAIAccounts(ctx context.Context) ([]service.Account, error) {
	all := []service.Account{}
	for page := 1; ; page++ {
		items, total, err := h.admin.ListAccounts(ctx, page, 100, "openai", "oauth", "", "", 0, "", "id", "asc")
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if int64(len(all)) >= total {
			return all, nil
		}
		if len(items) == 0 {
			return nil, &requestError{503, "账号列表未读取完整，请稍后再试。"}
		}
	}
}
func (h *Handler) reloginAccounts(c *gin.Context) {
	accounts, err := h.allOpenAIAccounts(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	rows, err := h.db.QueryContext(c.Request.Context(), "SELECT email FROM workspace_relogin_accounts")
	if err != nil {
		fail(c, err)
		return
	}
	imported := map[string]bool{}
	for rows.Next() {
		var email string
		if rows.Scan(&email) == nil {
			imported[email] = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		fail(c, err)
		return
	}
	items := []accountSummary{}
	for _, a := range accounts {
		if a.ParentAccountID != nil {
			continue
		}
		email := accountEmail(a)
		items = append(items, accountSummary{ID: a.ID, Email: email, Name: a.Name, Status: a.Status, Schedulable: a.Schedulable, PlanType: a.GetCredential("plan_type"), ErrorMessage: a.ErrorMessage, HasImported: imported[email], UpdatedAt: a.UpdatedAt})
	}
	response.Success(c, gin.H{"items": items, "synced_at": time.Now()})
}

func (h *Handler) loadSecret(ctx context.Context, email string) (loginSecret, error) {
	var blob string
	if err := h.db.QueryRowContext(ctx, "SELECT secret_blob FROM workspace_relogin_accounts WHERE email=$1", email).Scan(&blob); err != nil {
		return loginSecret{}, err
	}
	raw, err := h.encryptor.Decrypt(blob)
	if err != nil {
		return loginSecret{}, err
	}
	var secret loginSecret
	if err = json.Unmarshal([]byte(raw), &secret); err != nil {
		return secret, err
	}
	// 将邮箱写入加密内容并复核，避免密文被调换后关联到另一个账号。
	if secret.Email != email {
		return loginSecret{}, badRequest("保存的账号资料与邮箱不一致，请重新导入。")
	}
	return secret, nil
}
func validateSecret(s *loginSecret) error {
	var err error
	s.Email, err = normalizeEmail(s.Email)
	if err != nil {
		return err
	}
	if s.AuthMode == "" {
		s.AuthMode = "password_2fa"
	}
	if s.AuthMode != "password_2fa" && s.AuthMode != "email_otp" {
		return badRequest("请选择密码验证或邮箱验证码。")
	}
	if len(s.Password) > 1024 || len(s.MFASecret) > 512 || len(s.MFACode) > 32 || len(s.InboxURL) > 2048 {
		return badRequest("账号资料过长，请检查输入。")
	}
	if s.AuthMode == "password_2fa" && strings.TrimSpace(s.Password) == "" {
		return badRequest("密码验证方式需要填写密码。")
	}
	return nil
}

func (h *Handler) importAccounts(c *gin.Context) {
	var p struct {
		Items []loginSecret `json:"items"`
	}
	if err := body(c, &p, 2<<20); err != nil {
		fail(c, err)
		return
	}
	if len(p.Items) == 0 || len(p.Items) > 100 {
		fail(c, badRequest("每次请导入 1 至 100 条账号资料。"))
		return
	}
	for i := range p.Items {
		if err := validateSecret(&p.Items[i]); err != nil {
			fail(c, err)
			return
		}
	}
	ctx := c.Request.Context()
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		fail(c, err)
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(748315202)"); err != nil {
		fail(c, err)
		return
	}
	for _, item := range p.Items {
		raw, e := json.Marshal(item)
		if e != nil {
			fail(c, e)
			return
		}
		blob, e := h.encryptor.Encrypt(string(raw))
		if e != nil {
			fail(c, e)
			return
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO workspace_relogin_accounts(email,secret_blob) VALUES($1,$2) ON CONFLICT(email) DO UPDATE SET secret_blob=EXCLUDED.secret_blob,updated_at=NOW()", item.Email, blob)
		if err != nil {
			fail(c, err)
			return
		}
	}
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM workspace_relogin_accounts").Scan(&count); err != nil {
		fail(c, err)
		return
	}
	if count > 500 {
		fail(c, badRequest("最多保存 500 条账号资料，请整理后再导入。"))
		return
	}
	if err = tx.Commit(); err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"saved": len(p.Items)})
}
func (h *Handler) listImported(c *gin.Context) {
	page := pageNumber(c.Query("page"), 1, 100000)
	size := pageNumber(c.Query("page_size"), 20, 50)
	q := strings.TrimSpace(c.Query("query"))
	if len(q) > 320 {
		fail(c, badRequest("搜索内容太长。"))
		return
	}
	var total int
	ctx := c.Request.Context()
	if err := h.db.QueryRowContext(ctx, "SELECT count(*) FROM workspace_relogin_accounts WHERE email ILIKE '%'||$1||'%'", q).Scan(&total); err != nil {
		fail(c, err)
		return
	}
	rows, err := h.db.QueryContext(ctx, "SELECT email,secret_blob,created_at,updated_at,last_used_at FROM workspace_relogin_accounts WHERE email ILIKE '%'||$1||'%' ORDER BY updated_at DESC LIMIT $2 OFFSET $3", q, size, (page-1)*size)
	if err != nil {
		fail(c, err)
		return
	}
	defer rows.Close()
	items := []importedAccount{}
	for rows.Next() {
		var item importedAccount
		var blob string
		if err = rows.Scan(&item.Email, &blob, &item.CreatedAt, &item.UpdatedAt, &item.LastUsedAt); err != nil {
			fail(c, err)
			return
		}
		raw, e := h.encryptor.Decrypt(blob)
		if e != nil {
			fail(c, e)
			return
		}
		var secret loginSecret
		if e = json.Unmarshal([]byte(raw), &secret); e != nil {
			fail(c, e)
			return
		}
		item.AuthMode = secret.AuthMode
		item.HasPassword = secret.Password != ""
		item.HasMFA = secret.MFASecret != ""
		item.HasInbox = secret.InboxURL != ""
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"items": items, "total": total, "page": page, "page_size": size})
}

type emailsPayload struct {
	Emails  []string `json:"emails"`
	Confirm bool     `json:"confirm"`
}

func validateEmails(p *emailsPayload) error {
	if len(p.Emails) == 0 || len(p.Emails) > 500 {
		return badRequest("请选择 1 至 500 条资料。")
	}
	for i, v := range p.Emails {
		email, err := normalizeEmail(v)
		if err != nil {
			return err
		}
		p.Emails[i] = email
	}
	return nil
}
func (h *Handler) exportImported(c *gin.Context) {
	var p emailsPayload
	if err := body(c, &p, 256<<10); err != nil {
		fail(c, err)
		return
	}
	if err := validateEmails(&p); err != nil {
		fail(c, err)
		return
	}
	items := []loginSecret{}
	for _, email := range p.Emails {
		secret, err := h.loadSecret(c.Request.Context(), email)
		if err != nil {
			fail(c, err)
			return
		}
		items = append(items, secret)
	}
	c.Header("Cache-Control", "private, no-store")
	response.Success(c, gin.H{"items": items})
}
func (h *Handler) deleteImported(c *gin.Context) {
	var p emailsPayload
	if err := body(c, &p, 256<<10); err != nil {
		fail(c, err)
		return
	}
	if err := validateEmails(&p); err != nil {
		fail(c, err)
		return
	}
	if !p.Confirm {
		fail(c, badRequest("请先确认删除所选登录资料。"))
		return
	}
	ctx := c.Request.Context()
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		fail(c, err)
		return
	}
	defer tx.Rollback()
	for _, email := range p.Emails {
		if _, err = tx.ExecContext(ctx, "DELETE FROM workspace_relogin_accounts WHERE email=$1", email); err != nil {
			fail(c, err)
			return
		}
	}
	if err = tx.Commit(); err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": len(p.Emails)})
}

type invalidImported struct {
	Email     string    `json:"email"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (h *Handler) invalidImported(ctx context.Context) ([]invalidImported, string, error) {
	accounts, err := h.allOpenAIAccounts(ctx)
	if err != nil {
		return nil, "", err
	}
	keep := map[string]bool{}
	for _, a := range accounts {
		if email := accountEmail(a); email != "" {
			keep[email] = true
		}
	}
	rows, err := h.db.QueryContext(ctx, "SELECT email,updated_at FROM workspace_relogin_accounts ORDER BY email")
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	items := []invalidImported{}
	for rows.Next() {
		var item invalidImported
		if err = rows.Scan(&item.Email, &item.UpdatedAt); err != nil {
			return nil, "", err
		}
		if !keep[item.Email] {
			items = append(items, item)
		}
	}
	if err = rows.Err(); err != nil {
		return nil, "", err
	}
	raw, _ := json.Marshal(items)
	sum := sha256.Sum256(raw)
	return items, hex.EncodeToString(sum[:]), nil
}
func (h *Handler) previewInvalidImported(c *gin.Context) {
	items, revision, err := h.invalidImported(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"items": items, "revision": revision})
}
func (h *Handler) deleteInvalidImported(c *gin.Context) {
	var p struct {
		Revision string `json:"revision"`
		Confirm  bool   `json:"confirm"`
	}
	if err := body(c, &p, 1024); err != nil {
		fail(c, err)
		return
	}
	if !p.Confirm {
		fail(c, badRequest("请先预览并确认要删除的资料。"))
		return
	}
	items, revision, err := h.invalidImported(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	if revision != p.Revision {
		fail(c, &requestError{409, "账号或资料发生了变化，请重新预览。"})
		return
	}
	ctx := c.Request.Context()
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		fail(c, err)
		return
	}
	defer tx.Rollback()
	count := int64(0)
	// 再次检查当前账号表，只删除已预览且未被更新的登录资料；不删除渠道账号。
	for _, item := range items {
		res, e := tx.ExecContext(ctx, `DELETE FROM workspace_relogin_accounts r WHERE email=$1 AND updated_at=$2 AND NOT EXISTS (SELECT 1 FROM accounts a WHERE a.platform='openai' AND a.type='oauth' AND a.deleted_at IS NULL AND lower(trim(a.credentials->>'email'))=r.email)`, item.Email, item.UpdatedAt)
		if e != nil {
			fail(c, e)
			return
		}
		n, e := res.RowsAffected()
		if e != nil {
			fail(c, e)
			return
		}
		count += n
	}
	if count != int64(len(items)) {
		fail(c, &requestError{409, "账号或资料发生了变化，请重新预览。"})
		return
	}
	if err = tx.Commit(); err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": count})
}

const runColumns = "id,email,account_id,trigger,status,started_at,finished_at,error_message,plan_type,applied,apply_action,result_blob"

func scanRun(row rowScanner) (reloginRun, error) {
	var run reloginRun
	err := row.Scan(&run.ID, &run.Email, &run.AccountID, &run.Trigger, &run.Status, &run.StartedAt, &run.FinishedAt, &run.ErrorMessage, &run.PlanType, &run.Applied, &run.ApplyAction, &run.Blob)
	return run, err
}
func (h *Handler) loadRun(ctx context.Context, id int64) (reloginRun, error) {
	return scanRun(h.db.QueryRowContext(ctx, "SELECT "+runColumns+" FROM workspace_relogin_runs WHERE id=$1", id))
}
func (h *Handler) listRuns(c *gin.Context) {
	rows, err := h.db.QueryContext(c.Request.Context(), "SELECT "+runColumns+" FROM workspace_relogin_runs ORDER BY id DESC LIMIT 200")
	if err != nil {
		fail(c, err)
		return
	}
	defer rows.Close()
	items := []reloginRun{}
	for rows.Next() {
		run, e := scanRun(rows)
		if e != nil {
			fail(c, e)
			return
		}
		items = append(items, run)
	}
	if err = rows.Err(); err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"items": items})
}
func (h *Handler) getRun(c *gin.Context) {
	id, err := positiveID(c)
	if err != nil {
		fail(c, err)
		return
	}
	run, err := h.loadRun(c.Request.Context(), id)
	if err != nil {
		fail(c, err)
		return
	}
	if run.Status == "challenge" {
		result, e := h.runResult(run)
		if e != nil {
			fail(c, e)
			return
		}
		if challenge, ok := result["challenge"].(map[string]any); ok {
			run.Challenge = map[string]any{}
			for _, key := range []string{"id", "type", "message", "phone_channel", "masked_email", "masked_phone"} {
				if v, exists := challenge[key]; exists {
					run.Challenge[key] = v
				}
			}
		}
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, run)
}
func (h *Handler) clearRuns(c *gin.Context) {
	var p struct {
		Confirm bool `json:"confirm"`
	}
	if err := body(c, &p, 1024); err != nil {
		fail(c, err)
		return
	}
	if !p.Confirm {
		fail(c, badRequest("请先确认清理历史记录。"))
		return
	}
	_, err := h.db.ExecContext(c.Request.Context(), "DELETE FROM workspace_relogin_runs WHERE status NOT IN ('running','challenge')")
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"cleared": true})
}

func sortedEmails(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
