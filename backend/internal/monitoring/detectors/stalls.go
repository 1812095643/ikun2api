package detectors

import (
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"sort"
	"time"
)

const stallWindow = 10 * time.Minute
const stallMinimumRequests = 3

func (e *evaluation) stalls() {
	type request struct {
		latest, fault domain.Observation
		maxIdle       float64
		invalid       bool
		scope         string
	}
	requests := map[string]*request{}
	for _, o := range e.rows {
		if o.Source != "gateway" || o.RequestID == "" || (o.Kind != "request.active" && o.Kind != "request.finished" && o.Kind != "request.cancelled") {
			continue
		}
		key := requestKey(o)
		r := requests[key]
		if r == nil {
			r = &request{}
			requests[key] = r
		}
		if o.N("stall_evidence_v1") != 1 || !fresh(o) || o.N("reasoning_started") == 1 || o.N("reasoning_output") == 1 || o.N("realtime") == 1 {
			r.invalid = true
		}
		if (o.AccountID == 0 && o.ChannelID == 0) || o.Model == "" {
			continue
		}
		scope := cohort(o)
		if r.scope != "" && r.scope != scope {
			r.invalid = true
		}
		r.scope = scope
		if o.OccurredAt.After(e.now) || e.now.Sub(o.OccurredAt) > stallWindow {
			continue
		}
		if r.latest.EventID == "" || !o.OccurredAt.Before(r.latest.OccurredAt) {
			r.latest = o
		}
		if o.N("has_output") == 1 && o.Has("idle_ms") && o.N("idle_ms") >= float64(e.cfg.Profile(o.Model).StallWarnSeconds*1000) {
			if r.fault.EventID == "" {
				r.fault = o
			}
			if o.N("idle_ms") > r.maxIdle {
				r.maxIdle = o.N("idle_ms")
			}
		}
	}
	groups := map[string][]*request{}
	for _, r := range requests {
		if !r.invalid && r.scope != "" && r.latest.EventID != "" && r.latest.N("has_output") == 1 {
			groups[r.scope] = append(groups[r.scope], r)
		}
	}
	for scope, rows := range groups {
		var bad, good []*request
		for _, r := range rows {
			if r.fault.EventID != "" {
				bad = append(bad, r)
			} else if r.latest.Kind == "request.finished" && r.latest.Outcome == "success" && r.latest.N("terminal") == 1 {
				good = append(good, r)
			}
		}
		sort.Slice(bad, func(i, j int) bool { return requestKey(bad[i].fault) < requestKey(bad[j].fault) })
		if len(bad) >= stallMinimumRequests && len(bad)*5 >= len(rows) {
			latest := bad[0]
			for _, r := range bad {
				if r.fault.OccurredAt.After(latest.fault.OccurredAt) {
					latest = r
				}
			}
			{
				r := latest
				o := r.fault
				o.EventID = "repeated-stalls:" + requestKey(o)
				f := e.add("L04", o, scope, "repeated_observed_gaps", "P2", false, []string{fmt.Sprintf("最近10分钟可评估%d个请求，%d个不同请求曾连续至少%d秒未观察到新的有效输出（%.1f%%）", len(rows), len(bad), e.cfg.Profile(o.Model).StallWarnSeconds, 100*float64(len(bad))/float64(len(rows))), "单次停顿不通知；同一请求反复采样只计一次；协议不完整及思考请求已排除", fmt.Sprintf("样本最后有效输出：%s；该请求最长已观察到的间隔%.1f秒", o.OccurredAt.Add(-time.Duration(o.N("idle_ms"))*time.Millisecond).In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05"), r.maxIdle/1000)}, []string{"这是服务端观察到的输出间隔，不证明上游卡死或客户端网络故障；隐藏思考和不完整协议不在统计覆盖内"})
				if f != nil {
					for _, r := range bad {
						o := r.fault
						f.RelatedSamples = append(f.RelatedSamples, domain.IncidentSample{RequestID: o.RequestID, UserID: o.UserID, AccountID: o.AccountID, GroupID: o.GroupID, KeyID: o.KeyID})
					}
				}
			}
		} else if len(bad) == 0 && len(good) >= 5 {
			sort.Slice(good, func(i, j int) bool { return good[i].latest.OccurredAt.Before(good[j].latest.OccurredAt) })
			if good[len(good)-1].latest.OccurredAt.Sub(good[0].latest.OccurredAt) >= 5*time.Minute {
				e.add("L04", good[len(good)-1].latest, scope, "repeated_observed_gaps", "P2", true, []string{"观察窗口内至少5个可评估请求成功结束，跨越5分钟且未观察到长停顿"}, []string{"只证明可评估样本改善；没有新请求或证据缺失不视为恢复"})
			}
		}
	}
}
