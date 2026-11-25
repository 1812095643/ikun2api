package runtime

import "testing"

func TestStorageGateHysteresis(t *testing.T) {
	g := &storageGate{}
	steps := []struct {
		usage  float64
		bucket int64
		want   string
	}{
		{50, 1, "source.recovered"},
		{81, 2, "source.gap"},
		{79, 3, ""}, // below 80 but not below 70: still alerting
		{81, 4, "source.gap"},
		{65, 5, ""},
		{65, 5, ""}, // same 30s bucket does not count twice
		{66, 6, ""},
		{72, 7, ""}, // back above 70 resets the run
		{60, 8, ""},
		{60, 9, ""},
		{60, 10, "source.recovered"},
		{60, 11, "source.recovered"},
		{85, 12, "source.gap"},
	}
	for n, s := range steps {
		if got := g.observe(s.usage, s.bucket); got != s.want {
			t.Fatalf("step %d usage %.0f: got %q want %q", n, s.usage, got, s.want)
		}
	}
}

// Usage oscillating around 80% must not produce repeated recoveries.
func TestStorageGateNoFlapAroundThreshold(t *testing.T) {
	g := &storageGate{}
	recovered := 0
	for b := int64(0); b < 200; b++ {
		usage := 79.5
		if b%2 == 0 {
			usage = 80.5
		}
		if g.observe(usage, b) == "source.recovered" && b > 0 {
			recovered++
		}
	}
	if recovered != 0 {
		t.Fatalf("flapped %d times", recovered)
	}
}
