package claude

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// 对照实际客户端采集的公开环境字段，不从系统 Node 版本推测内置运行时。
func TestLocalEnvironmentHeadersMatchNativeCapture(t *testing.T) {
	raw, err := os.ReadFile("testdata/claude-linux-2.1.299-headers.json")
	if err != nil {
		t.Fatal(err)
	}
	var captured struct {
		Headers map[string]string `json:"headers"`
	}
	if err := json.Unmarshal(raw, &captured); err != nil {
		t.Fatal(err)
	}
	nativeProduct := strings.Fields(captured.Headers["User-Agent"])[0]
	nativeVersion := strings.TrimPrefix(nativeProduct, "claude-cli/")
	// 声明版本通过原有运维配置固定，不改变 Sub6 的最低兼容版本规则。
	withCLIVersionResolver(t, func() string { return nativeVersion })
	got := DefaultHeaders()
	for _, key := range []string{"X-Stainless-Lang", "X-Stainless-Package-Version", "X-Stainless-OS", "X-Stainless-Arch", "X-Stainless-Runtime", "X-Stainless-Runtime-Version", "Accept-Encoding"} {
		if captured.Headers[key] == "" || got[key] != captured.Headers[key] {
			t.Errorf("header %s differs: got %q, native %q", key, got[key], captured.Headers[key])
		}
	}
	if !strings.HasPrefix(got["User-Agent"], nativeProduct+" ") {
		t.Errorf("CLI version differs: got %q, native %q", got["User-Agent"], captured.Headers["User-Agent"])
	}
	// 采集使用非交互 sdk-cli 入口，网关继续保留 Sub6 原有的交互 CLI 模拟契约。
	if !strings.HasSuffix(got["User-Agent"], " (external, cli)") {
		t.Fatal("the upstream CLI entrypoint contract changed")
	}
}
