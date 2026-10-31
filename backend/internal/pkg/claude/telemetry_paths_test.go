package claude

import "testing"

func TestClaudeTelemetryPathsKeepDiscardSeparateFromReconstruction(t *testing.T) {
	for _, path := range DiscardedTelemetryPaths() {
		if !IsTelemetryPath(path) || IsTelemetryBatchPath(path) {
			t.Errorf("discard-only telemetry path misclassified: %s", path)
		}
	}
	for _, path := range []string{"/v1/messages", "/v1/messages/count_tokens", "/api/v1/admin/settings", "/api/event_logging", "/api/v2/logs-other"} {
		if IsTelemetryPath(path) || IsTelemetryBatchPath(path) {
			t.Errorf("business path misclassified: %s", path)
		}
	}
	for _, path := range []string{"/api/event_logging/batch", "/api/event_logging/v2/batch", "/v1/api/event_logging/batch", "/v1/api/event_logging/v2/batch"} {
		if !IsTelemetryPath(path) || !IsTelemetryBatchPath(path) {
			t.Errorf("batch telemetry path misclassified: %s", path)
		}
	}
	if !IsTelemetryPath("/api/event_logging/future") || IsTelemetryBatchPath("/api/event_logging/future") {
		t.Fatal("unknown event versions must remain discard-only")
	}
}
