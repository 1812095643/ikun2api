// Package ownership publishes a credential-free, read-only projection of the
// monitor's authoritative config for the legacy notification sender.
package ownership

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"io"
	"os"
	"path/filepath"
)

type Claim struct {
	LegacyRuleID int64  `json:"legacy_rule_id"`
	GroupID      *int64 `json:"group_id,omitempty"`
	Platform     string `json:"platform,omitempty"`
}
type Projection struct {
	Version int     `json:"version"`
	Claims  []Claim `json:"claims"`
}

func Project(c domain.Config) Projection {
	p := Projection{Version: c.Version, Claims: []Claim{}}
	if c.Shadow {
		return p
	}
	for _, h := range c.LegacyHandoffs {
		enabled := len(h.MonitorRules) > 0
		group := int64(0)
		if h.GroupID != nil {
			group = *h.GroupID
		}
		for _, id := range h.MonitorRules {
			if !c.Rule(id).Enabled || c.Owner(id, "", group, "") != "monitor" {
				enabled = false
			}
			// The legacy sender has only group/platform dimensions. A broader
			// claim must never hide an exception that it cannot discriminate.
			for _, o := range c.OwnerOverrides {
				if o.Rule == id && o.Owner == "legacy" && (h.GroupID == nil || o.GroupID == nil || *h.GroupID == *o.GroupID) {
					enabled = false
				}
			}
		}
		if enabled {
			p.Claims = append(p.Claims, Claim{h.LegacyRuleID, h.GroupID, h.Platform})
		}
	}
	return p
}
func Union(a, b Projection) Projection {
	out := b
	out.Claims = append(append([]Claim{}, a.Claims...), b.Claims...)
	return out
}
func Write(path string, p Projection) error {
	if !filepath.IsAbs(path) {
		return errors.New("absolute ownership path required")
	}
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if len(b) > 128<<10 {
		return errors.New("ownership projection too large")
	}
	old, err := os.ReadFile(path)
	if err == nil && bytes.Equal(old, b) {
		return nil
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".ownership-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Chmod(0640); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
func Suppressed(path string, id, group int64, platform string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	var p Projection
	if err = json.NewDecoder(io.LimitReader(f, 128<<10)).Decode(&p); err != nil {
		return false, err
	}
	for _, c := range p.Claims {
		if c.LegacyRuleID == id && (c.GroupID == nil || *c.GroupID == group) && (c.Platform == "" || c.Platform == platform) {
			return true, nil
		}
	}
	return false, nil
}
