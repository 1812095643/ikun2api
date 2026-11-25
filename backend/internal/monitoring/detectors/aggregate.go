package detectors

import (
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"math"
	"sort"
	"strings"
	"time"
)

func (e *evaluation) latency() {
	groups := map[string][]domain.Observation{}
	telemetryPresent := map[string]bool{}
	segmentedPresent := map[string]bool{}
	for _, o := range e.rows {
		if (o.Kind == "usage.recorded" || o.Kind == "request.finished") && o.Has("pre_forward_ms") && e.now.Sub(o.OccurredAt) <= 10*time.Minute {
			segmentedPresent[cohort(o)] = true
		}
		if o.Kind == "request.finished" && o.Has("first_effective_ms") && e.now.Sub(o.OccurredAt) <= 10*time.Minute {
			telemetryPresent[cohort(o)] = true
		}
	}
	usage := map[string]domain.Observation{}
	ambiguous := map[string]bool{}
	for _, o := range e.rows {
		if o.Kind == "usage.recorded" && fresh(o) && o.Has("request_stream") {
			k := latencyUsageKey(o)
			if old, ok := usage[k]; ok && old.EventID != o.EventID {
				ambiguous[k] = true
			}
			usage[k] = o
		}
	}
	seen := map[string]bool{}
	for _, o := range e.rows {
		if !fresh(o) || o.Model == "" || strings.Contains(strings.ToLower(o.Model), "image") || strings.Contains(strings.ToLower(o.Model), "video") || strings.Contains(strings.ToLower(o.Model), "sora") || e.now.Sub(o.OccurredAt) > 10*time.Minute || o.OccurredAt.After(e.now) {
			continue
		}
		if ambiguous[latencyUsageKey(o)] {
			continue
		}
		if u, ok := usage[latencyUsageKey(o)]; ok && u.N("request_stream") == 0 {
			continue
		}
		scope := cohort(o)
		ms := -1.
		if o.Kind == "request.finished" && fresh(o) && o.N("has_output") == 1 && o.Has("first_effective_ms") && !earlyHiddenReasoning(o, e.cfg.Profile(o.Model).FirstWarnSeconds) {
			ms = o.N("first_effective_ms")
		}
		if o.Kind == "usage.recorded" && !telemetryPresent[scope] && o.Has("client_first_token_ms") {
			ms = o.N("client_first_token_ms")
		}
		if segmentedPresent[scope] && !o.Has("pre_forward_ms") {
			continue
		}
		if ms >= 0 && o.Has("pre_forward_ms") && o.N("pre_forward_ms") >= 0 && o.N("pre_forward_ms") <= ms {
			ms -= o.N("pre_forward_ms")
		}
		if ms < 0 {
			continue
		}
		key := requestKey(o)
		if o.RequestID == "" {
			key = o.Source + ":" + o.EventID
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		o.Metrics = copyMetrics(o.Metrics)
		o.Metrics["sample_first_ms"] = ms
		groups[scope] = append(groups[scope], o)
	}
	for scope, rows := range groups {
		if len(rows) == 0 {
			continue
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].OccurredAt.Before(rows[j].OccurredAt) })
		p := e.cfg.Profile(rows[len(rows)-1].Model)
		warn := float64(p.FirstWarnSeconds * 1000)
		if strings.HasPrefix(strings.ToLower(rows[len(rows)-1].Model), "gpt-") && warn < 60000 {
			warn = 60000
		}
		var firstSlow, lastSlow time.Time
		slow, consecutive, switches := 0, 0, 0
		var values []float64
		var goodSince time.Time
		goodCount := 0
		for _, o := range rows {
			v := o.N("sample_first_ms")
			values = append(values, v)
			if v > warn {
				slow++
				if firstSlow.IsZero() {
					firstSlow = o.OccurredAt
				}
				lastSlow = o.OccurredAt
				consecutive++
				goodSince = time.Time{}
				goodCount = 0
			} else {
				consecutive = 0
				if goodSince.IsZero() {
					goodSince = o.OccurredAt
				}
				goodCount++
			}
			if o.N("switches") > 0 {
				switches++
			}
		}
		sort.Float64s(values)
		p95 := values[int(math.Ceil(float64(len(values))*.95))-1]
		bad := slow >= 5 && float64(slow)/float64(len(rows)) >= .3 && lastSlow.Sub(firstSlow) >= 5*time.Minute
		latest := rows[len(rows)-1]
		if bad {
			for n := len(rows) - 1; n >= 0; n-- {
				if rows[n].N("sample_first_ms") > warn {
					latest = rows[n]
					break
				}
			}
			f := e.add("L03", latest, scope, "slow_distribution", "P2", false, []string{fmt.Sprintf("10分钟样本%d，慢首字比例%.1f%%，P95 %.1f秒，末尾连续慢样本%d；至少5个慢请求跨5分钟才提醒（GPT慢样本下限60秒，有分段时剔除转发前耗时）", len(rows), 100*float64(slow)/float64(len(rows)), p95/1000, consecutive)}, []string{"缺少转发前分段时使用端到端耗时；转发后时间可能包含重试，不能单凭该指标认定上游故障"})
			if f != nil {
				e.explainLatency(f, rows, warn)
			}
		} else if goodCount >= 5 && latest.OccurredAt.Sub(goodSince) >= 5*time.Minute && fresh(latest) {
			e.add("L03", latest, scope, "slow_distribution", "P2", true, []string{"至少5个有效样本且持续5分钟改善"}, nil)
		}
		if len(rows) >= 10 && float64(switches)/float64(len(rows)) > .1 {
			e.add("R01", latest, scope, "switch_ratio", "P2", false, []string{fmt.Sprintf("10分钟%d个请求中%d个发生换号", len(rows), switches)}, nil)
		} else if len(rows) >= 10 && fresh(latest) {
			e.add("R01", latest, scope, "switch_ratio", "P2", true, []string{"换号比例已回落，仍需连续健康窗口"}, nil)
		}
	}
}
func copyMetrics(in map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(in)+1)
	for k, v := range in {
		out[k] = v
	}
	return out
}
func (e *evaluation) cancellations() {
	type bucket struct {
		oldN, oldCancel, n, cancel, degraded int
		last                                 domain.Observation
	}
	groups := map[string]*bucket{}
	for _, o := range e.ends {
		age := e.now.Sub(o.OccurredAt)
		if age > 20*time.Minute || age < 0 || !fresh(o) {
			continue
		}
		scope := poolScope(o)
		b := groups[scope]
		if b == nil {
			b = &bucket{}
			groups[scope] = b
		}
		cancelled := o.Kind == "request.cancelled" || o.Outcome == "cancelled"
		if age > 10*time.Minute {
			b.oldN++
			if cancelled {
				b.oldCancel++
			}
			continue
		}
		b.n++
		if cancelled {
			b.cancel++
		}
		if (o.Has("first_effective_ms") && o.N("first_effective_ms") >= float64(e.cfg.Profile(o.Model).FirstWarnSeconds*1000) && !earlyHiddenReasoning(o, e.cfg.Profile(o.Model).FirstWarnSeconds)) || o.Outcome == "failed" {
			b.degraded++
		}
		if o.OccurredAt.After(b.last.OccurredAt) {
			b.last = o
		}
	}
	for scope, b := range groups {
		if b.oldN < 20 || b.n < 20 {
			continue
		}
		baseline := float64(b.oldCancel) / float64(b.oldN)
		ratio := float64(b.cancel) / float64(b.n)
		if ratio > math.Max(.1, 2*baseline) && b.cancel >= 3 && b.degraded >= 3 {
			f := e.add("C01", b.last, scope, "cancellation_spike", "P2", false, []string{fmt.Sprintf("取消比例由%.1f%%上升至%.1f%%，伴随%d个慢或失败请求", baseline*100, ratio*100, b.degraded)}, []string{"无法仅凭服务端取消信号区分主动取消、客户端网络或VPN故障"})
			if f != nil {
				f.Explanation.Confidence = "medium"
			}
		} else if fresh(b.last) {
			e.add("C01", b.last, scope, "cancellation_spike", "P2", true, []string{"近期取消比例与体验关联指标已回落，仍需连续窗口"}, nil)
		}
	}
}
