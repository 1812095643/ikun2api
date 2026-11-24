package workspace

import (
	"strings"

	"github.com/MACOS-DO/sub4api/internal/service"
)

func reloginResultMatchesAccount(run reloginRun, account *service.Account) bool {
	if !service.IsOpenAIOAuthReloginAccount(account) {
		return false
	}
	if run.AccountFingerprint == "" {
		// 升级前自动任务没有凭据版本，不能在重启后无条件覆盖新登录。
		return run.Trigger != "auto"
	}
	return run.AccountFingerprint == service.OpenAIOAuthRecoveryFingerprint(account)
}

// 只认结构化的明确结论；网络、验证码、风控挑战和通用报错不能被当成封号。
func reloginTerminalFailure(result map[string]any) string {
	if findCredentials(result) != nil || result["challenge"] != nil {
		return ""
	}
	var classify func(map[string]any, int) string
	classify = func(value map[string]any, depth int) string {
		if depth > 4 {
			return ""
		}
		for _, key := range []string{"code", "error_code", "error"} {
			switch strings.ToLower(stringValue(value, key)) {
			case "account_deactivated", "account_disabled", "user_deactivated", "account_banned", "organization_deactivated":
				return "OpenAI OAuth account_disabled：登录确认账号已停用，已暂停调度，请人工处理。"
			case "invalid_grant", "refresh_token_expired", "refresh_token_reused", "refresh_token_invalidated", "token_revoked", "token_invalidated", "invalid_credentials", "invalid_password", "incorrect_password":
				return "OpenAI OAuth credentials permanently rejected：登录资料或授权已失效，已暂停调度，请更新资料后重登。"
			}
		}
		for _, key := range []string{"error", "data", "payload", "result"} {
			if nested, ok := value[key].(map[string]any); ok {
				if message := classify(nested, depth+1); message != "" {
					return message
				}
			}
		}
		return ""
	}
	return classify(result, 0)
}
