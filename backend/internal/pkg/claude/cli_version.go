package claude

import (
	"log/slog"
	"os"
	"strings"

	"golang.org/x/mod/semver"
)

// CLIVersionEnv selects a reviewed profile by its CLI version. Unknown versions
// fall back to the captured default; changing only a version string is not an upgrade.
const CLIVersionEnv = "SUB2API_CLAUDE_CLI_VERSION"

// Resolve environment selection once per process.
var resolvedCLIVersion = resolveCLIVersion(os.Getenv(CLIVersionEnv))

// CLIVersion returns the reviewed environment/default profile version.
func CLIVersion() string {
	return resolvedCLIVersion
}

// IsSupportedCLIVersion validates release-discovery syntax and the historical
// protocol floor. It does not authorize a new client profile for outbound use.
func IsSupportedCLIVersion(version string) bool {
	version = strings.TrimSpace(version)
	if version == "" {
		return false
	}
	// semver 允许 "v1.2" 与预发布/构建元数据，这里都不接受：
	// Canonical 相等可排除省略段，再显式排除预发布与构建元数据。
	canonical := "v" + version
	if !semver.IsValid(canonical) || semver.Canonical(canonical) != canonical {
		return false
	}
	if semver.Prerelease(canonical) != "" || semver.Build(canonical) != "" {
		return false
	}
	return semver.Compare(canonical, "v"+CLICurrentVersion) >= 0
}

// resolveCLIVersion 把环境变量的原始值解析成可用的版本号。
// 空值静默回落（未配置是正常状态）；非空但不合法则回落并告警——
// 静默忽略一个显式配置会让运维以为已经生效。
func resolveCLIVersion(raw string) string {
	version := strings.TrimSpace(raw)
	if version == "" {
		return DefaultProfileVersion
	}
	if _, approved := ClientProfileForVersion(version); !approved {
		slog.Warn("ignoring invalid Claude CLI version override; falling back to the built-in pin",
			"env", CLIVersionEnv,
			"value", version,
			"builtin", DefaultProfileVersion,
			"requirement", "a reviewed client profile with HTTP and TLS capture evidence")
		return DefaultProfileVersion
	}
	return version
}
