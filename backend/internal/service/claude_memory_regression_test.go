package service

import (
	"bytes"
	"crypto/sha256"
	"runtime"
	"testing"

	"github.com/tidwall/gjson"
)

func largeClaudeMemoryBody(size int, control bool) []byte {
	body := []byte(`{"model":"claude-opus-5-5","max_tokens":1024,"messages":[`)
	if control {
		body = append(body, []byte(`{"role":"system","content":[],"output_config":{"effort":"high"}},`)...)
	}
	body = append(body, []byte(`{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"`)...)
	body = append(body, bytes.Repeat([]byte("A"), size)...)
	return append(body, []byte(`"}}]},{"role":"assistant","content":[{"type":"thinking","thinking":"synthetic thinking","signature":"synthetic-signature"}]}]}`)...)
}
func BenchmarkClaudeMemorySanitizers(b *testing.B) {
	body := largeClaudeMemoryBody(8<<20, false)
	control := largeClaudeMemoryBody(8<<20, true)
	for _, tc := range []struct {
		name string
		fn   func()
	}{
		{"privacy", func() {
			if _, err := sanitizeClaudePrivacyBody(body); err != nil {
				b.Fatal(err)
			}
		}},
		{"output_config", func() {
			if _, changed := stripAnthropicMessageOutputConfigUnlessBeta(control, ""); !changed {
				b.Fatal("control not removed")
			}
		}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tc.fn()
			}
		})
	}
}
func TestClaudeMemorySanitizersPreserveSignedHistory(t *testing.T) {
	if !runMemoryTestIsolated(t) {
		return
	}
	body := largeClaudeMemoryBody(8<<20, false)
	beforeHash := sha256.Sum256(body)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	out, err := sanitizeClaudePrivacyBody(body)
	runtime.ReadMemStats(&after)
	if err != nil || !bytes.Equal(out, body) {
		t.Fatal("clean history changed", err)
	}
	if after.TotalAlloc-before.TotalAlloc > 1<<20 {
		t.Fatalf("privacy no-op copied history: %d", after.TotalAlloc-before.TotalAlloc)
	}
	if sha256.Sum256(body) != beforeHash {
		t.Fatal("privacy mutated caller bytes")
	}
	control := largeClaudeMemoryBody(8<<20, true)
	controlHash := sha256.Sum256(control)
	runtime.ReadMemStats(&before)
	out, changed := stripAnthropicMessageOutputConfigUnlessBeta(control, "")
	runtime.ReadMemStats(&after)
	if !changed || !gjson.ValidBytes(out) {
		t.Fatal("control removal failed")
	}
	if after.TotalAlloc-before.TotalAlloc > uint64(len(control)*2) {
		t.Fatalf("control removal copied history repeatedly: %d", after.TotalAlloc-before.TotalAlloc)
	}
	if !bytes.Equal(out, body) || sha256.Sum256(control) != controlHash {
		t.Fatal("control removal changed signed content or source bytes")
	}
}

func TestClaudeMemoryPrivacyChangedOutputOwnsStorage(t *testing.T) {
	body := largeClaudeMemoryBody(2<<20, false)
	body = append(append([]byte(nil), body[:len(body)-1]...), []byte(`,"client_private":"remove"}`)...)
	original := sha256.Sum256(body)
	out, err := sanitizeClaudePrivacyBody(body)
	if err != nil || gjson.GetBytes(out, "client_private").Exists() {
		t.Fatal("privacy filtering changed", err)
	}
	if !bytes.Equal(out, largeClaudeMemoryBody(2<<20, false)) {
		t.Fatal("opaque signed history changed")
	}
	out[0] = '['
	if sha256.Sum256(body) != original {
		t.Fatal("changed output aliases caller storage")
	}
}
