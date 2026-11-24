package telemetry

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Opt-in controlled load experiment. This isolates instrumentation overhead;
// it is not a production throughput benchmark or a substitute for P6.
func TestLoadBudget(t *testing.T) {
	if os.Getenv("MONITOR_LOAD_CHECK") != "1" {
		t.Skip("opt-in load experiment")
	}
	gin.SetMode(gin.TestMode)
	const n = 1600
	const workers = 16
	run := func(enabled bool) (time.Duration, time.Duration) {
		r := gin.New()
		if enabled {
			r.Use(New("test", "load", "bench", 4096, 8192).Middleware())
		}
		r.POST("/chat", func(c *gin.Context) {
			time.Sleep(10 * time.Millisecond)
			c.Header("Content-Type", "text/event-stream")
			c.Writer.WriteString("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n")
		})
		latencies := make([]time.Duration, n)
		var index atomic.Int64
		var wg sync.WaitGroup
		start := time.Now()
		for worker := 0; worker < workers; worker++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					i := int(index.Add(1) - 1)
					if i >= n {
						return
					}
					start := time.Now()
					r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/chat", nil))
					latencies[i] = time.Since(start)
				}
			}()
		}
		wg.Wait()
		elapsed := time.Since(start)
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		return elapsed, latencies[n*95/100]
	}
	var disabled, enabled, baseP95, enabledP95 []time.Duration
	for i := 0; i < 8; i++ {
		var d, e, dp, ep time.Duration
		if i%2 == 0 {
			d, dp = run(false)
			e, ep = run(true)
		} else {
			e, ep = run(true)
			d, dp = run(false)
		}
		disabled = append(disabled, d)
		enabled = append(enabled, e)
		baseP95 = append(baseP95, dp)
		enabledP95 = append(enabledP95, ep)
	}
	median := func(x []time.Duration) time.Duration {
		sort.Slice(x, func(i, j int) bool { return x[i] < x[j] })
		return (x[3] + x[4]) / 2
	}
	d, e := median(disabled), median(enabled)
	overhead := median(enabledP95) - median(baseP95)
	regression := 100 * (1 - float64(d)/float64(e))
	t.Logf("mock upstream=10ms concurrency=%d samples=%d per mode; baseline=%s enabled=%s; P95 delta=%s throughput regression=%.3f%%", workers, n*8, d, e, overhead, regression)
	if overhead > 5*time.Millisecond || regression > 2 {
		t.Fatalf("local instrumentation budget exceeded")
	}
}
