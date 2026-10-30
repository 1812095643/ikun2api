package claude

// ClientProfile is a reviewed, indivisible client configuration. Request headers
// and release discovery cannot introduce a new combination. Add a profile only
// with matching HTTP and TLS capture fixtures.
type ClientProfile struct {
	ID             string
	CLIVersion     string
	SDKVersion     string
	OS             string
	Arch           string
	Runtime        string
	RuntimeVersion string
	TLSSampleID    string
	Source         string
}

const DefaultProfileVersion = "2.1.299"
const DefaultTLSSampleID = "claude-linux-2.1.299-clienthello"

func ClientProfileForVersion(version string) (ClientProfile, bool) {
	if version != DefaultProfileVersion {
		return ClientProfile{}, false
	}
	return ClientProfile{
		ID: "claude-linux-x64-2.1.299-r1", CLIVersion: DefaultProfileVersion,
		SDKVersion: StainlessPackageVersion, OS: StainlessOS, Arch: StainlessArch,
		Runtime: StainlessRuntime, RuntimeVersion: StainlessRuntimeVersion,
		TLSSampleID: DefaultTLSSampleID,
		Source:      "native-linux-capture-2.1.299",
	}, true
}

func EffectiveClientProfile() ClientProfile {
	p, _ := ClientProfileForVersion(EffectiveCLIVersion())
	return p
}
