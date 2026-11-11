package workspace

import (
	"context"
	"encoding/json"
	"github.com/DATA-DOG/go-sqlmock"
	"testing"

	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestReloginTemplateRestoresAdvancedDefaults(t *testing.T) {
	cfg := defaultReloginConfig()
	require.Equal(t, "full", cfg.Template.Extra.CodexFingerprintMode)
	require.Equal(t, service.OpenAIWSIngressModeCtxPool, cfg.Template.Extra.OpenAIWSMode)
	require.True(t, cfg.Template.Extra.OpenAIWSEnabled)
	require.True(t, cfg.Template.AutoPauseOnExpired)
	require.Equal(t, 10000, *cfg.Template.LoadFactor)
	require.NoError(t, normalizeReloginTemplate(&cfg.Template))
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.True(t, hasCompleteReloginTemplate(raw))
	require.False(t, hasCompleteReloginTemplate(json.RawMessage(`{"template":{"concurrency":100}}`)))
}

func TestReloginTemplateRoundTripAndCreateInput(t *testing.T) {
	cfg := defaultReloginConfig()
	require.NoError(t, json.Unmarshal([]byte(`{"template":{"auto_pause_on_expired":false,"load_factor":27,"extra":{"codex_fingerprint_mode":"device","openai_oauth_responses_websockets_v2_mode":"off"},"model_mapping":{"alias":"target"}}}`), &cfg))
	require.NoError(t, normalizeReloginTemplate(&cfg.Template))
	require.False(t, cfg.Template.Extra.OpenAIWSEnabled)
	source := map[string]any{"refresh_token": "synthetic", "email": "owner@example.test"}
	input := cfg.Template.createInput("owner@example.test", source)
	require.Equal(t, "device", input.Extra["codex_fingerprint_mode"])
	require.Equal(t, "off", input.Extra["openai_oauth_responses_websockets_v2_mode"])
	require.False(t, input.Extra["openai_oauth_responses_websockets_v2_enabled"].(bool))
	require.False(t, *input.AutoPauseOnExpired)
	require.Equal(t, 27, *input.LoadFactor)
	require.Equal(t, "target", input.Credentials["model_mapping"].(map[string]any)["alias"])
	require.NotContains(t, source, "model_mapping")
	require.NotContains(t, input.Extra, "privacy_mode")
}

func TestReloginTemplateRejectsInvalidOptions(t *testing.T) {
	for _, key := range []string{"fingerprint", "ws", "load", "mapping"} {
		t.Run(key, func(t *testing.T) {
			cfg := defaultReloginConfig()
			switch key {
			case "fingerprint":
				cfg.Template.Extra.CodexFingerprintMode = "unknown"
			case "ws":
				cfg.Template.Extra.OpenAIWSMode = "unknown"
			case "load":
				x := 10001
				cfg.Template.LoadFactor = &x
			case "mapping":
				cfg.Template.ModelMapping["alias"] = " "
			}
			require.Error(t, normalizeReloginTemplate(&cfg.Template))
		})
	}
}

type reloginTemplateTestCipher struct{}

func (reloginTemplateTestCipher) Encrypt(value string) (string, error) { return value, nil }
func (reloginTemplateTestCipher) Decrypt(value string) (string, error) { return value, nil }

func TestReloginTemplateRecoversArchivedSettingsWithoutOverwritingCurrentChoices(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	h := &Handler{db: db, encryptor: reloginTemplateTestCipher{}}
	oldTemplate := `{"proxy_id":6,"load_factor":42,"extra":{"codex_fingerprint_mode":"device","openai_oauth_responses_websockets_v2_mode":"off","openai_oauth_responses_websockets_v2_enabled":false},"model_mapping":{"alias":"target"}}`
	archive, err := json.Marshal(map[string]any{"runtime": []map[string]string{{"key": "account_template", "value": oldTemplate}}})
	require.NoError(t, err)
	envelope, err := json.Marshal(map[string]string{"ciphertext": string(archive)})
	require.NoError(t, err)
	current := []byte(`{"template":{"proxy_id":9,"concurrency":37,"group_ids":[2]}}`)
	mock.ExpectQuery("SELECT value FROM workspace_runtime WHERE key").WithArgs("relogin_settings").WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(current))
	mock.ExpectQuery("SELECT value FROM workspace_runtime WHERE key").WithArgs("migration43_legacy_relogin_archive").WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(envelope))
	cfg, err := h.loadReloginSettings(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(9), *cfg.Template.ProxyID)
	require.Equal(t, 37, cfg.Template.Concurrency)
	require.Equal(t, 42, *cfg.Template.LoadFactor)
	require.Equal(t, "device", cfg.Template.Extra.CodexFingerprintMode)
	require.Equal(t, service.OpenAIWSIngressModeCtxPool, cfg.Template.Extra.OpenAIWSMode)
	require.Equal(t, "target", cfg.Template.ModelMapping["alias"])
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestReloginCompleteTemplateSkipsLegacyArchive(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	cfg := defaultReloginConfig()
	cfg.Template.Extra.OpenAIWSMode = service.OpenAIWSIngressModeOff
	cfg.Template.Extra.OpenAIWSEnabled = false
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	mock.ExpectQuery("SELECT value FROM workspace_runtime WHERE key").WithArgs("relogin_settings").WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(raw))
	loaded, err := (&Handler{db: db}).loadReloginSettings(context.Background())
	require.NoError(t, err)
	require.Equal(t, service.OpenAIWSIngressModeOff, loaded.Template.Extra.OpenAIWSMode)
	require.False(t, loaded.Template.Extra.OpenAIWSEnabled)
	require.NoError(t, mock.ExpectationsWereMet())
}
