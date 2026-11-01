//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUpdateMaintenanceNotificationResetsForRetry(t *testing.T) {
	EndUpgradeMaintenance()
	t.Cleanup(EndUpgradeMaintenance)
	first := UpgradeMaintenanceStarted()
	select {
	case <-first:
		t.Fatal("maintenance signaled before starting")
	default:
	}
	require.NoError(t, BeginUpgradeMaintenance(context.Background()))
	select {
	case <-first:
	default:
		t.Fatal("existing observers were not notified")
	}
	select {
	case <-UpgradeMaintenanceStarted():
	default:
		t.Fatal("late observer missed the active maintenance")
	}
	require.Error(t, BeginUpgradeMaintenance(context.Background()))
	EndUpgradeMaintenance()
	EndUpgradeMaintenance()
	second := UpgradeMaintenanceStarted()
	require.NotEqual(t, first, second)
	select {
	case <-second:
		t.Fatal("retry inherited the previous notification")
	default:
	}
	require.NoError(t, BeginUpgradeMaintenance(context.Background()))
	select {
	case <-second:
	default:
		t.Fatal("retry did not notify its observers")
	}
}

func TestUpdateMaintenanceTimeoutRetainsBusinessRequestProtection(t *testing.T) {
	EndUpgradeMaintenance()
	t.Cleanup(EndUpgradeMaintenance)
	require.True(t, EnterUpgradeRequest())
	defer LeaveUpgradeRequest()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	err := BeginUpgradeMaintenance(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Contains(t, err.Error(), "1 active")
	// A failed drain resumes admission without losing the outstanding request.
	require.True(t, EnterUpgradeRequest())
	LeaveUpgradeRequest()
	upgradeMu.Lock()
	active, requests := upgradeState.Active, upgradeState.Requests
	upgradeMu.Unlock()
	require.False(t, active)
	require.Equal(t, 1, requests)
}
