package service

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// stageUpgradeCandidate is the upstream atomic replacement step with our
// existing compatibility check, recovery journal and full snapshot retained.
func stageUpgradeCandidate(ctx context.Context, binary, candidate string, safety UpgradeSafety, previousVersion, targetVersion string) error {
	upgradeFilesMu.Lock()
	defer upgradeFilesMu.Unlock()
	if _, err := os.Stat(UpgradeJournalPath(binary)); err == nil {
		return fmt.Errorf("an upgrade is already waiting for restart")
	} else if !os.IsNotExist(err) {
		return err
	}
	if safety == nil {
		return fmt.Errorf("upgrade safety service unavailable")
	}
	exeDir := filepath.Dir(binary)
	root, err := upgradeBackupRoot(binary, true)
	if err != nil {
		return err
	}
	backupDir := filepath.Join(root, time.Now().UTC().Format(upgradeBackupTimeLayout))
	if err := safety.Prepare(ctx, candidate, backupDir); err != nil {
		return err
	}
	// Persist the old executable before one atomic rename over the live path.
	backupPath := binary + ".backup"
	in, err := os.Open(binary)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.CreateTemp(exeDir, ".previous-binary-*")
	if err != nil {
		return err
	}
	backupTemp := out.Name()
	defer os.Remove(backupTemp)
	if err := out.Chmod(0755); err != nil {
		out.Close()
		return err
	}
	_, copyErr := io.Copy(out, in)
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil {
		return fmt.Errorf("executable backup incomplete")
	}
	if err := os.Rename(backupTemp, backupPath); err != nil {
		return fmt.Errorf("save executable backup: %w", err)
	}
	previousHash, err := UpgradeFileHash(backupPath)
	if err != nil {
		return err
	}
	candidateHash, err := UpgradeFileHash(candidate)
	if err != nil {
		return err
	}
	if err := retainUpgradeExecutable(backupDir, backupPath, previousVersion, targetVersion, previousHash, candidateHash); err != nil {
		return err
	}
	journal := UpgradeJournal{BackupDirectory: backupDir, PreviousBinary: backupPath, PreviousSHA256: previousHash, CandidateSHA256: candidateHash, CreatedAt: time.Now().UTC()}
	if err := WriteUpgradeJournal(binary, journal); err != nil {
		return err
	}
	if err := os.Rename(candidate, binary); err != nil {
		_ = os.Remove(UpgradeJournalPath(binary))
		return fmt.Errorf("atomic executable replacement: %w", err)
	}
	_ = os.Remove(binary + ".upgrade-result.json")
	if dir, e := os.Open(exeDir); e == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}

	return nil
}
