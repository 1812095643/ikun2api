package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type opsAccountLimitRepo struct {
	service.AccountRepository
	calls int
	err   error
	until map[int64]time.Time
}

func (r *opsAccountLimitRepo) SetRateLimited(_ context.Context, id int64, until time.Time) error {
	r.calls++
	if r.err == nil {
		if r.until == nil {
			r.until = make(map[int64]time.Time)
		}
		r.until[id] = until
	}
	return r.err
}

func TestOpsClientRestrictionIgnoreOnlyLocalGate(t *testing.T) {
	for _, tc := range []struct {
		name, settings string
		localGate      bool
		wantLogs       int64
	}{
		{"enabled", `{"ignore_client_restriction_errors":true}`, true, 0},
		{"disabled", `{"ignore_client_restriction_errors":false}`, true, 1},
		{"legacy_default", `{}`, true, 1},
		{"matching_upstream_message", `{"ignore_client_restriction_errors":true}`, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupOpsErrorLogTestQueue(t, 4)
			ops := service.NewOpsService(nil, &opsAdvancedSettingsRepoStub{advanced: tc.settings}, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			router := gin.New()
			router.Use(OpsErrorLoggerMiddleware(ops))
			router.POST("/v1/messages", func(c *gin.Context) {
				group := &service.Group{Platform: service.PlatformAnthropic, ClaudeCodeOnly: true, AllowClaudeDesktop: true}
				if tc.localGate {
					require.True(t, (&GatewayHandler{}).rejectClaudeClientRestriction(c, &service.APIKey{Group: group}))
					return
				}
				// An upstream can repeat the same text; the ignore switch must not hide it.
				service.SetOpsUpstreamError(c, 403, group.ClaudeClientRestrictionMessage(), "")
				c.JSON(403, gin.H{"error": gin.H{"type": "permission_error", "message": group.ClaudeClientRestrictionMessage()}})
			})
			request := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{}`))
			request.Header.Set(service.OpsClientRestrictionKey, "true")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			require.Equal(t, 403, recorder.Code, "忽略记录不能改变访问限制")
			require.Equal(t, tc.wantLogs, OpsErrorLogQueueLength())
		})
	}
}

func TestOpsHandled429AccountStateBeforeLogFiltering(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		writeError bool
		mixed400   bool
		stream     bool
		wantLogs   int64
	}{
		{"http_429", 429, false, false, false, 0},
		{"recovered_200", 200, false, false, false, 0},
		{"stream_429", 200, false, false, true, 0},
		{"persistence_failed", 429, true, false, false, 1},
		{"mixed_real_400", 429, false, true, false, 1},
		{"recovered_mixed_real_400", 200, false, true, false, 1},
		{"unrelated_terminal_500", 500, false, false, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupOpsErrorLogTestQueue(t, 4)
			ops := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			repo := &opsAccountLimitRepo{}
			if tc.writeError {
				repo.err = errors.New("state write failed")
			}
			limiter := service.NewRateLimitService(repo, nil, nil, nil, nil)
			router := gin.New()
			router.Use(OpsErrorLoggerMiddleware(ops))
			router.POST("/v1/messages", func(c *gin.Context) {
				events := []*service.OpsUpstreamErrorEvent{}
				if tc.mixed400 {
					events = append(events, &service.OpsUpstreamErrorEvent{AccountID: 4, UpstreamStatusCode: 400, Message: "invalid signature"})
				}
				for _, id := range []int64{7, 5, 6} {
					account := &service.Account{ID: id, Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth}
					limiter.HandleUpstreamError(c.Request.Context(), account, 429, http.Header{"Retry-After": {"120"}}, []byte(`{"error":{"type":"rate_limit_error","message":"Error"}}`), "claude-opus-5-5")
					events = append(events, &service.OpsUpstreamErrorEvent{AccountID: id, UpstreamStatusCode: 429, Message: "Error"})
				}
				c.Set(opsAccountIDKey, int64(6))
				c.Set(service.OpsUpstreamErrorsKey, events)
				service.SetOpsUpstreamError(c, 429, "Error", "")
				if tc.stream {
					service.MarkOpsStreamError(c, "rate_limit_error", "Upstream rate limit exceeded", 429)
					c.Status(200)
					return
				}
				c.JSON(tc.status, gin.H{"error": gin.H{"type": "rate_limit_error", "message": "Upstream rate limit exceeded"}})
			})
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/messages", nil))
			require.Equal(t, tc.status, recorder.Code)
			require.Equal(t, 3, repo.calls, "每次换号的限流状态都应处理")
			if !tc.writeError {
				require.Len(t, repo.until, 3)
				for _, until := range repo.until {
					require.Greater(t, time.Until(until), time.Minute, "不能把 Retry-After=120 缩成默认 5 秒")
				}
			}
			require.Equal(t, tc.wantLogs, OpsErrorLogQueueLength())
			if tc.mixed400 {
				job := <-opsErrorLogQueue
				require.NotNil(t, job.entry.UpstreamErrorsJSON)
				events, err := service.ParseOpsUpstreamErrors(*job.entry.UpstreamErrorsJSON)
				require.NoError(t, err)
				require.Len(t, events, 1)
				require.Equal(t, 400, *job.entry.UpstreamStatusCode)
				require.Equal(t, int64(4), *job.entry.AccountID)
			}
		})
	}
}

func TestOps429WithoutStateReceiptStillVisible(t *testing.T) {
	for _, upstream := range []bool{false, true} {
		setupOpsErrorLogTestQueue(t, 4)
		ops := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
		router := gin.New()
		router.Use(OpsErrorLoggerMiddleware(ops))
		router.POST("/v1/messages", func(c *gin.Context) {
			if upstream {
				c.Set(opsAccountIDKey, int64(6))
				service.SetOpsUpstreamError(c, 429, "Error", "")
			}
			c.JSON(429, gin.H{"error": gin.H{"type": "rate_limit_error", "message": "Too many pending requests"}})
		})
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/messages", nil))
		require.Equal(t, 429, recorder.Code)
		require.Equal(t, int64(1), OpsErrorLogQueueLength(), "没有状态落库凭据，不能仅凭 429 隐藏错误")
	}
}

func TestOps429ReceiptDoesNotLeakAcrossWebSocketTurns(t *testing.T) {
	setupOpsErrorLogTestQueue(t, 4)
	ops := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	limiter := service.NewRateLimitService(&opsAccountLimitRepo{}, nil, nil, nil, nil)
	router := gin.New()
	router.Use(OpsErrorLoggerMiddleware(ops))
	router.POST("/v1/responses", func(c *gin.Context) {
		service.SetOpenAIClientTransport(c, service.OpenAIClientTransportWS)
		for turn := 1; turn <= 2; turn++ {
			service.BeginOpsStreamTurn(c, turn)
			require.False(t, service.OpsAccountLimitHandled(c.Request.Context(), 6))
			if turn == 1 {
				limiter.HandleUpstreamError(c.Request.Context(), &service.Account{ID: 6, Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth}, 429, http.Header{}, nil)
			}
			c.Set(opsAccountIDKey, int64(6))
			c.Set(service.OpsUpstreamErrorsKey, []*service.OpsUpstreamErrorEvent{{AccountID: 6, UpstreamStatusCode: 429, Message: "Error"}})
			service.SetOpsUpstreamError(c, 429, "Error", "")
			service.MarkOpsStreamFailure(c, "rate_limit_error", "", "Upstream rate limit exceeded", 429)
		}
		c.Status(200)
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/responses", nil))
	require.Equal(t, 200, recorder.Code)
	require.Equal(t, int64(1), OpsErrorLogQueueLength(), "只过滤第一轮已经落库的限流，第二轮未处理的 429 必须保留")
}
