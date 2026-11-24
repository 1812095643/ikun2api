package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/requesttiming"
	"github.com/stretchr/testify/require"
)

func TestConnectionTraceCursorRoundTrip(t *testing.T) {
	at := time.Date(2026, 11, 23, 1, 2, 3, 123456000, time.UTC)
	id := "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	cursor := EncodeConnectionCursor(at, id)
	gotAt, gotID, err := DecodeConnectionCursor(cursor)
	require.NoError(t, err)
	require.Equal(t, at, gotAt)
	require.Equal(t, id, gotID)
	for _, raw := range []string{"!", strings.Repeat("A", 200), EncodeConnectionCursor(at, "not-a-uuid")} {
		_, _, err := DecodeConnectionCursor(raw)
		require.Error(t, err)
	}
}

func TestConnectionTraceQueueIsBoundedAndSamplesOnlySuccess(t *testing.T) {
	sink := &connectionTraceSink{queue: make(chan ConnectionTrace, 1)}
	s := &OpsService{connectionTraces: sink}
	detail := &requesttiming.Snapshot{Version: 1}
	s.RecordConnectionTrace(ConnectionTrace{TraceID: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", State: "completed", Detail: detail})
	first := <-sink.queue
	require.Nil(t, first.Detail)
	s.RecordConnectionTrace(ConnectionTrace{State: "failed", Detail: detail})
	s.RecordConnectionTrace(ConnectionTrace{State: "failed", Detail: detail})
	require.Equal(t, uint64(1), sink.health().Dropped)
	require.NotNil(t, (<-sink.queue).Detail)
	sink.closed = true
	s.RecordConnectionTrace(ConnectionTrace{State: "failed"})
	require.Empty(t, sink.queue)
	require.Equal(t, uint64(2), sink.health().Dropped)
}

func TestConnectionTraceRequiresEnabledMonitoring(t *testing.T) {
	s := &OpsService{}
	require.False(t, s.ConnectionDiagnosticsEnabled(context.Background()))
	_, err := s.ListConnectionTraces(context.Background(), ConnectionTraceFilter{})
	require.Error(t, err)
}
