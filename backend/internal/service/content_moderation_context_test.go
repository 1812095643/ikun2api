package service

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContentModerationScoresRequirePresentPositiveFiniteValue(t *testing.T) {
	for _, scores := range []map[string]float64{nil, {}, {"sexual": 0}, {"sexual": math.NaN()}, {"sexual": math.Inf(1)}, {"sexual": -1}, {"sexual": 1.1}} {
		flagged, _, _ := evaluateModerationScores(scores, map[string]float64{"sexual": 0})
		require.False(t, flagged)
	}
	flagged, category, score := evaluateModerationScores(map[string]float64{"harassment": 0.99, "illicit": 0.96}, map[string]float64{"harassment": 1, "illicit": 0.95})
	require.True(t, flagged)
	require.Equal(t, "illicit", category)
	require.Equal(t, 0.96, score)
	flagged, _, _ = evaluateModerationScores(map[string]float64{"sexual": 0.1}, nil)
	require.False(t, flagged)
}

func TestContentModerationHashPolicyIsolation(t *testing.T) {
	cfg := defaultContentModerationConfig()
	original := cfg.flaggedCacheKey("input")
	cfg.APIKeys = []string{"rotated-key"}
	require.Equal(t, original, cfg.flaggedCacheKey("input"))
	cfg.Thresholds["illicit"] = 0.99
	require.NotEqual(t, original, cfg.flaggedCacheKey("input"))
	cfg = defaultContentModerationConfig()
	cfg.Engine = "typesafe"
	require.NotEqual(t, original, cfg.flaggedCacheKey("input"))
}
