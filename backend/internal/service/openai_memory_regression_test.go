package service

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"testing"
	"time"
)

func memoryHistoryBody() []byte {
	body := []byte(`{"model":"gpt-6.1-sol","store":false,"input":[{"type":"message","content":"compaction_trigger \\u1234"},{"type":"function_call_output","call_id":"call_history","output":"`)
	body = append(body, bytes.Repeat([]byte("x"), 8<<20)...)
	return append(body, []byte(`"}]}`)...)
}
func BenchmarkMemoryHistoryCompaction(b *testing.B) {
	body := memoryHistoryBody()
	b.ReportAllocs()
	b.SetBytes(int64(len(body)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, changed, err := NormalizeCompactionTriggerInputOrder(body)
		if err != nil || changed || !bytes.Equal(out, body) {
			b.Fatal("unexpected change", err)
		}
		if HasCompactionTriggerInInput(body) {
			b.Fatal("false trigger")
		}
	}
}

func TestMemoryHistoryCompactionSkipsPromptMentions(t *testing.T) {
	if !runMemoryTestIsolated(t) {
		return
	}
	body := memoryHistoryBody()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := 0; i < 3; i++ {
		out, changed, err := NormalizeCompactionTriggerInputOrder(body)
		if err != nil || changed || &out[0] != &body[0] || HasCompactionTriggerInInput(body) {
			t.Fatal("normal history changed", err)
		}
	}
	runtime.ReadMemStats(&after)
	// Bytes catch whole-history copies; unrelated background test goroutines
	// can change allocation counts without growing with the request size.
	if after.TotalAlloc-before.TotalAlloc > 1<<20 {
		t.Fatalf("normal history copied: %d bytes", after.TotalAlloc-before.TotalAlloc)
	}

}

func TestCompactionStructuralScanPreservesDecoderFallback(t *testing.T) {
	for _, body := range []string{
		`{"input":[{"type":"compaction_trigger","type":"message"}]}`,
		`{"input":[{"type":"message","type":"compaction_trigger"}]}`,
		`{"input":[],"input":[{"type":"compaction_trigger"}]}`,
		`{"input":[{"type":"compaction_trigger"}],"input":[]}`,
		`{"input":[{"type":"compaction_trigg\u0065r"},{"type":"message","content":"x"}]}`,
	} {
		if !compactionTriggerNeedsDecode([]byte(body)) {
			t.Fatalf("lost ambiguous/escaped trigger: %s", body)
		}
		if _, _, err := NormalizeCompactionTriggerInputOrder([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	for _, body := range []string{`{"input":[]} trailing`, `{"input":[}`, `[]`} {
		if _, _, err := NormalizeCompactionTriggerInputOrder([]byte(body)); err == nil {
			t.Fatalf("lost validation: %s", body)
		}
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
