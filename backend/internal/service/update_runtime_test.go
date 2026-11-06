//go:build unit

package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func runtimeFixture(t *testing.T, path, version string, preflightOK bool) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("real executable fixtures require Linux shell")
	}
	code := 0
	if !preflightOK {
		code = 17
	}
	content := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"--upgrade-preflight\" ]; then exit %d; fi\nprintf '%%s INFO IKUN2api %s (commit: fixture, built: fixture)\\n' \"$(date +%%s%%N)\"\n", code, version)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0755))
}

func TestPersistentRuntimeColdStartAndLegacyMarkerCannotDowngrade(t *testing.T) {
	root := t.TempDir()
	image := filepath.Join(root, "image")
	directory := filepath.Join(root, "runtime")
	runtimeFixture(t, image, "1.3.49", true)
	binary, err := PreparePersistentRuntime(context.Background(), image, directory, "1.3.49")
	require.NoError(t, err)
	first, err := UpgradeFileHash(image)
	require.NoError(t, err)
	copied, err := UpgradeFileHash(binary)
	require.NoError(t, err)
	require.Equal(t, first, copied)
	runtimeFixture(t, binary, "1.3.52", true)
	before, err := UpgradeFileHash(binary)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(directory, ".image-version"), []byte("old timestamp INFO IKUN2api 1.3.49"), 0600))
	// Repeated starts of an old image must preserve a newer web-installed file.
	for n := 0; n < 3; n++ {
		selected, err := PreparePersistentRuntime(context.Background(), image, directory, "1.3.49")
		require.NoError(t, err)
		require.Equal(t, binary, selected)
	}
	after, err := UpgradeFileHash(binary)
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.NoFileExists(t, UpgradeJournalPath(binary))
}

func TestPersistentRuntimeNewerImageUsesSharedReplacementAndRecoveryJournal(t *testing.T) {
	root := t.TempDir()
	image := filepath.Join(root, "image")
	directory := filepath.Join(root, "runtime")
	binary := filepath.Join(directory, "ikun2api")
	runtimeFixture(t, image, "1.3.53", true)
	runtimeFixture(t, binary, "1.3.52", true)
	before, err := UpgradeFileHash(binary)
	require.NoError(t, err)
	selected, err := PreparePersistentRuntime(context.Background(), image, directory, "1.3.53")
	require.NoError(t, err)
	require.Equal(t, binary, selected)
	journal, err := ReadUpgradeJournal(binary)
	require.NoError(t, err)
	require.Equal(t, before, journal.PreviousSHA256)
	retained, err := UpgradeFileHash(filepath.Join(journal.BackupDirectory, "program"))
	require.NoError(t, err)
	require.Equal(t, before, retained)
	info, err := os.Stat(journal.PreviousBinary)
	require.NoError(t, err)
	require.NotZero(t, info.Mode()&0111)
	actual, err := inspectRuntimeVersion(context.Background(), binary)
	require.NoError(t, err)
	require.Equal(t, "1.3.53", actual)
	// Pending recovery always wins over another image, even if newer.
	_, err = PreparePersistentRuntime(context.Background(), image, directory, "9.0.0")
	require.NoError(t, err)
	after, err := ReadUpgradeJournal(binary)
	require.NoError(t, err)
	require.Equal(t, journal.CandidateSHA256, after.CandidateSHA256)
}

func TestPersistentRuntimeFailedPreflightDoesNotReplaceExistingFile(t *testing.T) {
	root := t.TempDir()
	image := filepath.Join(root, "image")
	directory := filepath.Join(root, "runtime")
	binary := filepath.Join(directory, "ikun2api")
	runtimeFixture(t, image, "1.3.53", false)
	runtimeFixture(t, binary, "1.3.52", true)
	before, err := UpgradeFileHash(binary)
	require.NoError(t, err)
	selected, err := PreparePersistentRuntime(context.Background(), image, directory, "1.3.53")
	require.NoError(t, err)
	require.Equal(t, binary, selected)
	after, err := UpgradeFileHash(binary)
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.NoFileExists(t, UpgradeJournalPath(binary))
}

func TestPersistentRuntimeSameVersionDifferentImageDoesNotOverwrite(t *testing.T) {
	root := t.TempDir()
	image := filepath.Join(root, "image")
	directory := filepath.Join(root, "runtime")
	binary := filepath.Join(directory, "ikun2api")
	runtimeFixture(t, image, "1.3.52", true)
	runtimeFixture(t, binary, "1.3.52", false)
	before, err := UpgradeFileHash(binary)
	require.NoError(t, err)
	_, err = PreparePersistentRuntime(context.Background(), image, directory, "1.3.52")
	require.NoError(t, err)
	after, err := UpgradeFileHash(binary)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestPersistentRuntimeUnknownExecutableIsNeverOverwritten(t *testing.T) {
	root := t.TempDir()
	image := filepath.Join(root, "image")
	directory := filepath.Join(root, "runtime")
	binary := filepath.Join(directory, "ikun2api")
	runtimeFixture(t, image, "1.3.53", true)
	runtimeFixture(t, binary, "1.3.52bad", true)
	before, err := UpgradeFileHash(binary)
	require.NoError(t, err)
	_, err = PreparePersistentRuntime(context.Background(), image, directory, "1.3.53")
	require.Error(t, err)
	after, err := UpgradeFileHash(binary)
	require.NoError(t, err)
	require.Equal(t, before, after)
}
