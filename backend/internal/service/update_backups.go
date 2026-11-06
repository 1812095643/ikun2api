package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Shared by replacement, settings and cleanup in the serving process. The
// supervisor's database snapshot is additionally protected by its journal.
var upgradeFilesMu sync.Mutex

const upgradeBackupTimeLayout = "20060102T150405.000000000Z"

type UpgradeBackupPolicy struct {
	AutoCleanup bool `json:"auto_cleanup"`
	Keep        int  `json:"keep"`
}

type UpgradeBackupStatus struct {
	Policy         UpgradeBackupPolicy `json:"policy"`
	Count          int                 `json:"count"`
	CompleteCount  int                 `json:"complete_count"`
	ProtectedCount int                 `json:"protected_count"`
	Bytes          int64               `json:"bytes"`
}

type upgradeBackupEntry struct {
	path     string
	created  time.Time
	complete bool
	bytes    int64
}

type upgradeBackupManifest struct {
	Format           int    `json:"format"`
	PreviousVersion  string `json:"previous_version"`
	CandidateVersion string `json:"candidate_version"`
	PreviousSHA256   string `json:"previous_sha256"`
	CandidateSHA256  string `json:"candidate_sha256"`
}

func upgradeBackupRoot(binary string, create bool) (string, error) {
	root := filepath.Join(filepath.Dir(binary), "upgrade-backups")
	if create {
		if err := os.MkdirAll(root, 0700); err != nil {
			return "", err
		}
	}
	info, err := os.Lstat(root)
	if os.IsNotExist(err) && !create {
		return root, nil
	}
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("upgrade backup root must be a real directory")
	}
	return root, nil
}

func readUpgradeBackupPolicy(root string) (UpgradeBackupPolicy, error) {
	policy := UpgradeBackupPolicy{AutoCleanup: true, Keep: 3}
	data, err := os.ReadFile(filepath.Join(root, "policy.json"))
	if os.IsNotExist(err) {
		return policy, nil
	}
	if err != nil {
		return policy, err
	}
	if err := json.Unmarshal(data, &policy); err != nil {
		return policy, err
	}
	if policy.Keep < 1 || policy.Keep > 30 {
		return policy, fmt.Errorf("backup keep count must be between 1 and 30")
	}
	return policy, nil
}

func writeUpgradeJSON(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".metadata-*")
	if err != nil {
		return err
	}
	temporary := f.Name()
	defer os.Remove(temporary)
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	for _, err := range []error{writeErr, syncErr, closeErr} {
		if err != nil {
			return err
		}
	}
	if err := os.Rename(temporary, path); err != nil {
		return err
	}
	if dir, err := os.Open(filepath.Dir(path)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

func retainUpgradeExecutable(directory, previous, previousVersion, candidateVersion, previousHash, candidateHash string) error {
	if err := copyUpgradeExecutable(previous, filepath.Join(directory, "program")); err != nil {
		return err
	}
	return writeUpgradeJSON(filepath.Join(directory, "manifest.json"), upgradeBackupManifest{
		Format: 1, PreviousVersion: previousVersion, CandidateVersion: candidateVersion,
		PreviousSHA256: previousHash, CandidateSHA256: candidateHash,
	})
}

func regularUpgradeFile(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}

func scanUpgradeBackups(root string) ([]upgradeBackupEntry, error) {
	dirs, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	entries := make([]upgradeBackupEntry, 0)
	for _, dir := range dirs {
		if !dir.IsDir() || dir.Type()&os.ModeSymlink != 0 {
			continue
		}
		path := filepath.Join(root, dir.Name())
		created, nameErr := time.Parse(upgradeBackupTimeLayout, dir.Name())
		entry := upgradeBackupEntry{path: path, created: created}
		dataInfo, dataErr := os.Lstat(filepath.Join(path, "data"))
		entry.complete = nameErr == nil && dataErr == nil && dataInfo.IsDir() && dataInfo.Mode()&os.ModeSymlink == 0 &&
			regularUpgradeFile(filepath.Join(path, "COMPLETE")) && regularUpgradeFile(filepath.Join(path, "database.sql")) && regularUpgradeFile(filepath.Join(path, "database.sql.sha256"))
		err := filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.Type().IsRegular() {
				info, err := d.Info()
				if err != nil {
					return err
				}
				entry.bytes += info.Size()
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].created.After(entries[j].created) })
	return entries, nil
}

func ReadUpgradeBackupStatus(binary string) (*UpgradeBackupStatus, error) {
	upgradeFilesMu.Lock()
	defer upgradeFilesMu.Unlock()
	root, err := upgradeBackupRoot(binary, false)
	if err != nil {
		return nil, err
	}
	policy, err := readUpgradeBackupPolicy(root)
	if err != nil {
		return nil, err
	}
	entries, err := scanUpgradeBackups(root)
	if err != nil {
		return nil, err
	}
	status := &UpgradeBackupStatus{Policy: policy, Count: len(entries)}
	for _, entry := range entries {
		status.Bytes += entry.bytes
		if entry.complete {
			status.CompleteCount++
		} else {
			status.ProtectedCount++
		}
	}
	if journal, err := ReadUpgradeJournal(binary); err == nil {
		for _, entry := range entries {
			if filepath.Clean(entry.path) == filepath.Clean(journal.BackupDirectory) && entry.complete {
				status.ProtectedCount++
			}
		}
	}
	return status, nil
}

func SaveUpgradeBackupPolicy(binary string, policy UpgradeBackupPolicy) error {
	if policy.Keep < 1 || policy.Keep > 30 {
		return fmt.Errorf("backup keep count must be between 1 and 30")
	}
	upgradeFilesMu.Lock()
	defer upgradeFilesMu.Unlock()
	root, err := upgradeBackupRoot(binary, true)
	if err != nil {
		return err
	}
	return writeUpgradeJSON(filepath.Join(root, "policy.json"), policy)
}

func verifyUpgradeBackup(entry upgradeBackupEntry) bool {
	expected, err := os.ReadFile(filepath.Join(entry.path, "database.sql.sha256"))
	if err != nil {
		return false
	}
	actual, err := UpgradeFileHash(filepath.Join(entry.path, "database.sql"))
	if err != nil || len(strings.TrimSpace(string(expected))) != 64 || actual != strings.TrimSpace(string(expected)) {
		return false
	}
	// Legacy snapshots have no manifest. Their COMPLETE marker, timestamp,
	// database digest and data directory still identify them as our backups.
	manifestPath := filepath.Join(entry.path, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if os.IsNotExist(err) {
		return true
	}
	if err != nil || !regularUpgradeFile(manifestPath) || !regularUpgradeFile(filepath.Join(entry.path, "program")) {
		return false
	}
	var manifest upgradeBackupManifest
	if json.Unmarshal(data, &manifest) != nil || manifest.Format != 1 {
		return false
	}
	hash, err := UpgradeFileHash(filepath.Join(entry.path, "program"))
	return err == nil && hash == manifest.PreviousSHA256
}

// CleanupUpgradeBackups only removes complete, verified snapshots owned by the
// updater. No pruning is allowed while an update/recovery journal exists.
func CleanupUpgradeBackups(ctx context.Context, binary string) (int, error) {
	upgradeFilesMu.Lock()
	defer upgradeFilesMu.Unlock()
	if _, err := os.Lstat(UpgradeJournalPath(binary)); err == nil {
		return 0, nil
	} else if !os.IsNotExist(err) {
		return 0, err
	}
	root, err := upgradeBackupRoot(binary, false)
	if err != nil {
		return 0, err
	}
	policy, err := readUpgradeBackupPolicy(root)
	if err != nil || !policy.AutoCleanup {
		return 0, err
	}
	entries, err := scanUpgradeBackups(root)
	if err != nil {
		return 0, err
	}
	kept, removed := 0, 0
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return removed, err
		}
		if !entry.complete {
			continue
		}
		// Corrupt/incomplete snapshots never count against the healthy copies.
		if !verifyUpgradeBackup(entry) {
			continue
		}
		kept++
		if kept <= policy.Keep {
			continue
		}
		rel, err := filepath.Rel(root, entry.path)
		if err != nil || !filepath.IsLocal(rel) || filepath.Dir(rel) != "." {
			return removed, fmt.Errorf("invalid backup cleanup path")
		}
		info, err := os.Lstat(entry.path)
		if err != nil {
			return removed, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return removed, fmt.Errorf("backup changed during cleanup")
		}
		if err := os.RemoveAll(entry.path); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

func currentUpgradeBinary() (string, error) {
	binary, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(binary)
}

func (s *UpdateService) GetUpgradeBackups() (*UpgradeBackupStatus, error) {
	binary, err := currentUpgradeBinary()
	if err != nil {
		return nil, err
	}
	return ReadUpgradeBackupStatus(binary)
}

func (s *UpdateService) ConfigureUpgradeBackups(policy UpgradeBackupPolicy) (*UpgradeBackupStatus, error) {
	binary, err := currentUpgradeBinary()
	if err != nil {
		return nil, err
	}
	if err := SaveUpgradeBackupPolicy(binary, policy); err != nil {
		return nil, err
	}
	status, err := ReadUpgradeBackupStatus(binary)
	if err != nil {
		return nil, err
	}
	go runUpgradeBackupCleanup(binary)
	return status, nil
}

func runUpgradeBackupCleanup(binary string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	removed, err := CleanupUpgradeBackups(ctx, binary)
	if err != nil {
		log.Printf("Upgrade backup cleanup failed: %v", err)
	} else if removed > 0 {
		log.Printf("Pruned %d old upgrade backups", removed)
	}
}

// Runs in the ready serving child, so cleanup also works after upgrades under
// older supervisors. A pending journal always postpones deletion.
func StartUpgradeBackupCleanup() func() {
	ctx, cancel := context.WithCancel(context.Background())
	binary, err := currentUpgradeBinary()
	if err != nil {
		return cancel
	}
	go func() {
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				if _, err := os.Lstat(UpgradeJournalPath(binary)); os.IsNotExist(err) {
					cleanupCtx, stop := context.WithTimeout(ctx, 5*time.Minute)
					removed, err := CleanupUpgradeBackups(cleanupCtx, binary)
					stop()
					if err != nil && ctx.Err() == nil {
						log.Printf("Upgrade backup cleanup failed: %v", err)
					} else if removed > 0 {
						log.Printf("Pruned %d old upgrade backups", removed)
					}
					timer.Reset(time.Hour)
				} else {
					timer.Reset(time.Minute)
				}
			}
		}
	}()
	return cancel
}
