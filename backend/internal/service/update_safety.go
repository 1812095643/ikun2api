package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type UpgradeSafety interface {
	Prepare(context.Context, string, string) error
}
type upgradeSafety struct{}

func NewUpgradeSafety() UpgradeSafety { return &upgradeSafety{} }

func (s *upgradeSafety) Prepare(ctx context.Context, binary, backupDir string) error {
	SetUpgradePhase("validating", "")
	preflightCtx, preflightCancel := context.WithTimeout(ctx, 60*time.Second)
	defer preflightCancel()
	cmd := exec.CommandContext(preflightCtx, binary, "--upgrade-preflight")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("candidate compatibility check failed: %s", boundedUpgradeOutput(out))
	}
	drainCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if err := BeginUpgradeMaintenance(drainCtx); err != nil {
		return err
	}
	SetUpgradePhase("backing_up", "")
	return os.MkdirAll(backupDir, 0700)
}

// WriteUpgradeBackup is a local, privileged CLI helper. It never initializes users
// or rewrites configuration; the switching script has already stopped the old app.
func WriteUpgradeBackup(ctx context.Context, dumper DBDumper, dataDir, backupDir string) error {
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(backupDir, 0700); err != nil {
		return err
	}
	if err := backupUpgradeConfig(dataDir, filepath.Join(backupDir, "data")); err != nil {
		return err
	}
	if dumper == nil {
		return fmt.Errorf("database backup service unavailable")
	}
	stream, err := dumper.Dump(ctx)
	if err != nil {
		return fmt.Errorf("cannot start database backup")
	}
	target, err := os.OpenFile(filepath.Join(backupDir, "database.sql"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		stream.Close()
		return err
	}
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(target, hash), stream)
	closeErr := stream.Close()
	syncErr := target.Sync()
	fileErr := target.Close()
	if copyErr != nil || closeErr != nil || syncErr != nil || fileErr != nil || n == 0 {
		return fmt.Errorf("database backup incomplete; update stopped")
	}
	if err := os.WriteFile(filepath.Join(backupDir, "database.sql.sha256"), []byte(hex.EncodeToString(hash.Sum(nil))), 0600); err != nil {
		return err
	}
	env, _ := json.Marshal(os.Environ())
	if err := os.WriteFile(filepath.Join(backupDir, "environment.private.json"), env, 0600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(backupDir, "COMPLETE"), []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0600)
}
func boundedUpgradeOutput(b []byte) string {
	if len(b) > 2048 {
		b = b[len(b)-2048:]
	}
	return string(b)
}
func backupUpgradeConfig(source, dest string) error {
	// Copy the application data, including installed marker, TOTP/secret material,
	// attachments and configuration. Never follow symlinks into unrelated trees.
	return filepath.WalkDir(source, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel != "." && (d.Name() == "ikun-switch-backups" || d.Name() == "upgrade-backups" || d.Name() == "updates" || d.Name() == "backups" || d.Name() == ".git") && d.IsDir() {
			return filepath.SkipDir
		}
		if d.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(target, filepath.Join(dest, rel))
		}
		to := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(to, 0700)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, err = io.Copy(out, in)
		if err == nil {
			err = out.Sync()
		}
		closeErr := out.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
}
