package service

import (
	"context"
	"encoding/json"
	"github.com/MACOS-DO/sub4api/internal/pkg/claude"
	infraerrors "github.com/MACOS-DO/sub4api/internal/pkg/errors"
	"sort"
	"strings"
)

const AccountModelAllowlistExtraKey = "model_allowlist"

// NormalizeAccountModelAllowlistExtra validates administrative configuration before any write.
func NormalizeAccountModelAllowlistExtra(extra map[string]any) (map[string]any, error) {
	raw, exists := extra[AccountModelAllowlistExtraKey]
	if !exists {
		return extra, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, infraerrors.BadRequest("INVALID_MODEL_ALLOWLIST", "模型白名单格式无效")
	}
	var cfg GroupModelAllowlist
	if err = json.Unmarshal(data, &cfg); err != nil {
		return nil, infraerrors.BadRequest("INVALID_MODEL_ALLOWLIST", "模型白名单格式无效")
	}
	if len(cfg.Models) > 512 {
		return nil, infraerrors.BadRequest("INVALID_MODEL_ALLOWLIST", "模型白名单最多 512 项")
	}
	for _, m := range cfg.Models {
		if len(m) > 200 || strings.ContainsAny(m, "\r\n\x00") {
			return nil, infraerrors.BadRequest("INVALID_MODEL_ALLOWLIST", "模型名称无效")
		}
	}
	cfg, err = normalizeGroupModelAllowlist(cfg)
	if err != nil {
		return nil, err
	}
	out := make(map[string]any, len(extra))
	for k, v := range extra {
		out[k] = v
	}
	out[AccountModelAllowlistExtraKey] = cfg
	return out, nil
}

// AllowsConfiguredModel restricts selection only; it never rewrites a provider request.
func (a *Account) AllowsConfiguredModel(model string) bool {
	if a == nil {
		return false
	}
	raw, ok := a.Extra[AccountModelAllowlistExtraKey]
	if !ok {
		return true
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return false
	}
	var cfg GroupModelAllowlist
	if json.Unmarshal(data, &cfg) != nil {
		return false
	}
	if !cfg.Enabled {
		return true
	}
	// Canonicalize both configured entries and the requested model. Only existing aliases count.
	candidates := groupModelAllowlistCandidates(model)
	for _, entry := range cfg.Models {
		for _, pattern := range groupModelAllowlistCandidates(entry) {
			for _, candidate := range candidates {
				if groupAllowlistPatternMatches(pattern, candidate) {
					return true
				}
			}
		}
	}
	return false
}

// AccountAllowlistedModels reads current administrative policy for listing. Request scheduling
// continues through the existing hot cache; a policy list never merges unrestricted fallbacks.
func (s *GatewayService) AccountAllowlistedModels(ctx context.Context, groupID *int64, platform string) ([]string, bool) {
	if s == nil || s.accountRepo == nil || platform != PlatformAnthropic {
		return nil, false
	}
	var accounts []Account
	var err error
	if groupID != nil {
		accounts, err = s.accountRepo.ListSchedulableByGroupID(ctx, *groupID)
	} else {
		accounts, err = s.accountRepo.ListSchedulable(ctx)
	}
	if err != nil {
		return []string{}, true
	}
	restricted := false
	for _, a := range accounts {
		if a.Platform != platform {
			continue
		}
		raw, ok := a.Extra[AccountModelAllowlistExtraKey]
		if !ok {
			continue
		}
		data, _ := json.Marshal(raw)
		var cfg GroupModelAllowlist
		if json.Unmarshal(data, &cfg) != nil || cfg.Enabled {
			restricted = true
		}
	}
	if !restricted {
		return nil, false
	}
	set := map[string]bool{}
	for _, a := range accounts {
		if a.Platform != platform {
			continue
		}
		source := claude.DefaultModelIDs()
		mapping := a.GetModelMapping()
		if len(mapping) > 0 {
			source = nil
			for model := range mapping {
				source = append(source, model)
			}
		}
		if raw, ok := a.Extra[AccountModelAllowlistExtraKey]; ok {
			data, _ := json.Marshal(raw)
			var cfg GroupModelAllowlist
			if json.Unmarshal(data, &cfg) == nil && cfg.Enabled {
				for _, model := range cfg.Models {
					if !strings.Contains(model, "*") {
						source = append(source, model)
					}
				}
			}
		}
		for _, model := range source {
			if a.AllowsConfiguredModel(model) && s.isModelSupportedByAccount(&a, model) {
				set[model] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for model := range set {
		out = append(out, model)
	}
	sort.Strings(out)
	return out, true
}
