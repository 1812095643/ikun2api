//go:build unit

package service

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUpdatePrepareDoesNotDrainOrCancelBusinessRequests(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("candidate fixture uses the same shell entry as Linux releases")
	}
	EndUpgradeMaintenance()
	SetUpgradePhase("idle", "")
	t.Cleanup(func() { EndUpgradeMaintenance(); SetUpgradePhase("idle", "") })
	require.True(t, EnterUpgradeRequest())
	defer LeaveUpgradeRequest()
	candidate := filepath.Join(t.TempDir(), "candidate")
	require.NoError(t, os.WriteFile(candidate, []byte("#!/bin/sh\nexit 0\n"), 0700))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	backup := filepath.Join(t.TempDir(), "snapshot")
	require.NoError(t, NewUpgradeSafety().Prepare(ctx, candidate, backup))
	// Preparing a replacement does not touch or wait for the running request.
	require.True(t, EnterUpgradeRequest())
	LeaveUpgradeRequest()
	require.DirExists(t, backup)
}
