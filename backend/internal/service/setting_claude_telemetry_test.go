package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestClaudeTelemetryUIOffCancelsRunningDelivery(t *testing.T) {
	t.Setenv("SUB6_CLAUDE_TELEMETRY_ENABLED", "true")
	settingsService := NewSettingService(&panelRateLimitSettingRepo{}, &config.Config{})
	transport := &telemetrySerialTransport{entered: make(chan telemetrySerialObservation, 1), proceed: make(chan struct{})}
	gateway := &GatewayService{settingService: settingsService, httpUpstream: transport}
	scheduler := NewClaudeTelemetryScheduler(gateway, &telemetrySchedulerKeyRepo{key: telemetrySchedulerKey()})
	defer scheduler.Stop()
	delivery := telemetryBatchDelivery(t, true)
	delivery.groupID = 0
	require.True(t, gateway.dispatchClaudeTelemetry(delivery))
	select {
	case <-transport.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("local test transport did not start")
	}
	settings := DefaultClaudeTelemetrySettings()
	settings.Enabled = false
	require.NoError(t, settingsService.SetClaudeTelemetrySettings(context.Background(), settings))
	require.Eventually(t, func() bool { return transport.active.Load() == 0 }, time.Second, 10*time.Millisecond)
}

func TestClaudeTelemetryConfiguredGapCannotBeBypassed(t *testing.T) {
	sender := claudeTelemetrySerialSender{slot: make(chan struct{}, 1)}
	release, err := sender.acquire(context.Background(), 4*time.Second)
	require.NoError(t, err)
	release()
	require.InDelta(t, 4, sender.nextAt.Sub(sender.lastFinished).Seconds(), 0.01)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = sender.acquire(ctx, 6*time.Second)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Zero(t, len(sender.slot))
}

func TestClaudeTelemetrySettingsDefaultsLegacyAndPersistence(t *testing.T) {
	t.Setenv("SUB6_CLAUDE_TELEMETRY_ENABLED", "")
	repo := &panelRateLimitSettingRepo{}
	s := NewSettingService(repo, &config.Config{})
	settings, err := s.GetClaudeTelemetrySettings(context.Background())
	require.NoError(t, err)
	require.Equal(t, ClaudeTelemetrySettings{true, 10, 2, 10}, settings)
	t.Setenv("SUB6_CLAUDE_TELEMETRY_ENABLED", "false")
	settings, err = s.GetClaudeTelemetrySettings(context.Background())
	require.NoError(t, err)
	require.False(t, settings.Enabled)
	settings.Enabled = true
	settings.FlushIntervalSeconds = 25
	require.NoError(t, s.SetClaudeTelemetrySettings(context.Background(), settings))
	require.Equal(t, settings, s.claudeTelemetrySettings(context.Background()))
	restarted := NewSettingService(repo, &config.Config{})
	stored, err := restarted.GetClaudeTelemetrySettings(context.Background())
	require.NoError(t, err)
	require.Equal(t, settings, stored, "UI choice must override the legacy environment and survive restart")
	for _, broken := range []ClaudeTelemetrySettings{{true, 0, 2, 10}, {true, 301, 2, 10}, {true, 10, 1, 10}, {true, 10, 61, 10}, {true, 10, 2, 2}, {true, 10, 2, 61}} {
		require.Error(t, s.SetClaudeTelemetrySettings(context.Background(), broken))
	}
	require.Equal(t, settings, s.claudeTelemetrySettings(context.Background()))
}

func TestClaudeTelemetryUnreadableSettingsNeverEnableDelivery(t *testing.T) {
	t.Setenv("SUB6_CLAUDE_TELEMETRY_ENABLED", "true")
	for _, repo := range []*panelRateLimitSettingRepo{
		{getValueErr: errors.New("db unavailable")},
		{values: map[string]string{SettingKeyClaudeTelemetry: `{"enabled":true,"flush_interval_seconds":-1}`}},
		{values: map[string]string{SettingKeyClaudeTelemetry: `not-json`}},
		{values: map[string]string{SettingKeyClaudeTelemetry: `null`}},
		{values: map[string]string{SettingKeyClaudeTelemetry: `{}`}},
		{values: map[string]string{SettingKeyClaudeTelemetry: `{"enabled":null,"flush_interval_seconds":10,"min_send_gap_seconds":2,"timeout_seconds":10}`}},
	} {
		s := NewSettingService(repo, &config.Config{})
		require.False(t, s.claudeTelemetrySettings(context.Background()).Enabled)
	}
}

func TestClaudeTelemetrySwitchCancelsAndDoesNotReplayOldSummaries(t *testing.T) {
	t.Setenv("SUB6_CLAUDE_TELEMETRY_ENABLED", "false")
	s := NewSettingService(&panelRateLimitSettingRepo{}, &config.Config{})
	gateway := &GatewayService{settingService: s, httpUpstream: &localClaudeTelemetryTransport{}}
	scheduler := NewClaudeTelemetryScheduler(gateway, &telemetrySchedulerKeyRepo{key: telemetrySchedulerKey()})
	defer scheduler.Stop()
	settings := DefaultClaudeTelemetrySettings()
	settings.Enabled = true
	settings.FlushIntervalSeconds = 27
	require.NoError(t, s.SetClaudeTelemetrySettings(context.Background(), settings))
	generation := scheduler.observationGeneration()
	ctx, ok := scheduler.beginDelivery(generation)
	require.True(t, ok)
	scheduler.inflight.Done()
	scheduler.remember(17, telemetryClientSession, claudeTelemetryDelivery{eventName: "tengu_api_success"}, generation)
	delay, pending := gateway.claudeTelemetry.nextDelay(time.Now())
	require.True(t, pending)
	require.InDelta(t, 27, delay.Seconds(), 0.5)
	settings.Enabled = false
	require.NoError(t, s.SetClaudeTelemetrySettings(context.Background(), settings))
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	_, pending = gateway.claudeTelemetry.nextDelay(time.Now())
	require.False(t, pending)
	settings.Enabled = true
	require.NoError(t, s.SetClaudeTelemetrySettings(context.Background(), settings))
	_, ok = scheduler.beginDelivery(generation)
	require.False(t, ok, "a task claimed before disable must not run after re-enable")
	scheduler.remember(17, telemetryClientSession, claudeTelemetryDelivery{eventName: "tengu_api_success"}, generation)
	_, pending = gateway.claudeTelemetry.nextDelay(time.Now())
	require.False(t, pending, "a stale observation must not be resurrected")
}

func TestClaudeTelemetryUIOffBlocksTimerIngressAndDirectSend(t *testing.T) {
	t.Setenv("SUB6_CLAUDE_TELEMETRY_ENABLED", "true")
	s := NewSettingService(&panelRateLimitSettingRepo{}, &config.Config{})
	settings := DefaultClaudeTelemetrySettings()
	settings.Enabled = false
	require.NoError(t, s.SetClaudeTelemetrySettings(context.Background(), settings))
	transport := &localClaudeTelemetryTransport{}
	gateway := &GatewayService{settingService: s, httpUpstream: transport}
	req, _ := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", nil)
	gateway.rememberClaudeTelemetry(telemetryTestContext(), telemetryClientSession, req, telemetryTestWire(), &Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth}, nil, nil, time.Now(), 200, 1, telemetryTestProxy, nil)
	require.Zero(t, gateway.RelayClaudeTelemetry(17, telemetryTestBatch(t, "tengu_api_success")))
	require.False(t, gateway.dispatchClaudeTelemetry(claudeTelemetryDelivery{}))
	_, _, err := gateway.sendClaudeTelemetry(context.Background(), claudeTelemetryDelivery{proxyURL: telemetryTestProxy})
	require.Error(t, err)
	require.Zero(t, transport.calls)
}
