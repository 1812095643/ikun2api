package runtime

// Storage alert hysteresis: alert at storageAlertPercent, recover only after
// storageClearSamples consecutive 30-second samples below storageClearPercent.
// Usage hovering around one threshold therefore cannot flap the M01 card.
const (
	storageAlertPercent = 80.0
	storageClearPercent = 70.0
	storageClearSamples = 3
)

type storageGate struct {
	Alert      bool  `json:"alert"`
	OKSamples  int   `json:"ok_samples"`
	LastBucket int64 `json:"last_bucket"`
}

// observe returns the observation kind to emit for this sample: "source.gap",
// "source.recovered", or "" while an alert waits for enough low samples.
func (g *storageGate) observe(usage float64, bucket int64) string {
	if usage >= storageAlertPercent {
		g.Alert, g.OKSamples = true, 0
		g.LastBucket = bucket
		return "source.gap"
	}
	if !g.Alert {
		return "source.recovered"
	}
	if usage >= storageClearPercent {
		g.OKSamples = 0
	} else if bucket != g.LastBucket {
		g.OKSamples++
	}
	g.LastBucket = bucket
	if g.OKSamples >= storageClearSamples {
		g.Alert, g.OKSamples = false, 0
		return "source.recovered"
	}
	return ""
}
