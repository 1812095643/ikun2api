package service

import (
	"math"
	"strconv"
	"strings"
)

// Missing or invalid conversion rates never enable a fabricated CNY price.
func parseModelPlazaCreditsPerCNY(raw string) float64 {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return value
}
