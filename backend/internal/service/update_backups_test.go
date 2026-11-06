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

func backupFixture(t *testing.T, binary string, order int, complete bool) string {
	t.Helper()
	directory := filepath.Join(filepath.Dir(binary), "upgrade-backups", time.Date(2026, 11, 1, 0, 0, order, 0, time.UTC).Format(upgradeBackupTimeLayout))
	require.NoError(t, os.MkdirAll(filepath.Join(directory, "data"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "data", "config.yaml"), []byte("fixture-only"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "database.sql"), []byte("SELECT 'fixture';"), 0600))
	hash, err := UpgradeFileHash(filepath.Join(directory, "database.sql"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(directory, "database.sql.sha256"), []byte(hash), 0600))
	if complete {
		require.NoError(t, os.WriteFile(filepath.Join(directory, "COMPLETE"), []byte("fixture"), 0600))
	}
	return directory
}

func TestUpgradeBackupsDefaultKeepsThreeVerifiedSnapshots(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "ikun2api")
	require.NoError(t, os.WriteFile(binary, []byte("current"), 0700))
	require.NoError(t, os.WriteFile(binary+".backup", []byte("previous"), 0700))
	dirs := make([]string, 0)
	for n := 0; n < 6; n++ {
		dirs = append(dirs, backupFixture(t, binary, n, true))
	}
	removed, err := CleanupUpgradeBackups(context.Background(), binary)
	require.NoError(t, err)
	require.Equal(t, 3, removed)
	for n, dir := range dirs {
		if n < 3 {
			require.NoDirExists(t, dir)
		} else {
			require.DirExists(t, dir)
		}
	}
	status, err := ReadUpgradeBackupStatus(binary)
	require.NoError(t, err)
	require.Equal(t, UpgradeBackupPolicy{AutoCleanup: true, Keep: 3}, status.Policy)
	require.Equal(t, 3, status.Count)
	require.Positive(t, status.Bytes)
	data, err := os.ReadFile(binary + ".backup")
	require.NoError(t, err)
	require.Equal(t, "previous", string(data))
}

func TestUpgradeBackupsKeepIncompleteUnknownAndCorruptWithoutDisplacingHealthy(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "ikun2api")
	dirs := []string{}
	for n := 0; n < 5; n++ {
		dirs = append(dirs, backupFixture(t, binary, n, true))
	}
	corrupt := backupFixture(t, binary, 6, true)
	require.NoError(t, os.WriteFile(filepath.Join(corrupt, "database.sql"), []byte("corrupt"), 0600))
	partial := backupFixture(t, binary, 7, false)
	manual := filepath.Join(filepath.Dir(binary), "upgrade-backups", "manual-important")
	require.NoError(t, os.MkdirAll(manual, 0700))
	removed, err := CleanupUpgradeBackups(context.Background(), binary)
	require.NoError(t, err)
	require.Equal(t, 2, removed)
	require.DirExists(t, corrupt)
	require.DirExists(t, partial)
	require.DirExists(t, manual)
	for _, dir := range dirs[2:] {
		require.DirExists(t, dir)
	}
}

func TestUpgradeBackupsDisabledAndActiveRecoveryNeverDelete(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "ikun2api")
	for n := 0; n < 6; n++ {
		backupFixture(t, binary, n, true)
	}
	require.NoError(t, SaveUpgradeBackupPolicy(binary, UpgradeBackupPolicy{AutoCleanup: false, Keep: 1}))
	removed, err := CleanupUpgradeBackups(context.Background(), binary)
	require.NoError(t, err)
	require.Zero(t, removed)
	status, err := ReadUpgradeBackupStatus(binary)
	require.NoError(t, err)
	require.Equal(t, 6, status.Count)
	require.NoError(t, SaveUpgradeBackupPolicy(binary, UpgradeBackupPolicy{AutoCleanup: true, Keep: 1}))
	// Even an invalid/interrupted journal must stop cleanup, not be ignored.
	require.NoError(t, os.WriteFile(UpgradeJournalPath(binary), []byte("interrupted"), 0600))
	removed, err = CleanupUpgradeBackups(context.Background(), binary)
	require.NoError(t, err)
	require.Zero(t, removed)
	require.NoError(t, os.Remove(UpgradeJournalPath(binary)))
	removed, err = CleanupUpgradeBackups(context.Background(), binary)
	require.NoError(t, err)
	require.Equal(t, 5, removed)
}

func TestUpgradeBackupsRejectInvalidPolicyAndFailClosedOnDamagedSettings(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "ikun2api")
	dir := backupFixture(t, binary, 1, true)
	for _, keep := range []int{-1, 0, 31} {
		require.Error(t, SaveUpgradeBackupPolicy(binary, UpgradeBackupPolicy{AutoCleanup: true, Keep: keep}))
	}
	root := filepath.Dir(dir)
	require.NoError(t, os.WriteFile(filepath.Join(root, "policy.json"), []byte("{invalid"), 0600))
	_, err := CleanupUpgradeBackups(context.Background(), binary)
	require.Error(t, err)
	require.DirExists(t, dir)
}

func TestUpgradeBackupsDoNotFollowSymlinkRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires Windows developer privileges")
	}
	root := t.TempDir()
	target := t.TempDir()
	binary := filepath.Join(root, "ikun2api")
	require.NoError(t, os.Symlink(target, filepath.Join(root, "upgrade-backups")))
	_, err := CleanupUpgradeBackups(context.Background(), binary)
	require.Error(t, err)
	require.DirExists(t, target)
}

func TestUpgradeBackupsMissingRootIsReadOnly(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "ikun2api")
	status, err := ReadUpgradeBackupStatus(binary)
	require.NoError(t, err)
	require.Zero(t, status.Count)
	_, err = CleanupUpgradeBackups(context.Background(), binary)
	require.NoError(t, err)
	require.NoDirExists(t, filepath.Join(filepath.Dir(binary), "upgrade-backups"))
}

func TestUpgradeBackupsRetainedProgramIsIndependentAndIntegrityChecked(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "ikun2api")
	require.NoError(t, os.WriteFile(binary+".backup", []byte("previous-version"), 0700))
	hash, err := UpgradeFileHash(binary + ".backup")
	require.NoError(t, err)
	snapshot := backupFixture(t, binary, 1, true)
	require.NoError(t, retainUpgradeExecutable(snapshot, binary+".backup", "1.3.51", "1.3.52", hash, "unused-candidate"))
	require.NoError(t, os.WriteFile(binary+".backup", []byte("another-version"), 0700))
	archived, err := UpgradeFileHash(filepath.Join(snapshot, "program"))
	require.NoError(t, err)
	require.Equal(t, hash, archived)
	entries, err := scanUpgradeBackups(filepath.Dir(snapshot))
	require.NoError(t, err)
	require.True(t, verifyUpgradeBackup(entries[0]))
	require.NoError(t, os.WriteFile(filepath.Join(snapshot, "program"), []byte("tampered"), 0700))
	require.False(t, verifyUpgradeBackup(entries[0]))
}
