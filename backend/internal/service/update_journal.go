package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

type UpgradeJournal struct {
	BackupDirectory string    `json:"backup_directory"`
	PreviousBinary  string    `json:"previous_binary"`
	PreviousSHA256  string    `json:"previous_sha256"`
	CandidateSHA256 string    `json:"candidate_sha256"`
	CreatedAt       time.Time `json:"created_at"`
}

func UpgradeFileHash(path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func UpgradeJournalPath(binary string) string { return binary + ".upgrade.json" }
func ReadUpgradeJournal(binary string) (*UpgradeJournal, error) {
	b, e := os.ReadFile(UpgradeJournalPath(binary))
	if e != nil {
		return nil, e
	}
	var j UpgradeJournal
	if e = json.Unmarshal(b, &j); e != nil {
		return nil, e
	}
	dir := filepath.Clean(j.BackupDirectory)
	root := filepath.Join(filepath.Dir(binary), "upgrade-backups")
	rel, e := filepath.Rel(root, dir)
	if e != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) || len(rel) > 2 && rel[:3] == "../" {
		return nil, fmt.Errorf("invalid upgrade backup path")
	}
	if len(j.PreviousSHA256) != 64 || len(j.CandidateSHA256) != 64 {
		return nil, fmt.Errorf("invalid upgrade executable digests")
	}
	if j.PreviousBinary != binary+".backup" {
		return nil, fmt.Errorf("invalid previous binary path")
	}
	return &j, nil
}
func WriteUpgradeJournal(binary string, j UpgradeJournal) error {
	b, e := json.Marshal(j)
	if e != nil {
		return e
	}
	file := UpgradeJournalPath(binary)
	temp := file + ".new"
	f, e := os.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	_, e = f.Write(b)
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil {
		os.Remove(temp)
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(temp, file)
}
func MarkUpgradeReady() {
	if os.Getenv("IKUN_SUPERVISED_CHILD") != "1" {
		return
	}
	binary, e := os.Executable()
	if e != nil {
		return
	}
	binary, e = filepath.EvalSymlinks(binary)
	if e != nil {
		return
	}
	j, e := ReadUpgradeJournal(binary)
	if e != nil {
		return
	}
	hash, e := UpgradeFileHash(binary)
	if e != nil || hash != j.CandidateSHA256 {
		return
	}
	b, _ := json.Marshal(map[string]any{"pid": os.Getpid(), "sha256": hash})
	_ = os.WriteFile(UpgradeJournalPath(binary)+".ready", b, 0600)
}
