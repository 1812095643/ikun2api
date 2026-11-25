package detectors

import (
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"time"
)

func (e *evaluation) escalateImpact() {
	pools, requests, users := map[string]bool{}, map[string]bool{}, map[int64]bool{}
	tasks, billedUsers := map[string]bool{}, map[int64]bool{}
	for _, o := range e.rows {
		if !fresh(o) || o.OccurredAt.Before(e.now.Add(-5*time.Minute)) || o.OccurredAt.After(e.now) {
			continue
		}
		if (o.Kind == "pool.empty" || o.Kind == "pool.snapshot") && o.Has("eligible") && o.N("eligible") == 0 && o.RequestID != "" {
			pools[poolScope(o)] = true
			requests[requestKey(o)] = true
			if o.UserID > 0 {
				users[o.UserID] = true
			}
		}
		if o.Kind == "billing.reconciliation" && o.N("confirmed_duplicate") == 1 && o.N("legitimate_refund") != 1 && o.TaskID != "" {
			tasks[taskScope(o)] = true
			if o.UserID > 0 {
				billedUsers[o.UserID] = true
			}
		}
	}
	wide := len(pools) >= 3 && len(users) >= 3 && len(requests) >= 5
	financial := len(tasks) >= 2 && len(billedUsers) >= 2
	for n := range e.findings {
		f := &e.findings[n]
		if f.Healthy || f.Quality != domain.Fresh || f.At.Before(e.now.Add(-5*time.Minute)) {
			continue
		}
		if f.Rule == "S01" && wide {
			f.Severity = "P0"
			f.Explanation.Facts = append(f.Explanation.Facts, fmt.Sprintf("近5分钟已观察到%d个分组/模型范围、%d个用户、%d个请求候选不足，达到广泛影响升级阈值；不代表全部用户不可用", len(pools), len(users), len(requests)))
		}
		if f.Rule == "B02" && financial && tasks[f.Scope] {
			f.Severity = "P0"
			f.Explanation.Facts = append(f.Explanation.Facts, fmt.Sprintf("近5分钟独立对账已确认%d个任务、%d个用户重复计费，达到批量资金风险阈值", len(tasks), len(billedUsers)))
		}
	}
}
