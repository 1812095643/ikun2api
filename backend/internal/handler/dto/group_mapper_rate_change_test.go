package dto

import (
	"encoding/json"
	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestGroupMapper_PriceChange(t *testing.T) {
	previous := 0.5
	changed := time.Date(2026, 11, 6, 8, 0, 0, 0, time.UTC)
	dto := GroupFromService(&service.Group{RateMultiplier: 0.8, PreviousRateMultiplier: &previous, RateMultiplierChangedAt: &changed})
	body, err := json.Marshal(dto)
	require.NoError(t, err)
	require.Contains(t, string(body), `"previous_rate_multiplier":0.5`)
	require.Contains(t, string(body), `"rate_multiplier_changed_at":"2026-11-06T08:00:00Z"`)
	empty, err := json.Marshal(GroupFromService(&service.Group{RateMultiplier: 0.8}))
	require.NoError(t, err)
	require.NotContains(t, string(empty), "previous_rate_multiplier")
	require.NotContains(t, string(empty), "rate_multiplier_changed_at")
}
