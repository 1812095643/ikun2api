package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func accountModerationFixture(t *testing.T, mode string, exempt []int64) (*ContentModerationService, *contentModerationRuntimeSettingRepo) {
	t.Helper()
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = mode
	cfg.KeywordBlockingMode = ContentModerationKeywordModeKeywordOnly
	cfg.BlockedKeywords = []string{"blocked-content"}
	cfg.ExemptAccountIDs = exempt
	cfg.APIKeys = []string{"local-test-only-key"}
	cfg.EmailOnHit, cfg.AutoBanEnabled = false, false
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	settings := &contentModerationRuntimeSettingRepo{values: map[string]string{
		SettingKeyContentModerationConfig: string(raw), SettingKeyRiskControlEnabled: "true",
	}}
	svc := runtimeCacheTestService(settings, time.Hour)
	svc.asyncQueue = make(chan contentModerationTask, 20)
	return svc, settings
}

func TestAccountContentModerationExemptionDoesNotLeakAcrossFailover(t *testing.T) {
	svc, _ := accountModerationFixture(t, ContentModerationModePreBlock, []int64{11})
	input := runtimeCacheTestInput("blocked-content")
	var reports int
	ctx := svc.PrepareAccountModeration(context.Background(), input, func(_ *gin.Context, d *ContentModerationDecision) {
		require.True(t, d.Blocked)
		reports++
	})
	// 入站延迟审计不产生审计任务，且请求正文中的账号号无法代替真实选号。
	decision, err := svc.Check(ctx, input)
	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.Empty(t, svc.asyncQueue)
	require.NoError(t, CheckAccountContentModeration(ctx, nil, &Account{ID: 11}))
	require.Empty(t, svc.asyncQueue)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(ctx)
	err = CheckAccountContentModeration(ctx, c, &Account{ID: 22})
	var auditErr *AccountContentModerationError
	require.ErrorAs(t, err, &auditErr)
	require.True(t, auditErr.Decision.Blocked)
	require.Equal(t, 1, reports)
	require.Len(t, svc.asyncQueue, 1)
	// 嵌套协议适配或同账号重试只审核一次；原本免审的账号仍可免审。
	require.Error(t, CheckAccountContentModeration(ctx, nil, &Account{ID: 33}))
	require.Len(t, svc.asyncQueue, 1)
	require.NoError(t, CheckAccountContentModeration(ctx, nil, &Account{ID: 11}))
}

func TestAccountContentModerationExemptDoesNotCallAuditProvider(t *testing.T) {
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"category_scores":{"illicit":0.99}}]}`))
	}))
	defer upstream.Close()
	svc, settings := accountModerationFixture(t, ContentModerationModePreBlock, []int64{11})
	cfg := defaultContentModerationConfig()
	cfg.Enabled, cfg.Mode, cfg.KeywordBlockingMode = true, ContentModerationModePreBlock, ContentModerationKeywordModeAPIOnly
	cfg.ExemptAccountIDs, cfg.BaseURL, cfg.APIKeys = []int64{11}, upstream.URL, []string{"test-only-key"}
	cfg.AutoBanEnabled, cfg.EmailOnHit = false, false
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, settings.Set(context.Background(), SettingKeyContentModerationConfig, string(raw)))
	svc.httpClient = upstream.Client()
	input := runtimeCacheTestInput("plain content")
	ctx := svc.PrepareAccountModeration(context.Background(), input, nil)
	require.NoError(t, CheckAccountContentModeration(ctx, nil, &Account{ID: 11}))
	require.Zero(t, calls.Load())
	require.Empty(t, svc.asyncQueue)
	require.Error(t, CheckAccountContentModeration(ctx, nil, &Account{ID: 22}))
	require.Equal(t, int64(1), calls.Load())
	require.Len(t, svc.asyncQueue, 1)
}

func TestAccountContentModerationConfigurationPreservesAndClearsExemptions(t *testing.T) {
	svc, _ := accountModerationFixture(t, ContentModerationModePreBlock, []int64{11})
	_, err := svc.loadRuntimeSnapshot(context.Background())
	require.NoError(t, err)
	ids := []int64{22, 11, 22}
	view, err := svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{ExemptAccountIDs: &ids})
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{11, 22}, view.ExemptAccountIDs)
	view, err = svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{})
	require.NoError(t, err)
	require.Len(t, view.ExemptAccountIDs, 2)
	input := runtimeCacheTestInput("blocked-content")
	ctx := svc.PrepareAccountModeration(context.Background(), input, nil)
	require.NoError(t, CheckAccountContentModeration(ctx, nil, &Account{ID: 22}))

	empty := []int64{}
	view, err = svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{ExemptAccountIDs: &empty})
	require.NoError(t, err)
	require.Empty(t, view.ExemptAccountIDs)
	ctx = svc.PrepareAccountModeration(context.Background(), input, nil)
	require.False(t, accountModerationDeferred(ctx))
	// 已有长连接下一轮同样清除旧的延迟审核标记。
	oldCtx := context.WithValue(ctx, accountModerationContextKey{}, &accountModerationGate{})
	require.False(t, accountModerationDeferred(svc.PrepareAccountModeration(oldCtx, input, nil)))
	d, err := svc.Check(ctx, input)
	require.NoError(t, err)
	require.True(t, d.Blocked, "清空白名单后恢复入站审计")

	for _, invalid := range [][]int64{{0}, {-1}, {11, 0}} {
		_, err := svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{ExemptAccountIDs: &invalid})
		require.Error(t, err)
	}
}

func TestAccountContentModerationObserveStillAuditsNonExemptAccount(t *testing.T) {
	svc, _ := accountModerationFixture(t, ContentModerationModeObserve, []int64{11})
	ctx := svc.PrepareAccountModeration(context.Background(), runtimeCacheTestInput("blocked-content"), nil)
	require.NoError(t, CheckAccountContentModeration(ctx, nil, &Account{ID: 11}))
	require.Empty(t, svc.asyncQueue)
	require.NoError(t, CheckAccountContentModeration(ctx, nil, &Account{ID: 22}))
	require.Len(t, svc.asyncQueue, 1)
}

func TestAccountContentModerationGuardsForwardEntriesBeforeDependencies(t *testing.T) {
	svc, _ := accountModerationFixture(t, ContentModerationModePreBlock, []int64{11})
	ctx := svc.PrepareAccountModeration(context.Background(), runtimeCacheTestInput("blocked-content"), nil)
	account := &Account{ID: 22}
	cc, _ := gin.CreateTestContext(httptest.NewRecorder())
	cc.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(ctx)
	gw, oa, ga, ag := &GatewayService{}, &OpenAIGatewayService{}, &GeminiMessagesCompatService{}, &AntigravityGatewayService{}
	cases := []struct {
		name string
		run  func() error
	}{
		{"claude", func() error { _, err := gw.Forward(ctx, cc, account, nil); return err }},
		{"openai", func() error { _, err := oa.Forward(ctx, cc, account, nil); return err }},
		{"openai_messages", func() error { _, err := oa.ForwardAsAnthropic(ctx, cc, account, nil, "", ""); return err }},
		{"gemini", func() error { _, err := ga.Forward(ctx, cc, account, nil); return err }},
		{"gemini_native", func() error {
			_, err := ga.ForwardNative(ctx, cc, account, "gemini-test", "generateContent", false, nil)
			return err
		}},
		{"antigravity", func() error { _, err := ag.Forward(ctx, cc, account, nil, false); return err }},
		{"images", func() error { _, err := oa.ForwardImages(ctx, cc, account, nil, nil, ""); return err }},
		{"count_tokens", func() error { return oa.ForwardResponsesInputTokens(ctx, cc, account, nil) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var target *AccountContentModerationError
			require.True(t, errors.As(tc.run(), &target), "必须在凭据、转换、发包之前阻断")
		})
	}
	require.Len(t, svc.asyncQueue, 1)
}

func TestAccountContentModerationRejectionDoesNotAffectUpstreamHealth(t *testing.T) {
	svc, _ := accountModerationFixture(t, ContentModerationModePreBlock, []int64{11})
	ctx := svc.PrepareAccountModeration(context.Background(), runtimeCacheTestInput("blocked-content"), nil)
	account := &Account{ID: 22}
	err := CheckAccountContentModeration(ctx, nil, account)
	require.Error(t, err)
	var gateway *OpenAIGatewayService
	require.False(t, gateway.ReportOpenAIAccountScheduleResult(account, "model-test", false, nil, err))
}

func TestAccountContentModerationExemptionAlsoSkipsCyberSideEffects(t *testing.T) {
	svc, _ := accountModerationFixture(t, ContentModerationModePreBlock, []int64{11})
	require.True(t, svc.IsAccountAuditExempt(context.Background(), 11))
	require.False(t, svc.IsAccountAuditExempt(context.Background(), 22))
	svc.RecordCyberPolicyEvent(context.Background(), CyberPolicyRecordInput{AccountID: 11, UserID: 7, Model: "test-model"})
	require.Empty(t, svc.repo.(*contentModerationTestRepo).snapshotLogs())
	require.Empty(t, svc.asyncQueue)
}
