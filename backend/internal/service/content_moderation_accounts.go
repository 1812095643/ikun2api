package service

import (
	"context"
	"sync"

	infraerrors "github.com/MACOS-DO/sub4api/internal/pkg/errors"
	"github.com/gin-gonic/gin"
)

type accountModerationContextKey struct{}

type accountModerationGate struct {
	service  *ContentModerationService
	input    ContentModerationCheckInput
	inputMu  sync.Mutex
	exempt   map[int64]struct{}
	once     sync.Once
	decision *ContentModerationDecision
	report   func(*gin.Context, *ContentModerationDecision)
}

// 账号级豁免只能在实际选号后判断。未配置白名单时保留原来的入站审计顺序。
func (s *ContentModerationService) PrepareAccountModeration(ctx context.Context, input ContentModerationCheckInput, report func(*gin.Context, *ContentModerationDecision)) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	// WebSocket 新一轮不能继承上一轮的免审或审核通过结果。
	if accountModerationDeferred(ctx) {
		previous, _ := ctx.Value(accountModerationContextKey{}).(*accountModerationGate)
		if previous != nil {
			previous.inputMu.Lock()
			previous.input.Body = nil
			previous.inputMu.Unlock()
		}
		ctx = context.WithValue(ctx, accountModerationContextKey{}, nil)
	}
	if s == nil || s.repo == nil {
		return ctx
	}
	snapshot, err := s.loadRuntimeSnapshot(ctx)
	if err != nil || !snapshot.riskControlEnabled || !snapshot.config.Enabled || snapshot.config.Mode == ContentModerationModeOff || len(snapshot.config.ExemptAccountIDs) == 0 {
		return ctx
	}
	return context.WithValue(ctx, accountModerationContextKey{}, &accountModerationGate{
		service: s, input: input, exempt: snapshot.config.exemptAccounts, report: report,
	})
}

func accountModerationDeferred(ctx context.Context) bool {
	return ctx != nil && ctx.Value(accountModerationContextKey{}) != nil
}

func (s *ContentModerationService) IsAccountAuditExempt(ctx context.Context, accountID int64) bool {
	if s == nil || accountID <= 0 {
		return false
	}
	snapshot, err := s.loadRuntimeSnapshot(ctx)
	if err != nil {
		return false
	}
	_, exempt := snapshot.config.exemptAccounts[accountID]
	return exempt
}

type AccountContentModerationError struct {
	Decision *ContentModerationDecision
	err      error
}

func (e *AccountContentModerationError) Error() string { return e.err.Error() }
func (e *AccountContentModerationError) Unwrap() error { return e.err }

// 在转发入口执行，先于请求正文改写、凭据刷新及上游发包；切号不继承豁免结果。
func CheckAccountContentModeration(ctx context.Context, c *gin.Context, account *Account) error {
	if c != nil && c.Request != nil {
		ctx = c.Request.Context()
	}
	if ctx == nil {
		return nil
	}
	gate, _ := ctx.Value(accountModerationContextKey{}).(*accountModerationGate)
	if gate == nil {
		return nil
	}
	if account == nil || account.ID <= 0 {
		return infraerrors.New(503, "AUDIT_ACCOUNT_REQUIRED", "上游账号尚未就绪，请稍后重试")
	}
	if _, ok := gate.exempt[account.ID]; ok {
		return nil
	}
	gate.once.Do(func() {
		gate.inputMu.Lock()
		input := gate.input
		gate.inputMu.Unlock()
		// 去掉延迟标记，仅此次真实检查进入原有关键词、哈希、模型审计和副作用链路。
		checkCtx := context.WithValue(ctx, accountModerationContextKey{}, nil)
		gate.decision, _ = gate.service.Check(checkCtx, input)
		gate.inputMu.Lock()
		gate.input.Body = nil
		gate.inputMu.Unlock()
	})
	decision := gate.decision
	if decision == nil || !decision.Blocked {
		return nil
	}
	if gate.report != nil && c != nil {
		gate.report(c, decision)
	}
	status := decision.StatusCode
	if status < 400 || status > 599 {
		status = 403
	}
	return &AccountContentModerationError{Decision: decision,
		err: infraerrors.New(status, "content_policy_violation", decision.Message)}
}
