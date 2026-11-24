package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const OpenAIOAuthRecoveryExtraKey = "openai_oauth_recovery"
const OpenAIOAuthRejectedMessage = "OpenAI OAuth credentials permanently rejected; sign in again"

var ErrOpenAIOAuthRecoveryStateChanged = errOAuthRefreshAccountStateChanged

// OpenAIOAuthRecoveryRepository 只允许修改触发故障的凭据版本，避免迟到请求覆盖新授权。
type OpenAIOAuthRecoveryRepository interface {
	PauseOpenAIOAuthIfUnchanged(context.Context, *Account, string, bool) (bool, error)
	ApplyOpenAIOAuthReloginIfUnchanged(context.Context, *Account, map[string]any) (bool, error)
}

// OpenAIOAuthRecoveryFingerprint 不持有原始令牌；代理变化也会使在途重登结果失效。
func OpenAIOAuthRecoveryFingerprint(account *Account) string {
	if account == nil {
		return ""
	}
	raw, _ := json.Marshal([]any{openAIRefreshFingerprint(account), account.ProxyID})
	fingerprint := sha256.Sum256(raw)
	return hex.EncodeToString(fingerprint[:])
}

func IsOpenAIOAuthReloginAccount(account *Account) bool {
	return account != nil && account.IsOpenAIOAuth() && !account.IsShadow() &&
		!account.IsOpenAIPersonalAccessToken() && !account.IsOpenAIAgentIdentity() && !account.IsOpenAIBPS() &&
		!strings.HasPrefix(strings.TrimSpace(account.GetOpenAIAccessToken()), "at-")
}

func IsOpenAIOAuthCredentialError(message string) bool {
	message = strings.ToLower(message)
	for _, signal := range []string{"credentials permanently rejected", "token revoked", "token_expired", "token_invalidated", "invalidated oauth token", "invalid_grant", "refresh_token_expired", "refresh_token_reused", "refresh_token_invalidated", "refresh_token is missing", "unauthorized", "authentication failed"} {
		if strings.Contains(message, signal) {
			return true
		}
	}
	return false
}

func OpenAIOAuthRecoveryPending(account *Account) bool {
	if !IsOpenAIOAuthReloginAccount(account) || account.Status != StatusError {
		return false
	}
	if value, exists := account.Extra[OpenAIOAuthRecoveryExtraKey]; exists {
		state, ok := value.(map[string]any)
		if !ok || state["fingerprint"] != OpenAIOAuthRecoveryFingerprint(account) || state["reason"] != account.ErrorMessage {
			return false
		}
		if retry, ok := state["auto_retry"].(bool); ok && !retry {
			return false
		}
		return true
	}
	return IsOpenAIOAuthCredentialError(account.ErrorMessage)
}

type openAIOAuthAuthFailures struct {
	fingerprint string
	times       []time.Time
}

// 只在认证异常路径计数，每账号最多保存 100 个时间点；正常请求不访问数据库或计数器。
func (api *OAuthRefreshAPI) RecordOpenAIOAuthAuthFailure(account *Account, status int, body []byte) {
	if api == nil || !IsOpenAIOAuthReloginAccount(account) || !account.IsActive() || !account.Schedulable {
		return
	}
	code := strings.ToLower(extractUpstreamErrorCode(body))
	authCode := code == "token_expired" || code == "token_invalidated" || code == "token_revoked" || code == "invalid_token" || code == "authentication_error" || code == "unauthorized"
	if isCloudflareBotBlockResponse(body) || (status != http.StatusUnauthorized && !(status == http.StatusForbidden && authCode)) {
		return
	}
	api.openAIAuthMu.Lock()
	defer api.openAIAuthMu.Unlock()
	if api.openAIAuthFailures == nil {
		api.openAIAuthFailures = make(map[int64]openAIOAuthAuthFailures)
	}
	now := time.Now()
	fingerprint := OpenAIOAuthRecoveryFingerprint(account)
	if now.Sub(api.openAIAuthPrunedAt) >= time.Minute {
		for id, old := range api.openAIAuthFailures {
			if len(old.times) == 0 || !old.times[len(old.times)-1].After(now.Add(-time.Hour)) {
				delete(api.openAIAuthFailures, id)
			}
		}
		api.openAIAuthPrunedAt = now
	}
	state := api.openAIAuthFailures[account.ID]
	if state.fingerprint != fingerprint {
		state = openAIOAuthAuthFailures{fingerprint: fingerprint}
	}
	cutoff := now.Add(-time.Hour)
	first := 0
	for first < len(state.times) && !state.times[first].After(cutoff) {
		first++
	}
	state.times = append(state.times[first:], now)
	if len(state.times) > 100 {
		state.times = append([]time.Time(nil), state.times[len(state.times)-100:]...)
	}
	api.openAIAuthFailures[account.ID] = state
}

func (api *OAuthRefreshAPI) OpenAIOAuthAuthFailureCount(account *Account, window time.Duration) int {
	if api == nil || account == nil {
		return 0
	}
	api.openAIAuthMu.Lock()
	defer api.openAIAuthMu.Unlock()
	now := time.Now()
	state := api.openAIAuthFailures[account.ID]
	if state.fingerprint != OpenAIOAuthRecoveryFingerprint(account) {
		return 0
	}
	count := 0
	for _, at := range state.times {
		if at.After(now.Add(-window)) {
			count++
		}
	}
	return count
}

func (api *OAuthRefreshAPI) pauseRejectedOpenAIOAuth(ctx context.Context, account *Account) {
	if api == nil || account == nil || !account.IsActive() || !account.Schedulable {
		return
	}
	repo, ok := api.accountRepo.(OpenAIOAuthRecoveryRepository)
	if !ok {
		return
	}
	stateCtx, cancel := openAIAccountStateContext(ctx)
	defer cancel()
	if _, err := repo.PauseOpenAIOAuthIfUnchanged(stateCtx, account, OpenAIOAuthRejectedMessage, true); err != nil {
		slog.Warn("openai_oauth_rejection_pause_failed", "account_id", account.ID)
	}
}

func (api *OAuthRefreshAPI) PauseOpenAIOAuthForRelogin(ctx context.Context, account *Account, message string, autoRetry bool) (*Account, error) {
	if api == nil {
		return nil, errors.New("OpenAI OAuth recovery is unavailable")
	}
	repo, ok := api.accountRepo.(OpenAIOAuthRecoveryRepository)
	if !ok || !IsOpenAIOAuthReloginAccount(account) {
		return nil, errors.New("OpenAI OAuth recovery is unavailable")
	}
	applied, err := repo.PauseOpenAIOAuthIfUnchanged(ctx, account, message, autoRetry)
	if err != nil {
		return nil, err
	}
	if !applied {
		return nil, errOAuthRefreshAccountStateChanged
	}
	return api.accountRepo.GetByID(ctx, account.ID)
}

func (api *OAuthRefreshAPI) ApplyOpenAIOAuthRelogin(ctx context.Context, expected *Account, credentials map[string]any) (*Account, error) {
	if api == nil {
		return nil, errors.New("OpenAI OAuth recovery is unavailable")
	}
	repo, ok := api.accountRepo.(OpenAIOAuthRecoveryRepository)
	if !ok || !IsOpenAIOAuthReloginAccount(expected) {
		return nil, errors.New("OpenAI OAuth recovery is unavailable")
	}
	applied, err := repo.ApplyOpenAIOAuthReloginIfUnchanged(ctx, expected, credentials)
	if err != nil {
		return nil, err
	}
	if !applied {
		return nil, errOAuthRefreshAccountStateChanged
	}
	return api.accountRepo.GetByID(ctx, expected.ID)
}
