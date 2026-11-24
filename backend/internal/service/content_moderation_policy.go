package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
)

func moderationCategoryHit(scores, thresholds map[string]float64, category string) bool {
	score, present := scores[category]
	if !present || score <= 0 || score > 1 || math.IsNaN(score) || math.IsInf(score, 0) {
		return false
	}
	threshold, present := thresholds[category]
	if !present {
		threshold = ContentModerationDefaultThresholds()[category]
	}
	if threshold < 0 || threshold > 1 || math.IsNaN(threshold) || math.IsInf(threshold, 0) {
		return false
	}
	return score >= threshold
}

// 阈值、引擎、模型和规则版本参与缓存键，旧误判不会跨策略继续阻断。
func (cfg *ContentModerationConfig) flaggedCacheKey(inputHash string) string {
	policy := struct {
		Version, Engine, BaseURL, Model, Rules, Input string
		Thresholds                                    map[string]float64
	}{Version: "moderation-cache-v2", Engine: moderationEngine(cfg.Engine), BaseURL: cfg.BaseURL, Model: cfg.Model, Input: inputHash, Thresholds: cfg.Thresholds}
	if policy.Engine == ContentModerationEngineTypeSafe {
		policy.Rules = TypeSafeModerationRulesVersion
	}
	raw, _ := json.Marshal(policy)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}
