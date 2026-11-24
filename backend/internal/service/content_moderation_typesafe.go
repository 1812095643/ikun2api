package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/MACOS-DO/sub4api/internal/pkg/typesafe"
)

const TypeSafeModerationRulesVersion = "content-audit-13-zh-v3-single"

// 每类只做一个简短判断；冗长的通用说明会干扰 Jev 分类，并增加审计耗时。
// 这些分类独立判断，不表示与 OpenAI 的审核策略等价。
var typeSafeModerationRules = map[string]string{
	"harassment":             "文本是否直接辱骂、贬损或骚扰个人或群体？",
	"harassment/threatening": "文本是否在骚扰他人的同时威胁对其施加严重人身伤害？",
	"hate":                   "文本是否因种族、宗教、性别等受保护身份而贬低或煽动仇恨某群体？",
	"hate/threatening":       "文本是否因种族、宗教、性别等身份而威胁对人施加暴力？",
	"illicit":                "用户是否在请求实施违法犯罪行为的具体帮助？",
	"illicit/violent":        "用户是否在请求实施暴力犯罪的具体帮助？",
	"self-harm":              "文本是否宣扬或鼓励自杀、自残等自我伤害行为？",
	"self-harm/intent":       "用户是否表达了当前自杀或自残的意图？",
	"self-harm/instructions": "用户是否请求或提供自杀、自残的具体方法？",
	"sexual":                 "文本是否包含或请求以性刺激为目的的露骨色情内容？",
	"sexual/minors":          "文本是否包含或请求涉及未满18岁者的色情内容？",
	"violence":               "文本是否描写、鼓励或威胁对人施加暴力伤害？",
	"violence/graphic":       "文本是否包含或请求血腥肢解等露骨暴力细节？",
}

func typeSafeModerationQuestions() map[string]typesafe.Question {
	questions := make(map[string]typesafe.Question, len(typeSafeModerationRules))
	for category, rule := range typeSafeModerationRules {
		questions[category] = typesafe.Question{Type: "noul", Instructions: rule}
	}
	return questions
}

func typeSafeText(input any) (string, int) {
	if text, ok := input.(string); ok {
		return text, 0
	}
	var texts []string
	skipped := 0
	if parts, ok := input.([]moderationAPIInputPart); ok {
		for _, part := range parts {
			if part.Type == "text" {
				texts = append(texts, part.Text)
			}
			if part.Type == "image_url" {
				skipped++
			}
		}
	}
	return strings.Join(texts, "\n"), skipped
}

func (s *ContentModerationService) callTypeSafeModeration(ctx context.Context, cfg *ContentModerationConfig, key string, input any, httpStatus *int) (*moderationAPIResult, error) {
	text, skipped := typeSafeText(input)
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("typesafe text-only: no text to audit; images were not audited")
	}
	client, err := s.moderationHTTPClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutMS)*time.Millisecond)
	defer cancel()
	result, status, err := typesafe.Evaluate(ctx, client, cfg.BaseURL, key, typesafe.Request{
		Model: cfg.Model, State: text, Questions: typeSafeModerationQuestions(),
	})
	if httpStatus != nil {
		*httpStatus = status
	}
	if err != nil {
		return nil, err
	}
	return &moderationAPIResult{CategoryScores: result.Scores, EngineMeta: &ContentModerationEngineMeta{
		Engine: ContentModerationEngineTypeSafe, Model: result.Model, RulesVersion: TypeSafeModerationRulesVersion, SkippedImages: skipped,
	}}, nil
}

func moderationAttemptMeta(cfg *ContentModerationConfig, input ContentModerationInput) *ContentModerationEngineMeta {
	meta := &ContentModerationEngineMeta{Engine: moderationEngine(cfg.Engine)}
	if cfg.Engine == ContentModerationEngineTypeSafe {
		meta.RulesVersion = TypeSafeModerationRulesVersion
		meta.SkippedImages = len(limitContentModerationImages(input.Images))
	}
	// 上游成功响应后才能记录实际模型，不能把配置值当作真实返回值。
	return meta
}
