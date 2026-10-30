package workspace

import (
	"context"
	"errors"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

func (h *Handler) failApplyRun(id int64, err error) {
	message := "授权已完成，写回账号暂未完成，可以重试写回。"
	var typed *requestError
	if errors.As(err, &typed) {
		message = typed.message
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = h.db.ExecContext(ctx, "UPDATE workspace_relogin_runs SET status='apply_failed',finished_at=NOW(),error_message=$2 WHERE id=$1", id, message)
}

func credentialExport(format string, run reloginRun, result map[string]any) (any, error) {
	credential := findCredentials(result)
	if credential == nil {
		return nil, badRequest("这条记录没有可导出的授权凭据。")
	}
	email := stringValue(credential, "email")
	if email != "" {
		normalized, err := normalizeEmail(email)
		if err != nil || normalized != run.Email {
			return nil, badRequest("授权结果与账号邮箱不一致，未导出。")
		}
	}
	accountID := stringValue(credential, "chatgpt_account_id")
	if accountID == "" {
		accountID = stringValue(credential, "account_id")
	}
	userID := stringValue(credential, "chatgpt_user_id")
	access, refresh, identity := stringValue(credential, "access_token"), stringValue(credential, "refresh_token"), stringValue(credential, "id_token")
	clientID, plan := stringValue(credential, "client_id"), stringValue(credential, "plan_type")
	tokens := map[string]any{"access_token": access, "refresh_token": refresh, "id_token": identity, "account_id": accountID}
	now := time.Now().UTC().Format(time.RFC3339)
	switch format {
	case "raw":
		return result, nil
	case "refresh_token":
		if refresh == "" {
			return nil, badRequest("这次授权没有返回 Refresh Token。")
		}
		return refresh, nil
	case "sub2api":
		return gin.H{"exported_at": now, "proxies": []any{}, "accounts": []any{gin.H{"name": run.Email, "platform": "openai", "type": "oauth", "credentials": credential, "extra": gin.H{"email": run.Email}}}}, nil
	case "codex":
		return gin.H{"auth_mode": "chatgpt", "OPENAI_API_KEY": nil, "tokens": tokens, "last_refresh": now}, nil
	case "cpa":
		return gin.H{"type": "codex", "account_id": accountID, "chatgpt_account_id": accountID, "email": run.Email, "name": run.Email, "plan_type": plan, "id_token": identity, "access_token": access, "refresh_token": refresh, "client_id": clientID, "last_refresh": now}, nil
	case "cockpit":
		return gin.H{"type": "codex", "id_token": identity, "access_token": access, "refresh_token": refresh, "client_id": clientID, "account_id": accountID, "last_refresh": now, "email": run.Email}, nil
	case "9router":
		return gin.H{"accessToken": access, "refreshToken": refresh, "clientId": clientID, "expiresAt": credential["expires_at"], "providerSpecificData": gin.H{"chatgptAccountId": accountID, "chatgptPlanType": plan, "chatgptUserId": userID}, "id": accountID, "provider": "codex", "authType": "oauth", "name": run.Email, "email": run.Email, "isActive": true, "createdAt": now, "updatedAt": now}, nil
	case "axonhub":
		if refresh == "" {
			return nil, badRequest("AxonHub 格式需要有效的 Refresh Token。")
		}
		return gin.H{"auth_mode": "chatgpt", "last_refresh": now, "tokens": gin.H{"access_token": access, "refresh_token": refresh, "id_token": identity}}, nil
	case "codexmanager":
		return gin.H{"tokens": tokens, "meta": gin.H{"label": run.Email, "chatgpt_account_id": accountID}}, nil
	case "session":
		return gin.H{"user": gin.H{"id": userID, "email": run.Email}, "account": gin.H{"id": accountID, "planType": plan}, "accessToken": access, "refreshToken": refresh, "idToken": identity, "clientId": clientID}, nil
	case "portable":
		return gin.H{"format": "session-studio-credentials", "provider": "openai", "type": "oauth", "name": run.Email, "email": run.Email, "account_id": accountID, "credentials": credential}, nil
	default:
		return nil, badRequest("请选择支持的导出格式。")
	}
}

func (h *Handler) exportRun(c *gin.Context) {
	id, err := positiveID(c)
	if err != nil {
		fail(c, err)
		return
	}
	var input struct {
		Format string `json:"format"`
	}
	if err = body(c, &input, 1024); err != nil {
		fail(c, err)
		return
	}
	run, err := h.loadRun(c.Request.Context(), id)
	if err != nil {
		fail(c, err)
		return
	}
	switch run.Status {
	case "success", "applied", "paused_free", "apply_failed":
	default:
		fail(c, badRequest("这次授权尚未成功。"))
		return
	}
	result, err := h.runResult(run)
	if err != nil {
		fail(c, err)
		return
	}
	export, err := credentialExport(input.Format, run, result)
	if err != nil {
		fail(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	response.Success(c, gin.H{"format": input.Format, "content": export})
}
