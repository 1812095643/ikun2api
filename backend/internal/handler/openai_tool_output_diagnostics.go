package handler

import (
	"strings"
	"unicode"

	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

type responsesToolOutputDiagnostic struct {
	Index      int    `json:"input_index"`
	Type       string `json:"type"`
	Namespace  string `json:"namespace"`
	Name       string `json:"name"`
	OutputKind string `json:"output_kind"`
}

// 仅在预校验拒绝时扫描元数据，最多记录八项；不记录工具正文、call_id 或加密内容。
func invalidResponsesToolOutputs(body []byte, missingCallID bool) []responsesToolOutputDiagnostic {
	root := gjson.ParseBytes(body)
	references := map[string]bool{}
	if !missingCallID {
		root.Get("input").ForEach(func(_, item gjson.Result) bool {
			if item.Get("type").String() == "item_reference" {
				references[strings.TrimSpace(item.Get("id").String())] = true
			}
			return true
		})
	}
	outputs := make([]responsesToolOutputDiagnostic, 0, 1)
	root.Get("input").ForEach(func(index, item gjson.Result) bool {
		typ := item.Get("type").String()
		if !isResponsesCallOutputType(typ) {
			return true
		}
		callID := strings.TrimSpace(item.Get("call_id").String())
		if (missingCallID && callID != "") || (!missingCallID && references[callID]) {
			return true
		}
		output := item.Get("output")
		kind := output.Type.String()
		switch {
		case !output.Exists():
			kind = "missing"
		case output.IsArray():
			kind = "array"
			output.ForEach(func(_, part gjson.Result) bool {
				if part.Get("type").String() == "encrypted_content" {
					kind = "encrypted"
					return false
				}
				return true
			})
		case output.IsObject():
			kind = "object"
		}
		outputs = append(outputs, responsesToolOutputDiagnostic{Index: int(index.Int()), Type: boundedToolOutputName(typ), Namespace: boundedToolOutputName(item.Get("namespace").String()), Name: boundedToolOutputName(item.Get("name").String()), OutputKind: strings.ToLower(kind)})
		return len(outputs) < 8
	})
	return outputs
}

func boundedToolOutputName(value string) string {
	runes := []rune(value)
	if len(runes) > 96 {
		runes = runes[:96]
	}
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, string(runes))
}

func firstInvalidResponsesOutputIndex(outputs []responsesToolOutputDiagnostic) int {
	if len(outputs) == 0 {
		return 0
	}
	return outputs[0].Index
}

func logInvalidResponsesToolOutputs(log *zap.Logger, reason string, outputs []responsesToolOutputDiagnostic) {
	fields := []zap.Field{zap.String("reason", reason), zap.Any("invalid_outputs", outputs)}
	if len(outputs) > 0 {
		first := outputs[0]
		fields = append(fields, zap.Int("input_index", first.Index), zap.String("type", first.Type), zap.String("namespace", first.Namespace), zap.String("name", first.Name), zap.String("output_kind", first.OutputKind))
	}
	log.Warn("openai.request_validation_failed", fields...)
}
