//go:build unit

package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MACOS-DO/sub4api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type systemBackupStub struct {
	calls  int
	policy service.UpgradeBackupPolicy
}

func (s *systemBackupStub) GetUpgradeBackups() (*service.UpgradeBackupStatus, error) {
	return &service.UpgradeBackupStatus{Policy: service.UpgradeBackupPolicy{AutoCleanup: true, Keep: 3}}, nil
}
func (s *systemBackupStub) ConfigureUpgradeBackups(policy service.UpgradeBackupPolicy) (*service.UpgradeBackupStatus, error) {
	s.calls++
	s.policy = policy
	return &service.UpgradeBackupStatus{Policy: policy}, nil
}

func TestSystemHandlerUpgradeBackupPolicyValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.EndUpgradeMaintenance()
	service.SetUpgradePhase("idle", "")
	t.Cleanup(func() { service.EndUpgradeMaintenance(); service.SetUpgradePhase("idle", "") })
	repo := newMemoryIdempotencyRepoStub()
	handler := NewSystemHandler(&systemHandlerUpdateServiceStub{}, service.NewSystemOperationLockService(repo, service.IdempotencyConfig{ProcessingTimeout: time.Second, SystemOperationTTL: time.Minute}))
	stub := &systemBackupStub{}
	handler.backupSvc = stub
	router := gin.New()
	router.GET("/backups", handler.GetUpgradeBackups)
	router.PUT("/backups", handler.ConfigureUpgradeBackups)
	for _, body := range []string{`{}`, `{"keep":3}`, `{"auto_cleanup":true,"keep":0}`, `{"auto_cleanup":true,"keep":31}`, `{"auto_cleanup":true,"keep":2.5}`} {
		req := httptest.NewRequest(http.MethodPut, "/backups", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusBadRequest, rec.Code)
	}
	require.Zero(t, stub.calls)
	req := httptest.NewRequest(http.MethodPut, "/backups", strings.NewReader(`{"auto_cleanup":false,"keep":3}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 1, stub.calls)
	require.Equal(t, service.UpgradeBackupPolicy{AutoCleanup: false, Keep: 3}, stub.policy)
	service.SetUpgradePhase("downloading", "")
	req = httptest.NewRequest(http.MethodPut, "/backups", strings.NewReader(`{"auto_cleanup":true,"keep":1}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusConflict, rec.Code)
	require.Equal(t, 1, stub.calls)
}
