//go:build unit

package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/config"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/telemetry"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Recovery and provenance must coexist: successful recovery must not emit a
// user-visible failure; failed recovery reports the original rejected wire once.
func TestMergedSignatureRecoveryMonitoring(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, fail := range []bool{false, true} {
			t.Run(fmt.Sprintf("stream_%t/fail_%t", stream, fail), func(t *testing.T) {
				upstream := &thinkingRecoveryUpstream{failRepair: fail}
				cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
				svc := &GatewayService{cfg: cfg, httpUpstream: upstream, responseHeaderFilter: compileResponseHeaderFilter(cfg), rateLimitService: &RateLimitService{}, deferredService: &DeferredService{}, settingService: NewSettingService(&signatureBudgetSettings{}, cfg)}
				account := newAnthropicAPIKeyAccountForTest()
				raw, err := sjson.Set(thinkingRecoveryFixture, "stream", stream)
				require.NoError(t, err)
				body := []byte(raw)
				c, rec := newThinkingRecoveryContext(body, "merge-session", 7)
				hub := telemetry.New("test", "test", "test", 10, 100)
				ctx, tr := hub.Start(context.Background(), "merge-request", time.Now())
				tr.Update(domain.Observation{UserID: 4, KeyID: 7, AccountID: account.ID})
				c.Request = c.Request.WithContext(ctx)
				parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
				require.NoError(t, err)
				_, err = svc.Forward(ctx, c, account, parsed)
				require.Len(t, upstream.bodies, 2, "only one same-account repair")
				require.Contains(t, string(upstream.bodies[0]), "rejected-signature")
				require.NotContains(t, string(upstream.bodies[1]), "rejected-signature")
				failures := 0
				for _, event := range hub.Snapshot(0, time.Now()).Events {
					if event.Kind == "signature.rejected" {
						failures++
						require.Equal(t, float64(1), event.N("signature_forward_unchanged"))
						require.Equal(t, float64(1), event.N("signature_message_index"))
						require.NotEmpty(t, event.TaskID)
					}
				}
				if fail {
					require.Error(t, err)
					require.Equal(t, 400, rec.Code)
					require.Equal(t, 1, failures)
					require.Contains(t, rec.Body.String(), "Invalid `signature`")
				} else {
					require.NoError(t, err)
					require.Equal(t, 0, failures, "a recovered attempt is not a terminal failure")
					require.Contains(t, rec.Body.String(), "fresh-signature")
					follow := thinkingTraceFixture("fresh reasoning", "fresh-signature")
					trace := svc.beginClaudeThinkingTrace(c, account, "claude-opus-5-5", "22222222-2222-4222-8222-222222222222", follow)
					require.Equal(t, "matches_previous_upstream_response", trace.origin(gjson.Parse(`{"type":"thinking","thinking":"fresh reasoning","signature":"fresh-signature"}`)))
				}
			})
		}
	}
}
