package handler

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sync"
	"testing"
	"time"
)

func memoryHistoryBody() []byte {
	body := []byte(`{"model":"gpt-6.1-sol","tools":[{"type":"function","name":"create_thread","description":"automation_update codex_apps_open_page"}],"input":[{"type":"message","role":"user","content":"compaction_trigger \\u1234"},{"type":"function_call_output","call_id":"call_history","output":"`)
	body = append(body, bytes.Repeat([]byte("x"), 8<<20)...)
	return append(body, []byte(`"}]}`)...)
}

func BenchmarkMemoryHistoryBootstrap(b *testing.B) {
	body := memoryHistoryBody()
	for _, tc := range []struct {
		name string
		fn   func([]byte) ([]byte, bool)
	}{
		{"delegation", normalizeCodexDelegationBootstrap}, {"automation", normalizeCodexAutomationBootstrap}, {"page", normalizeCodexUntrustedContext},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(body)))
			for i := 0; i < b.N; i++ {
				out, changed := tc.fn(body)
				if changed || !bytes.Equal(out, body) {
					b.Fatal("history unexpectedly changed")
				}
			}
		})
	}
}

func TestMemoryHistoryBootstrapSkipsPromptMentions(t *testing.T) {
	if !runMemoryTestIsolated(t) {
		return
	}
	body := memoryHistoryBody()
	for _, fn := range []func([]byte) ([]byte, bool){normalizeCodexDelegationBootstrap, normalizeCodexAutomationBootstrap, normalizeCodexUntrustedContext} {
		allocs := testing.AllocsPerRun(2, func() {
			out, changed := fn(body)
			if changed || &out[0] != &body[0] {
				t.Fatal("normal history changed")
			}
		})
		if allocs > 4 {
			t.Fatalf("normal history allocates: %v", allocs)
		}
	}
}

func TestMemoryHistoryConcurrentBootstrap(t *testing.T) {
	if !runMemoryTestIsolated(t) {
		return
	}
	body := memoryHistoryBody()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, fn := range []func([]byte) ([]byte, bool){normalizeCodexDelegationBootstrap, normalizeCodexAutomationBootstrap, normalizeCodexUntrustedContext} {
				out, changed := fn(body)
				if changed || &out[0] != &body[0] {
					t.Error("concurrent history changed")
				}
			}
		}()
	}
	wg.Wait()
	runtime.ReadMemStats(&after)
	if after.TotalAlloc-before.TotalAlloc > 1<<20 {
		t.Fatalf("concurrent no-op processing copied history: %d bytes", after.TotalAlloc-before.TotalAlloc)
	}
}

// Allocation counters are process-wide. Run memory assertions in a fresh copy
// of this test binary so background workers from unrelated tests cannot alter
// the result. The child keeps race instrumentation and executes no other tests.
func runMemoryTestIsolated(t *testing.T) bool {
	t.Helper()
	if os.Getenv("IKUN_MEMORY_TEST_CHILD") == t.Name() {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 125*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+regexp.QuoteMeta(t.Name())+"$", "-test.count=1", "-test.timeout=120s", "-test.v")
	cmd.Env = append(os.Environ(), "IKUN_MEMORY_TEST_CHILD="+t.Name())
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("isolated allocation check: %v\n%s", err, output)
	}
	t.Logf("%s", output)
	return false
}
