package claude

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestClaudeClientProfileMatchesCapturedCombination(t *testing.T) {
	p := EffectiveClientProfile()
	require.NotEmpty(t, p.ID)
	require.NotEmpty(t, p.Source)
	raw, err := os.ReadFile(filepath.Join("testdata", "claude-linux-"+p.CLIVersion+"-headers.json"))
	require.NoError(t, err)
	var capture struct {
		Headers map[string]string `json:"headers"`
	}
	require.NoError(t, json.Unmarshal(raw, &capture))
	got := DefaultHeaders()
	for _, name := range []string{"X-Stainless-Package-Version", "X-Stainless-Runtime-Version", "X-Stainless-OS", "X-Stainless-Arch", "X-Stainless-Runtime"} {
		require.Equal(t, capture.Headers[name], got[name], name)
	}
	raw, err = os.ReadFile(filepath.Join("..", "tlsfingerprint", "testdata", p.TLSSampleID+".bin"))
	require.NoError(t, err)
	require.NotEmpty(t, raw)
}
