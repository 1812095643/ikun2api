package detectors

import (
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"sort"
	"strings"
)

func latencyUsageKey(o domain.Observation) string {
	id := strings.TrimPrefix(o.RequestID, "client:")
	if id == "" {
		return ""
	}
	return fmt.Sprintf("%s/%s/%d/%d/%s", o.Site, id, o.AccountID, o.GroupID, o.Model)
}
func median(v []float64) float64 {
	sort.Float64s(v)
	n := len(v)
	if n%2 == 1 {
		return v[n/2]
	}
	return (v[n/2-1] + v[n/2]) / 2
}
func (e *evaluation) explainLatency(f *domain.Finding, rows []domain.Observation, threshold float64) {
	usage := map[string]domain.Observation{}
	conflict := map[string]bool{}
	for _, o := range e.rows {
		if o.Kind != "usage.recorded" || !fresh(o) {
			continue
		}
		k := latencyUsageKey(o)
		if k == "" {
			continue
		}
		if old, ok := usage[k]; ok && old.EventID != o.EventID {
			conflict[k] = true
		}
		usage[k] = o
	}
	var contexts, uncached, cacheRatios, firstEvents, effective, gaps, pre, markerGaps []float64
	total, joined, configured, marked, success, retried := 0, 0, 0, 0, 0, 0
	for _, o := range rows {
		if o.N("sample_first_ms") <= threshold {
			continue
		}
		total++
		f.RelatedSamples = append(f.RelatedSamples, domain.IncidentSample{RequestID: o.RequestID, UserID: o.UserID, KeyID: o.KeyID, AccountID: o.AccountID, GroupID: o.GroupID})
		if o.Outcome == "success" {
			success++
		}
		if o.N("switches") > 0 {
			retried++
		}
		if o.Has("first_effective_ms") {
			effective = append(effective, o.N("first_effective_ms"))
		}
		if o.Has("pre_forward_ms") {
			pre = append(pre, o.N("pre_forward_ms"))
		}
		if o.N("reasoning_started") == 1 {
			marked++
			if o.Has("reasoning_started_ms") && o.Has("first_effective_ms") && o.N("first_effective_ms") >= o.N("reasoning_started_ms") {
				markerGaps = append(markerGaps, o.N("first_effective_ms")-o.N("reasoning_started_ms"))
			}
		}
		k := latencyUsageKey(o)
		u, ok := usage[k]
		if !ok || conflict[k] {
			continue
		}
		joined++
		if u.Has("context_input_tokens") && u.Has("context_cache_read_tokens") && u.Has("context_cache_write_tokens") {
			freshTokens := u.N("context_input_tokens") + u.N("context_cache_write_tokens")
			all := freshTokens + u.N("context_cache_read_tokens")
			if all > 0 {
				contexts = append(contexts, all)
				uncached = append(uncached, freshTokens)
				cacheRatios = append(cacheRatios, u.N("context_cache_read_tokens")/all)
			}
		}
		if u.N("reasoning_configured") == 1 {
			configured++
		}
		if u.N("first_event_semantics") == 1 && u.Has("client_first_token_ms") {
			firstEvents = append(firstEvents, u.N("client_first_token_ms"))
			if o.Has("first_effective_ms") && o.N("first_effective_ms") >= u.N("client_first_token_ms") {
				gaps = append(gaps, o.N("first_effective_ms")-u.N("client_first_token_ms"))
			}
		}
	}
	f.Explanation.Conclusion = "多个请求持续等待有效输出（原因待区分）"
	f.Explanation.Confidence = "medium"
	f.Explanation.Facts = append(f.Explanation.Facts, fmt.Sprintf("慢请求%d个，精确关联用量%d个，明确成功%d个，发生换号%d个", total, joined, success, retried))
	if len(contexts) > 0 {
		f.Explanation.Facts = append(f.Explanation.Facts, fmt.Sprintf("上下文覆盖%d/%d：输入总量中位%.1f万token（含缓存），未缓存/新写入中位%.0f token，缓存命中比例中位%.1f%%", len(contexts), total, median(contexts)/10000, median(uncached), 100*median(cacheRatios)))
	}
	if len(firstEvents) > 0 && len(effective) > 0 {
		f.Explanation.Facts = append(f.Explanation.Facts, fmt.Sprintf("协议首响应中位%.1f秒（%d样本）；有效输出中位%.1f秒（%d样本）。协议首响应可能只是message_start，并非正文或思考token", median(firstEvents)/1000, len(firstEvents), median(effective)/1000, len(effective)))
	}
	if len(gaps) > 0 {
		f.Explanation.Facts = append(f.Explanation.Facts, fmt.Sprintf("同请求协议首响应之后，继续等待有效输出的间隔中位%.1f秒（%d样本）", median(gaps)/1000, len(gaps)))
	}
	var phaseFacts []string
	if len(pre) > 0 {
		phaseFacts = append(phaseFacts, fmt.Sprintf("转发前等待中位%.1f秒（含上传/网关等待，已从渠道慢样本计时中扣除）", median(pre)/1000))
	}
	phaseFacts = append(phaseFacts, fmt.Sprintf("已观测思考标记%d/%d；用量明确配置思考档位%d/%d。配置档位不代表实际思考时长", marked, total, configured, joined))
	if len(markerGaps) > 0 {
		phaseFacts = append(phaseFacts, fmt.Sprintf("已观测思考标记至有效输出间隔中位%.1f秒；不包含标记之前不可见的内部计算", median(markerGaps)/1000))
	}
	f.Explanation.Facts = append(f.Explanation.Facts, strings.Join(phaseFacts, "；"))
	f.Explanation.Inferences = append(f.Explanation.Inferences, "大上下文、思考配置和响应后等待只能说明伴随特征；当前证据不能将全部等待归因于渠道故障、上下文预处理或隐藏思考")
	f.Explanation.Missing = append(f.Explanation.Missing, fmt.Sprintf("缺少%d个慢请求的完整token字段；供应商排队、上下文预处理和内部思考分段不可见", total-len(contexts)))
	f.Explanation.Advice = []string{"优先比较同账号、同模型、相近上下文长度/缓存命中/思考档位的请求；不能用短上下文的速度直接判长请求异常", "若短上下文低思考请求也持续变慢，再检查渠道；若只集中在长上下文或高思考，先缩短上下文或降低思考强度做对照"}
}
