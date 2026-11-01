package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

const SettingKeyClaudeTelemetry = "claude_telemetry_settings"

// ClaudeTelemetrySettings only controls delivery of server-observed summaries.
// Privacy filtering, ingress interception and concurrency=1 are unconditional.
type ClaudeTelemetrySettings struct {
	Enabled              bool `json:"enabled"`
	FlushIntervalSeconds int  `json:"flush_interval_seconds"`
	MinSendGapSeconds    int  `json:"min_send_gap_seconds"`
	TimeoutSeconds       int  `json:"timeout_seconds"`
}

type cachedClaudeTelemetrySettings struct {
	settings ClaudeTelemetrySettings
	expires  time.Time
}

func DefaultClaudeTelemetrySettings() ClaudeTelemetrySettings {
	settings := ClaudeTelemetrySettings{Enabled: true, FlushIntervalSeconds: 10, MinSendGapSeconds: 2, TimeoutSeconds: 10}
	// Existing installations keep their explicit legacy choice until saved in UI.
	if value := strings.TrimSpace(os.Getenv("SUB6_CLAUDE_TELEMETRY_ENABLED")); value != "" {
		settings.Enabled = strings.EqualFold(value, "true")
	}
	return settings
}

func (settings ClaudeTelemetrySettings) Validate() error {
	if settings.FlushIntervalSeconds < 5 || settings.FlushIntervalSeconds > 300 {
		return fmt.Errorf("定时发送延迟须为 5–300 秒")
	}
	if settings.MinSendGapSeconds < 2 || settings.MinSendGapSeconds > 60 {
		return fmt.Errorf("最小发送间隔须为 2–60 秒")
	}
	if settings.TimeoutSeconds < 3 || settings.TimeoutSeconds > 60 {
		return fmt.Errorf("发送超时须为 3–60 秒")
	}
	return nil
}

func (s *SettingService) GetClaudeTelemetrySettings(ctx context.Context) (ClaudeTelemetrySettings, error) {
	settings := DefaultClaudeTelemetrySettings()
	if s == nil || s.settingRepo == nil {
		return settings, nil
	}
	value, err := s.settingRepo.GetValue(ctx, SettingKeyClaudeTelemetry)
	if errors.Is(err, ErrSettingNotFound) || (err == nil && strings.TrimSpace(value) == "") {
		return settings, nil
	}
	if err != nil {
		return settings, err
	}
	var stored struct {
		Enabled              *bool `json:"enabled"`
		FlushIntervalSeconds *int  `json:"flush_interval_seconds"`
		MinSendGapSeconds    *int  `json:"min_send_gap_seconds"`
		TimeoutSeconds       *int  `json:"timeout_seconds"`
	}
	if err := json.Unmarshal([]byte(value), &stored); err != nil || stored.Enabled == nil || stored.FlushIntervalSeconds == nil || stored.MinSendGapSeconds == nil || stored.TimeoutSeconds == nil {
		settings = DefaultClaudeTelemetrySettings()
		settings.Enabled = false
		return settings, nil
	}
	settings = ClaudeTelemetrySettings{*stored.Enabled, *stored.FlushIntervalSeconds, *stored.MinSendGapSeconds, *stored.TimeoutSeconds}
	if err := settings.Validate(); err != nil {
		settings = DefaultClaudeTelemetrySettings()
		settings.Enabled = false
	}
	return settings, nil
}

func (s *SettingService) SetClaudeTelemetrySettings(ctx context.Context, settings ClaudeTelemetrySettings) error {
	if err := settings.Validate(); err != nil {
		return err
	}
	// Every persisted transition must reach the scheduler before the next write.
	// In particular, concurrent off/on saves cannot skip the cancellation step.
	s.claudeTelemetryUpdateMu.Lock()
	defer s.claudeTelemetryUpdateMu.Unlock()
	data, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	// Serialize DB writes and cache publication against refreshes.
	s.claudeTelemetrySettingsMu.Lock()
	if err = s.settingRepo.Set(ctx, SettingKeyClaudeTelemetry, string(data)); err == nil {
		s.claudeTelemetrySettingsCache.Store(&cachedClaudeTelemetrySettings{settings: settings, expires: time.Now().Add(5 * time.Second)})
	}
	listener := s.claudeTelemetrySettingsChanged
	s.claudeTelemetrySettingsMu.Unlock()
	if err == nil && listener != nil {
		listener(settings)
	}
	return err
}

func (s *SettingService) claudeTelemetrySettings(ctx context.Context) ClaudeTelemetrySettings {
	if s == nil || s.settingRepo == nil {
		return DefaultClaudeTelemetrySettings()
	}
	if cached := s.claudeTelemetrySettingsCache.Load(); cached != nil && time.Now().Before(cached.expires) {
		return cached.settings
	}
	s.claudeTelemetrySettingsMu.Lock()
	defer s.claudeTelemetrySettingsMu.Unlock()
	if cached := s.claudeTelemetrySettingsCache.Load(); cached != nil && time.Now().Before(cached.expires) {
		return cached.settings
	}
	if ctx == nil {
		ctx = context.Background()
	}
	dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	settings, err := s.GetClaudeTelemetrySettings(dbCtx)
	if err != nil {
		// Never enable outbound reporting on an unreadable/invalid configuration.
		settings = DefaultClaudeTelemetrySettings()
		settings.Enabled = false
	}
	s.claudeTelemetrySettingsCache.Store(&cachedClaudeTelemetrySettings{settings: settings, expires: time.Now().Add(5 * time.Second)})
	return settings
}

func (s *GatewayService) telemetrySettings() ClaudeTelemetrySettings {
	if s == nil {
		settings := DefaultClaudeTelemetrySettings()
		settings.Enabled = false
		return settings
	}
	return s.settingService.claudeTelemetrySettings(context.Background())
}
