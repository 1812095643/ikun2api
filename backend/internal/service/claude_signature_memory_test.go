//go:build unit

package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"github.com/MACOS-DO/sub4api/internal/monitoring/telemetry"
	"github.com/tidwall/gjson"
	"runtime"
	"testing"
	"time"
)

func TestSignatureDiagnosticsDoNotCopyLargeUnrelatedHistory(t *testing.T) {
	if !runMemoryTestIsolated(t) {
		return
	}
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"image","source":{"data":"`)
	body = append(body, bytes.Repeat([]byte("A"), 8<<20)...)
	body = append(body, []byte(`"}}]},{"role":"assistant","content":[{"type":"thinking","thinking":"safe test","signature":"synthetic-signature"}]}]}`)...)
	before := sha256.Sum256(body)
	svc := &GatewayService{}
	account := newAnthropicOAuthAccountForPartialUsageTest()
	c, _ := newThinkingRecoveryContext(nil, "memory-test", 7)
	var memBefore, memAfter runtime.MemStats
	runtime.ReadMemStats(&memBefore)
	allocs := testing.AllocsPerRun(2, func() {
		trace := svc.beginClaudeThinkingTrace(c, account, "claude-opus-5-5", "memory-test", body)
		if trace == nil || len(trace.input) != 1 || len(claudeThinkingBlockDigests(body)) != 1 {
			t.Fatal("diagnostic evidence lost")
		}
	})
	runtime.ReadMemStats(&memAfter)
	if memAfter.TotalAlloc-memBefore.TotalAlloc > 1<<20 {
		t.Fatalf("diagnostics copied large history: %d bytes", memAfter.TotalAlloc-memBefore.TotalAlloc)
	}
	if allocs > 80 {
		t.Fatalf("unexpected diagnostic allocations: %v", allocs)
	}
	if sha256.Sum256(body) != before {
		t.Fatal("diagnostics changed request")
	}
}
func TestSignatureTraceSkipsOversizedBlock(t *testing.T) {
	block := gjson.Parse(`{"type":"thinking","thinking":"` + string(bytes.Repeat([]byte("x"), claudeThinkingDigestMaxBytes)) + `","signature":"synthetic"}`)
	if (&claudeThinkingTrace{}).origin(block) != "diagnostic_block_size_limit" {
		t.Fatal("oversized block must not be labeled invalid")
	}
	if _, _, ok := thinkingBlockTraceDigests(block); ok {
		t.Fatal("oversized diagnostic block must remain unclassified")
	}
}

func TestSignatureOversizedRejectionDoesNotCopyWireBlock(t *testing.T) {
	if !runMemoryTestIsolated(t) {
		return
	}
	body := thinkingTraceFixture(string(bytes.Repeat([]byte("x"), 8<<20)), "synthetic")
	c, _ := newThinkingRecoveryContext(body, "memory-limit", 7)
	hub := telemetry.New("test", "test", "test", 10, 20)
	ctx, _ := hub.Start(context.Background(), "request", time.Now())
	c.Request = c.Request.WithContext(ctx)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	monitorClaudeSignatureRejection(c, 1, "session:test", nil, &claudeThinkingTrace{}, body, []byte(signatureErrorFixture))
	runtime.ReadMemStats(&after)
	if after.TotalAlloc-before.TotalAlloc > 1<<20 {
		t.Fatal("oversized rejection copied request block")
	}
	events := hub.Snapshot(0, time.Now()).Events
	if len(events) != 2 || events[1].Kind != "signature.rejected" || events[1].N("signature_changed_in_forward") != 0 || events[1].N("signature_forward_unchanged") != 0 {
		t.Fatal("unsupported block produced a false attribution")
	}
}
