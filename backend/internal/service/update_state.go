package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

var upgradeMu sync.Mutex
var upgradeState = struct {
	Phase    string `json:"phase"`
	Error    string `json:"error,omitempty"`
	Active   bool   `json:"active"`
	Requests int    `json:"-"`
}{Phase: "idle"}

func SetUpgradePhase(phase, message string) {
	upgradeMu.Lock()
	defer upgradeMu.Unlock()
	if phase == "downloading" {
		if binary, err := os.Executable(); err == nil {
			_ = os.Remove(binary + ".upgrade-result.json")
		}
	}
	upgradeState.Phase = phase
	upgradeState.Error = message
}
func UpgradeStatus() any {
	upgradeMu.Lock()
	idle := upgradeState.Phase == "idle"
	upgradeMu.Unlock()
	if idle && upgradeMaintenanceMarkerExists() {
		return map[string]any{"phase": "restarting", "active": true}
	}

	if binary, err := os.Executable(); err == nil {
		if b, err := os.ReadFile(binary + ".upgrade-result.json"); err == nil {
			var result map[string]any
			if json.Unmarshal(b, &result) == nil {
				upgradeMu.Lock()
				idle := upgradeState.Phase == "idle"
				upgradeMu.Unlock()
				if idle {
					return result
				}
			}
		}
	}
	upgradeMu.Lock()
	defer upgradeMu.Unlock()
	return struct {
		Phase  string `json:"phase"`
		Error  string `json:"error,omitempty"`
		Active bool   `json:"active"`
	}{upgradeState.Phase, upgradeState.Error, upgradeState.Active}
}
func EnterUpgradeRequest() bool {
	upgradeMu.Lock()
	defer upgradeMu.Unlock()
	if upgradeState.Active || upgradeMaintenanceMarkerExists() {
		return false
	}
	upgradeState.Requests++
	return true
}
func LeaveUpgradeRequest() { upgradeMu.Lock(); defer upgradeMu.Unlock(); upgradeState.Requests-- }
func BeginUpgradeMaintenance(ctx context.Context) error {
	upgradeMu.Lock()
	if upgradeState.Active {
		upgradeMu.Unlock()
		return fmt.Errorf("upgrade maintenance already active")
	}
	upgradeState.Active = true
	upgradeMu.Unlock()
	for {
		upgradeMu.Lock()
		n := upgradeState.Requests
		upgradeMu.Unlock()
		if n == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			EndUpgradeMaintenance()
			return fmt.Errorf("existing requests did not drain; update stopped")
		case <-time.After(100 * time.Millisecond):
		}
	}
}
func EndUpgradeMaintenance() { upgradeMu.Lock(); defer upgradeMu.Unlock(); upgradeState.Active = false }

func upgradeMaintenanceMarkerExists() bool {
	binary, _ := os.Executable()
	if _, err := os.Stat(UpgradeJournalPath(binary)); err == nil {
		return true
	}
	path := os.Getenv("IKUN_MAINTENANCE_FILE")
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func UpgradeInProgress() bool {
	upgradeMu.Lock()
	defer upgradeMu.Unlock()
	return upgradeState.Active || upgradeMaintenanceMarkerExists() || upgradeState.Phase == "downloading" || upgradeState.Phase == "validating" || upgradeState.Phase == "restarting"
}
