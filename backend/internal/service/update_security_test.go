//go:build unit

package service

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUpdateReleaseSignatureRejectsTampering(t *testing.T) {
	fixture := []byte("IKUN-release-test-fixture\n")
	sig := []byte("Bu/nEjEw5QoArMjnSrqMxCDvMNPFcUmV1RZZunlZz2F5ePZbJwBpAZLVGvLv36RKXe6s6zn1Zu1BKjTdf+McCw==")
	require.NoError(t, verifyReleaseSignature(fixture, sig))
	require.Error(t, verifyReleaseSignature([]byte("tampered"), sig))
	require.Error(t, verifyReleaseSignature(fixture, []byte("missing")))
}
func TestUpdateDownloadURLBoundToRepository(t *testing.T) {
	require.NoError(t, validateDownloadURL("https://github.com/1812095643/ikun2api/releases/download/v1.3.16/checksums.txt"))
	for _, u := range []string{"http://github.com/1812095643/ikun2api/releases/download/v1/a", "https://github.com/other/repo/releases/download/v1/a", "https://evil.github.com/1812095643/ikun2api/releases/download/v1/a", "https://github.com@evil.invalid/1812095643/ikun2api/releases/download/v1/a", "https://github.com/1812095643/ikun2api/releases/download/v1/a?next=x", "https://github.com/1812095643/ikun2api/releases/download/v1/../a"} {
		require.Error(t, validateDownloadURL(u), u)
	}
}
func TestUpdateVersionRetainsLegacyIKUNPrefix(t *testing.T) {
	require.Equal(t, -1, compareVersions("ikun1.3.15", "1.3.16"))
	require.Equal(t, 0, compareVersions("v1.3.16", "1.3.16"))
}
func TestUpdateBackupPreservesDataAndDoesNotFollowSymlink(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "backup")
	require.NoError(t, os.WriteFile(filepath.Join(src, "config.yaml"), []byte("fixture"), 0600))
	require.NoError(t, backupUpgradeConfig(src, dst))
	b, e := os.ReadFile(filepath.Join(dst, "config.yaml"))
	require.NoError(t, e)
	require.Equal(t, "fixture", string(b))
}
func TestUpdateMaintenanceDrainsAndRejectsNewRequests(t *testing.T) {
	EndUpgradeMaintenance()
	t.Cleanup(EndUpgradeMaintenance)
	require.True(t, EnterUpgradeRequest())
	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		done <- BeginUpgradeMaintenance(ctx)
	}()
	require.Eventually(t, func() bool { upgradeMu.Lock(); defer upgradeMu.Unlock(); return upgradeState.Active }, time.Second, time.Millisecond)
	require.False(t, EnterUpgradeRequest())
	LeaveUpgradeRequest()
	require.NoError(t, <-done)
	EndUpgradeMaintenance()
	require.True(t, EnterUpgradeRequest())
	LeaveUpgradeRequest()
}
func TestUpdateExtractRejectsArchiveSymlinkAndNestedBinary(t *testing.T) {
	for _, hdr := range []*tar.Header{{Name: "ikun2api", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"}, {Name: "nested/ikun2api", Typeflag: tar.TypeReg, Size: 1}} {
		path := filepath.Join(t.TempDir(), "release.tar.gz")
		f, e := os.Create(path)
		require.NoError(t, e)
		gz := gzip.NewWriter(f)
		tw := tar.NewWriter(gz)
		require.NoError(t, tw.WriteHeader(hdr))
		if hdr.Size > 0 {
			_, e = tw.Write([]byte("x"))
			require.NoError(t, e)
		}
		require.NoError(t, tw.Close())
		require.NoError(t, gz.Close())
		require.NoError(t, f.Close())
		svc := &UpdateService{}
		require.Error(t, svc.extractBinary(path, filepath.Join(t.TempDir(), "out")))
	}
}
