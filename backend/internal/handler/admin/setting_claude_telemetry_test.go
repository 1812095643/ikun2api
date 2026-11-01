package admin

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type claudeTelemetrySettingsRepo struct {
	service.SettingRepository
	mu     sync.Mutex
	values map[string]string
	getErr error
}

func (r *claudeTelemetrySettingsRepo) GetValue(_ context.Context, key string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.getErr != nil {
		return "", r.getErr
	}
	value, ok := r.values[key]
	if !ok {
		return "", service.ErrSettingNotFound
	}
	return value, nil
}

func (r *claudeTelemetrySettingsRepo) Set(_ context.Context, key, value string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.values == nil {
		r.values = make(map[string]string)
	}
	r.values[key] = value
	return nil
}

func TestClaudeTelemetrySettingsRejectPartialOrInvalidPayloads(t *testing.T) {
	repo := &claudeTelemetrySettingsRepo{values: map[string]string{"site_name": "Keep this site"}}
	h := NewSettingHandler(service.NewSettingService(repo, nil), nil, nil, nil, nil, nil, nil)
	call := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings/claude-telemetry", bytes.NewBufferString(body))
		c.Request.Header.Set("Content-Type", "application/json")
		h.UpdateClaudeTelemetrySettings(c)
		return rec
	}
	for _, invalid := range []string{
		`{}`, `{"enabled":false}`, `{"enabled":null,"flush_interval_seconds":10,"min_send_gap_seconds":2,"timeout_seconds":10}`,
		`{"enabled":true,"flush_interval_seconds":10.5,"min_send_gap_seconds":2,"timeout_seconds":10}`,
		`{"enabled":true,"flush_interval_seconds":10,"min_send_gap_seconds":0,"timeout_seconds":10}`,
		`{"enabled":true,"flush_interval_seconds":301,"min_send_gap_seconds":2,"timeout_seconds":10}`,
	} {
		require.Equal(t, http.StatusBadRequest, call(invalid).Code, invalid)
		require.Empty(t, repo.values[service.SettingKeyClaudeTelemetry])
	}
	require.Equal(t, http.StatusOK, call(`{"enabled":false,"flush_interval_seconds":15,"min_send_gap_seconds":3,"timeout_seconds":10}`).Code)
	settings, err := h.settingService.GetClaudeTelemetrySettings(context.Background())
	require.NoError(t, err)
	require.Equal(t, service.ClaudeTelemetrySettings{Enabled: false, FlushIntervalSeconds: 15, MinSendGapSeconds: 3, TimeoutSeconds: 10}, settings)
	require.Equal(t, "Keep this site", repo.values["site_name"])
}

func TestClaudeTelemetrySettingsReadFailureDoesNotExposeEnabledDefault(t *testing.T) {
	t.Setenv("SUB6_CLAUDE_TELEMETRY_ENABLED", "true")
	repo := &claudeTelemetrySettingsRepo{getErr: errors.New("database unavailable")}
	h := NewSettingHandler(service.NewSettingService(repo, nil), nil, nil, nil, nil, nil, nil)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings/claude-telemetry", nil)
	h.GetClaudeTelemetrySettings(c)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.NotContains(t, rec.Body.String(), `"enabled":true`)
}
