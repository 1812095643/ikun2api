package workspace

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/response"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const sessionStudioURL = "https://session.ameng2027.xyz/api/v1/relogin"

func (h *Handler) acquireRun(email string) bool {
	h.runMu.Lock()
	defer h.runMu.Unlock()
	if h.activeRuns[email] {
		return false
	}
	h.activeRuns[email] = true
	return true
}
func (h *Handler) releaseRun(email string) {
	h.runMu.Lock()
	delete(h.activeRuns, email)
	h.runMu.Unlock()
}

func (h *Handler) runResult(run reloginRun) (map[string]any, error) {
	if run.Blob == "" {
		return nil, badRequest("这次操作还没有返回登录结果。")
	}
	raw, err := h.encryptor.Decrypt(run.Blob)
	if err != nil {
		return nil, err
	}
	var saved struct {
		Email  string         `json:"email"`
		Result map[string]any `json:"result"`
	}
	if err = json.Unmarshal([]byte(raw), &saved); err != nil {
		return nil, err
	}
	if saved.Email != run.Email {
		return nil, badRequest("登录结果与邮箱不一致。")
	}
	return saved.Result, nil
}

func findCredentials(value any) map[string]any {
	object, ok := value.(map[string]any)
	if ok {
		if token, _ := object["access_token"].(string); token != "" {
			return object
		}
		for _, key := range []string{"credential", "credentials", "tokens", "data", "payload", "result"} {
			if result := findCredentials(object[key]); result != nil {
				return result
			}
		}
	}
	return nil
}
func stringValue(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return strings.TrimSpace(v)
}

func requestSession(ctx context.Context, payload map[string]any) (map[string]any, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sessionStudioURL, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/x-ndjson, application/json")
	req.Header.Set("X-Session-Studio-Relogin", "1")
	req.Header.Set("X-Session-Studio-Client", uuid.NewString())
	client := &http.Client{Timeout: 1700 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	r, err := client.Do(req)
	if err != nil {
		return nil, &requestError{502, "重登服务暂时无法连接，请稍后重试。"}
	}
	defer r.Body.Close()
	if r.StatusCode < 200 || r.StatusCode >= 300 {
		return nil, &requestError{502, "重登服务未完成这次请求，请检查账号资料后重试。"}
	}
	reader := io.LimitReader(r.Body, (1<<20)+1)
	if !strings.Contains(r.Header.Get("Content-Type"), "ndjson") {
		data, e := io.ReadAll(reader)
		if e != nil || len(data) > 1<<20 {
			return nil, &requestError{502, "重登结果过大或未完整返回。"}
		}
		var result map[string]any
		if e = json.Unmarshal(data, &result); e != nil {
			return nil, &requestError{502, "重登结果无法读取。"}
		}
		return result, nil
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 8192), (1<<20)+1)
	size := 0
	var result map[string]any
	for scanner.Scan() {
		size += len(scanner.Bytes())
		if size > 1<<20 {
			return nil, &requestError{502, "重登结果过大。"}
		}
		var event map[string]any
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			continue
		}
		if event["type"] == "result" {
			result, _ = event["payload"].(map[string]any)
		} else if findCredentials(event) != nil {
			result = event
		}
	}
	if scanner.Err() != nil || result == nil {
		return nil, &requestError{502, "重登服务没有完整返回结果，请稍后重试。"}
	}
	return result, nil
}

func (h *Handler) saveRunResult(ctx context.Context, id int64, email string, result map[string]any) (string, error) {
	status := "failed"
	message := "未取得完整凭据，请检查登录资料。"
	if findCredentials(result) != nil {
		status = "success"
		message = ""
	} else if _, ok := result["challenge"].(map[string]any); ok {
		status = "challenge"
		message = ""
	}
	raw, err := json.Marshal(map[string]any{"email": email, "result": result})
	if err != nil {
		return "", err
	}
	blob, err := h.encryptor.Encrypt(string(raw))
	if err != nil {
		return "", err
	}
	_, err = h.db.ExecContext(ctx, "UPDATE workspace_relogin_runs SET status=$2,finished_at=NOW(),result_blob=$3,error_message=$4 WHERE id=$1", id, status, blob, message)
	return status, err
}

func (h *Handler) failRun(id int64, err error) {
	message := "本次操作未完成，请检查资料后重试。"
	var e *requestError
	if errors.As(err, &e) {
		message = e.message
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = h.db.ExecContext(ctx, "UPDATE workspace_relogin_runs SET status='failed',finished_at=NOW(),error_message=$2 WHERE id=$1", id, message)
}

func (h *Handler) validateTargetAccount(ctx context.Context, email string, id *int64) error {
	if id == nil || *id == 0 {
		return nil
	}
	a, err := h.admin.GetAccount(ctx, *id)
	if err != nil {
		return err
	}
	if a.Platform != "openai" || a.Type != "oauth" || a.ParentAccountID != nil || accountEmail(*a) != email {
		return badRequest("目标渠道账号与邮箱不一致，请重新选择。")
	}
	return nil
}

func (h *Handler) queueRelogin(ctx context.Context, secret loginSecret, accountID *int64, trigger string, apply bool) (int64, error) {
	if err := validateSecret(&secret); err != nil {
		return 0, err
	}
	if err := h.validateTargetAccount(ctx, secret.Email, accountID); err != nil {
		return 0, err
	}
	if !h.acquireRun(secret.Email) {
		return 0, &requestError{409, "这个账号已有重登任务，请等待当前任务完成。"}
	}
	var id int64
	err := h.db.QueryRowContext(ctx, "INSERT INTO workspace_relogin_runs(email,account_id,trigger) VALUES($1,$2,$3) RETURNING id", secret.Email, accountID, trigger).Scan(&id)
	if err != nil {
		h.releaseRun(secret.Email)
		return 0, err
	}
	go func() {
		defer h.releaseRun(secret.Email)
		job, cancel := context.WithTimeout(h.background, 29*time.Minute)
		defer cancel()
		payload := map[string]any{"action": "start", "email": secret.Email, "auth_mode": secret.AuthMode, "phone_binding_mode": "manual"}
		if secret.AuthMode == "email_otp" {
			if secret.InboxURL != "" {
				payload["inbox_url"] = secret.InboxURL
			}
		} else {
			payload["password"] = secret.Password
			if secret.MFASecret != "" {
				payload["mfa_secret"] = secret.MFASecret
			}
			if secret.MFACode != "" {
				payload["mfa_code"] = secret.MFACode
			}
		}
		result, e := requestSession(job, payload)
		if e != nil {
			h.failRun(id, e)
			return
		}
		status, e := h.saveRunResult(job, id, secret.Email, result)
		if e != nil {
			h.failRun(id, e)
			return
		}
		_, _ = h.db.ExecContext(job, "UPDATE workspace_relogin_accounts SET last_used_at=NOW() WHERE email=$1", secret.Email)
		if status == "success" && apply {
			if e = h.applyRun(job, id); e != nil {
				h.failApplyRun(id, e)
			}
		}
		h.pruneRuns(job)
	}()
	return id, nil
}

func (h *Handler) startRelogin(c *gin.Context) {
	var p struct {
		loginSecret
		AccountID *int64 `json:"account_id"`
		UseSaved  bool   `json:"use_saved"`
		Apply     bool   `json:"apply"`
	}
	if err := body(c, &p, 2<<20); err != nil {
		fail(c, err)
		return
	}
	email, err := normalizeEmail(p.Email)
	if err != nil {
		fail(c, err)
		return
	}
	p.Email = email
	if p.UseSaved {
		p.loginSecret, err = h.loadSecret(c.Request.Context(), email)
		if err != nil {
			fail(c, err)
			return
		}
	}
	id, err := h.queueRelogin(c.Request.Context(), p.loginSecret, p.AccountID, "manual", p.Apply)
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"id": id, "status": "running"})
}

func (h *Handler) resumeRelogin(c *gin.Context) {
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
	if run.Status != "challenge" {
		fail(c, badRequest("这个任务当前没有等待验证码。"))
		return
	}
	var p struct {
		EmailCode    string `json:"email_code"`
		PhoneNumber  string `json:"phone_number"`
		PhoneCode    string `json:"phone_code"`
		PhoneChannel string `json:"phone_channel"`
	}
	if err = body(c, &p, 4096); err != nil {
		fail(c, err)
		return
	}
	result, err := h.runResult(run)
	if err != nil {
		fail(c, err)
		return
	}
	challenge, _ := result["challenge"].(map[string]any)
	challengeID := stringValue(challenge, "id")
	if challengeID == "" {
		fail(c, badRequest("验证会话已失效，请重新登录。"))
		return
	}
	if !h.acquireRun(run.Email) {
		fail(c, &requestError{409, "这个账号正在处理，请稍后再试。"})
		return
	}
	_, err = h.db.ExecContext(c.Request.Context(), "UPDATE workspace_relogin_runs SET status='running',error_message='',finished_at=NULL WHERE id=$1 AND status='challenge'", id)
	if err != nil {
		h.releaseRun(run.Email)
		fail(c, err)
		return
	}
	go func() {
		defer h.releaseRun(run.Email)
		ctx, cancel := context.WithTimeout(h.background, 29*time.Minute)
		defer cancel()
		payload := map[string]any{"action": "resume", "challenge_id": challengeID}
		for key, value := range map[string]string{"email_code": p.EmailCode, "phone_number": p.PhoneNumber, "phone_code": p.PhoneCode, "phone_channel": p.PhoneChannel} {
			if len(value) > 128 {
				h.failRun(id, badRequest("验证内容过长。"))
				return
			}
			if value != "" {
				payload[key] = value
			}
		}
		result, e := requestSession(ctx, payload)
		if e != nil {
			h.failRun(id, e)
			return
		}
		_, e = h.saveRunResult(ctx, id, run.Email, result)
		if e != nil {
			h.failRun(id, e)
		}
	}()
	response.Success(c, gin.H{"id": id, "status": "running"})
}

func (h *Handler) exchangeToken(ctx context.Context, token string, proxyID *int64, clientID string) (map[string]any, error) {
	token = strings.TrimSpace(token)
	if token == "" || len(token) > 16384 || strings.ContainsAny(token, " \t\r\n") {
		return nil, badRequest("请粘贴完整的 Refresh Token。")
	}
	proxyURL := ""
	if proxyID != nil {
		proxy, err := h.admin.GetProxy(ctx, *proxyID)
		if err != nil {
			return nil, err
		}
		if proxy.Status != "active" || proxy.IsExpired(time.Now()) {
			return nil, badRequest("所选代理当前不可用，请先检查代理。")
		}
		proxyURL = proxy.URL()
	}
	info, err := h.oauth.RefreshTokenWithClientID(ctx, token, proxyURL, clientID)
	if err != nil {
		return nil, &requestError{502, "凭据刷新未完成，请检查 Refresh Token 和所选代理。"}
	}
	credentials := h.oauth.BuildAccountCredentials(info)
	if stringValue(credentials, "refresh_token") == "" {
		credentials["refresh_token"] = token
	}
	if stringValue(credentials, "access_token") == "" {
		return nil, &requestError{502, "授权服务没有返回有效凭据。"}
	}
	return credentials, nil
}

func (h *Handler) applyRun(ctx context.Context, id int64) error {
	run, err := h.loadRun(ctx, id)
	if err != nil {
		return err
	}
	if run.Applied {
		return nil
	}
	if run.Status != "success" && run.Status != "apply_failed" {
		return badRequest("只有登录成功的任务可以写回账号。")
	}
	result, err := h.runResult(run)
	if err != nil {
		return err
	}
	credentials := findCredentials(result)
	if credentials == nil {
		return badRequest("登录结果缺少有效凭据。")
	}
	settings, err := h.loadReloginSettings(ctx)
	if err != nil {
		return err
	}
	proxyID := settings.Template.ProxyID
	var existing *service.Account
	if run.AccountID != nil {
		if err = h.validateTargetAccount(ctx, run.Email, run.AccountID); err != nil {
			return err
		}
		existing, err = h.admin.GetAccount(ctx, *run.AccountID)
		if err != nil {
			return err
		}
		proxyID = existing.ProxyID
	}
	if run.Trigger != "refresh_token" && result["verified"] != true {
		credentials, err = h.exchangeToken(ctx, stringValue(credentials, "refresh_token"), proxyID, stringValue(credentials, "client_id"))
		if err != nil {
			return err
		}
	}
	email, err := normalizeEmail(stringValue(credentials, "email"))
	if err != nil || email != run.Email {
		return badRequest("授权结果邮箱与目标账号不一致，未写回。")
	}
	if existing != nil {
		credentials = service.SanitizeStoredCredentials("openai", service.MergeCredentials(existing.Credentials, credentials))
	}
	// 刷新可能轮换 RT；在写回前保存已验证的新凭据，重试和导出不能再使用旧令牌。
	raw, err := json.Marshal(map[string]any{"email": run.Email, "result": map[string]any{"credential": credentials, "verified": true}})
	if err != nil {
		return err
	}
	blob, err := h.encryptor.Encrypt(string(raw))
	if err != nil {
		return err
	}
	if _, err = h.db.ExecContext(ctx, "UPDATE workspace_relogin_runs SET result_blob=$2 WHERE id=$1", id, blob); err != nil {
		return err
	}
	var account *service.Account
	if existing != nil {
		merged := service.SanitizeStoredCredentials("openai", service.MergeCredentials(existing.Credentials, credentials))
		account, err = h.admin.UpdateAccount(ctx, existing.ID, &service.UpdateAccountInput{Credentials: merged})
		if err == nil {
			account, err = h.admin.ClearAccountError(ctx, existing.ID)
		}
	} else {
		accounts, e := h.allOpenAIAccounts(ctx)
		if e != nil {
			return e
		}
		for _, a := range accounts {
			if accountEmail(a) == email {
				return &requestError{409, "这个邮箱已有关联账号，请选择现有账号重新授权。"}
			}
		}
		note := "由原生失效重登创建"
		template := settings.Template
		account, err = h.admin.CreateAccount(ctx, &service.CreateAccountInput{Name: email, Notes: &note, Platform: "openai", Type: "oauth", Credentials: credentials, ProxyID: template.ProxyID, Concurrency: template.Concurrency, Priority: template.Priority, RateMultiplier: &template.RateMultiplier, GroupIDs: template.GroupIDs, SkipDefaultGroupBind: len(template.GroupIDs) == 0})
	}
	if err != nil {
		return err
	}
	if account == nil {
		return errors.New("empty account after authorization")
	}
	if _, err = h.db.ExecContext(ctx, "UPDATE workspace_relogin_runs SET account_id=$2 WHERE id=$1", id, account.ID); err != nil {
		return err
	}
	if h.invalidator != nil {
		if err = h.invalidator.InvalidateToken(ctx, account); err != nil {
			return err
		}
	}
	action := "applied"
	plan := strings.ToLower(stringValue(credentials, "plan_type"))
	if plan == "free" {
		if _, err = h.admin.SetAccountSchedulable(ctx, account.ID, false); err != nil {
			return err
		}
		action = "paused_free"
	}
	_, err = h.db.ExecContext(ctx, "UPDATE workspace_relogin_runs SET account_id=$2,status=$3,applied=TRUE,apply_action=$3,plan_type=$4,finished_at=NOW(),error_message='' WHERE id=$1", id, account.ID, action, plan)
	return err
}

func (h *Handler) applyRelogin(c *gin.Context) {
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
	if !h.acquireRun(run.Email) {
		fail(c, &requestError{409, "这个账号正在处理，请稍后再试。"})
		return
	}
	defer h.releaseRun(run.Email)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Minute)
	defer cancel()
	if err = h.applyRun(ctx, id); err != nil {
		h.failApplyRun(id, err)
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"applied": true})
}

func (h *Handler) importRefreshToken(c *gin.Context) {
	var p struct {
		RefreshToken string `json:"refresh_token"`
		AccountID    *int64 `json:"account_id"`
	}
	if err := body(c, &p, 32<<10); err != nil {
		fail(c, err)
		return
	}
	ctx := c.Request.Context()
	settings, err := h.loadReloginSettings(ctx)
	if err != nil {
		fail(c, err)
		return
	}
	proxyID := settings.Template.ProxyID
	if p.AccountID != nil {
		a, e := h.admin.GetAccount(ctx, *p.AccountID)
		if e != nil {
			fail(c, e)
			return
		}
		proxyID = a.ProxyID
	}
	credentials, err := h.exchangeToken(ctx, p.RefreshToken, proxyID, "")
	if err != nil {
		fail(c, err)
		return
	}
	email, err := normalizeEmail(stringValue(credentials, "email"))
	if err != nil {
		fail(c, badRequest("刷新结果未包含账号邮箱，未创建账号。"))
		return
	}
	if err = h.validateTargetAccount(ctx, email, p.AccountID); err != nil {
		fail(c, err)
		return
	}
	if p.AccountID == nil {
		accounts, e := h.allOpenAIAccounts(ctx)
		if e != nil {
			fail(c, e)
			return
		}
		for _, a := range accounts {
			if accountEmail(a) == email {
				selected := a.ID
				p.AccountID = &selected
				break
			}
		}
	}
	if !h.acquireRun(email) {
		fail(c, &requestError{409, "这个账号正在处理，请稍后再试。"})
		return
	}
	defer h.releaseRun(email)
	var id int64
	err = h.db.QueryRowContext(ctx, "INSERT INTO workspace_relogin_runs(email,account_id,trigger) VALUES($1,$2,'refresh_token') RETURNING id", email, p.AccountID).Scan(&id)
	if err != nil {
		fail(c, err)
		return
	}
	_, err = h.saveRunResult(ctx, id, email, map[string]any{"credential": credentials})
	if err == nil {
		err = h.applyRun(ctx, id)
	}
	if err != nil {
		h.failApplyRun(id, err)
		fail(c, err)
		return
	}
	run, err := h.loadRun(ctx, id)
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, run)
}

func (h *Handler) pruneRuns(ctx context.Context) {
	_, _ = h.db.ExecContext(ctx, "DELETE FROM workspace_relogin_runs WHERE id IN (SELECT id FROM workspace_relogin_runs WHERE status NOT IN ('running','challenge') ORDER BY id DESC OFFSET 200)")
}
