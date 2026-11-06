package service

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

var runtimeVersionPattern = regexp.MustCompile(`IKUN2api ((?:v|ikun)?[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?)(?:\s|$)`)

func inspectRuntimeVersion(ctx context.Context, binary string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, binary, "--version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("read persistent executable version: %w", err)
	}
	match := runtimeVersionPattern.FindSubmatch(output)
	if len(match) != 2 {
		return "", fmt.Errorf("persistent executable returned no valid version")
	}
	version := strings.TrimPrefix(strings.TrimPrefix(string(match[1]), "ikun"), "v")
	if !semver.IsValid("v" + version) {
		return "", fmt.Errorf("persistent executable version is invalid")
	}
	return version, nil
}

// PreparePersistentRuntime preserves web updates in the data volume. Image
// changes are upgrades only when their actual semantic version is newer.
// Legacy .image-version files (including timestamped log lines) are ignored.
func PreparePersistentRuntime(ctx context.Context, image, directory, imageVersion string) (string, error) {
	if !semver.IsValid("v" + imageVersion) {
		return "", fmt.Errorf("image version is invalid")
	}
	directory, err := filepath.Abs(directory)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(directory, 0755); err != nil {
		return "", err
	}
	binary := filepath.Join(directory, "ikun2api")
	info, err := os.Lstat(binary)
	if os.IsNotExist(err) {
		if err := copyUpgradeExecutable(image, binary); err != nil {
			return "", err
		}
		return binary, nil
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("persistent executable must be a regular file")
	}
	// An interrupted replacement belongs to the existing recovery supervisor.
	if _, err := os.Stat(UpgradeJournalPath(binary)); err == nil {
		return binary, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	version, err := inspectRuntimeVersion(ctx, binary)
	if err != nil {
		return "", err
	} // Never silently overwrite an unknown runtime.
	if semver.Compare("v"+imageVersion, "v"+version) <= 0 {
		return binary, nil
	}

	staged, err := os.CreateTemp(directory, ".image-upgrade-*")
	if err != nil {
		return "", err
	}
	candidate := staged.Name()
	if err := staged.Close(); err != nil {
		return "", err
	}
	defer os.Remove(candidate)
	if err := copyUpgradeExecutable(image, candidate); err != nil {
		return "", err
	}
	previousHash, err := UpgradeFileHash(binary)
	if err != nil {
		return "", err
	}
	if err := stageUpgradeCandidate(ctx, binary, candidate, NewUpgradeSafety(), version, imageVersion); err != nil {
		// A rejected image must not make an otherwise usable deployment fail
		// to start. Only continue when the existing file is demonstrably intact.
		hash, hashErr := UpgradeFileHash(binary)
		if hashErr != nil || hash != previousHash {
			return "", err
		}
		log.Printf("Image update rejected; persistent version %s retained: %v", version, err)
	}
	return binary, nil
}

func copyUpgradeExecutable(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.CreateTemp(filepath.Dir(destination), ".executable-copy-*")
	if err != nil {
		return err
	}
	temporary := output.Name()
	defer os.Remove(temporary)
	_, copyErr := io.Copy(output, input)
	syncErr := output.Sync()
	modeErr := output.Chmod(0755)
	closeErr := output.Close()
	for _, err := range []error{copyErr, syncErr, modeErr, closeErr} {
		if err != nil {
			return err
		}
	}
	return os.Rename(temporary, destination)
}
