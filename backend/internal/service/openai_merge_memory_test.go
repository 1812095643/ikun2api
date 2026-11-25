package service

import (
	"bytes"
	"runtime"
	"testing"
)

func mergedCompactionHistory(nativeLast, escaped bool) []byte {
	body := []byte(`{"input":[{"type":"message","role":"user","content":"compaction_trigger compaction_trigger `)
	if escaped {
		body = append(body, []byte(`\u0061 `)...)
	}
	body = append(body, bytes.Repeat([]byte("x"), 8<<20)...)
	body = append(body, []byte(`"}`)...)
	if nativeLast {
		body = append(body, []byte(`,{"type":"compaction_trigger"}`)...)
	}
	return append(body, []byte(`]}`)...)
}

// Cover the intersection of the local prompt-mention fix and upstream's native
// final-trigger fast path. Neither may bring whole-history decoding back.
func TestMemoryMergedCompactionFastPaths(t *testing.T) {
	if !runMemoryTestIsolated(t) {
		return
	}
	for _, nativeLast := range []bool{false, true} {
		for _, escaped := range []bool{false, true} {
			body := mergedCompactionHistory(nativeLast, escaped)
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			out, changed, err := NormalizeCompactionTriggerInputOrder(body)
			runtime.ReadMemStats(&after)
			if err != nil || changed || !bytes.Equal(out, body) || &out[0] != &body[0] {
				t.Fatalf("no-op changed: native=%v escaped=%v err=%v", nativeLast, escaped, err)
			}
			if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 1<<20 {
				t.Fatalf("history decoded/copied: native=%v escaped=%v bytes=%d", nativeLast, escaped, allocated)
			}
			if HasCompactionTriggerInInput(body) != nativeLast {
				t.Fatal("trigger detection disagrees with actual input items")
			}
		}
	}
}

func BenchmarkMergedCompactionFastPaths(b *testing.B) {
	for _, tc := range []struct {
		name                string
		nativeLast, escaped bool
	}{
		{"prompt_mentions", false, false},
		{"native_last", true, false},
		{"escaped_prompt_mentions", false, true},
		{"escaped_native_last", true, true},
	} {
		body := mergedCompactionHistory(tc.nativeLast, tc.escaped)
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(body)))
			for i := 0; i < b.N; i++ {
				out, changed, err := NormalizeCompactionTriggerInputOrder(body)
				if err != nil || changed || &out[0] != &body[0] {
					b.Fatal("unexpected normalization", err)
				}
			}
		})
	}
}
