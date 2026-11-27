package detectors

import (
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/stretchr/testify/require"
)

func TestUpstreamHTTPStatusMatchesRuleCategory(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 429, 500, 501, 502, 503, 504, 529, 599} {
		now := time.Now()
		o := observation("attempt.failed", now)
		o.UpstreamStatus = status
		findings := Evaluate([]domain.Observation{o}, now, domain.DefaultConfig())
		serverFailure := status == 429 || status >= 500 && status <= 599
		require.Equal(t, serverFailure, hasRule(findings, "U01"), "status=%d", status)
		require.Equal(t, !serverFailure, hasRule(findings, "U02"), "status=%d", status)
	}
}
