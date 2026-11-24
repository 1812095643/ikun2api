package detectors

import (
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"time"
)

func (e *evaluation) pools() {
	pools := map[string][]domain.Observation{}
	for _, o := range e.rows {
		if o.Quality == domain.Stale || o.Quality == domain.Unsupported {
			continue
		}
		switch o.Kind {
		case "pool.snapshot", "pool.empty":
			scope := poolScope(o)
			pools[scope] = append(pools[scope], o)
			if o.Has("eligible") && o.N("eligible") == 0 {
				missing := []string{}
				if !o.Has("total") {
					missing = append(missing, "缺少完整候选过滤计数，无法确定更底层原因")
				}
				e.add("S01", o, scope, "empty", "P1", false, []string{fmt.Sprintf("候选总数%.0f，可调度%.0f，模型限流过滤%.0f，临时不可调度%.0f", o.N("total"), o.N("eligible"), o.N("model_rate_limited"), o.N("unschedulable"))}, missing)
			}
		case "account.quarantined", "account.refresh_failed":
			cause := "quarantine"
			if o.Kind == "account.refresh_failed" {
				cause = "refresh"
			}
			parentFP := ""
			if cause == "quarantine" && o.N("proxy_id") > 0 {
				parent := e.add("S03", o, fmt.Sprintf("proxy:%.0f", o.N("proxy_id")), "proxy_quarantine", "P1", false, []string{fmt.Sprintf("代理配置%.0f关联的账号%d触发隔离", o.N("proxy_id"), o.AccountID)}, []string{"这是同一代理配置的关联，不等于证明代理运营商故障"})
				if parent != nil {
					parentFP = parent.Fingerprint
				}
			}
			child := e.add("S03", o, fmt.Sprintf("account:%d", o.AccountID), cause, "P1", false, []string{"已观察到账号状态事件=" + o.Kind}, []string{"代理运营商或授权系统的深层原因尚未确定"})
			if child != nil {
				child.ParentFingerprint = parentFP
			}
		case "account.recovered":
			if !fresh(o) {
				continue
			}
			e.add("S03", o, fmt.Sprintf("account:%d", o.AccountID), "quarantine", "P1", fresh(o), []string{"隔离保护门已通过实际恢复探测"}, nil)
		}
		if o.Kind == "request.finished" && o.Outcome == "success" && o.AccountID > 0 && fresh(o) {
			e.add("S01", o, poolScope(o), "empty", "P1", true, []string{"该分组/模型已选出账号并完成请求"}, nil)
			e.add("S03", o, fmt.Sprintf("account:%d", o.AccountID), "refresh", "P1", true, []string{"该账号已完成真实授权请求"}, nil)
			e.add("R02", o, fmt.Sprintf("account:%d", o.AccountID), "selected_quarantined", "P1", true, []string{"该账号通过保护门且请求成功；此前调度违规证据保留"}, nil)
		}
	}
	for scope, rows := range pools {
		baseline := 0.
		var lowSince time.Time
		var latest domain.Observation
		for _, o := range rows {
			if !o.Has("eligible") {
				continue
			}
			n := o.N("eligible")
			if n > baseline {
				baseline = n
			}
			low := baseline >= 2 && (n == 1 || n < baseline*.5)
			if low {
				if lowSince.IsZero() {
					lowSince = o.OccurredAt
				}
			} else {
				lowSince = time.Time{}
			}
			latest = o
		}
		if baseline < 2 || latest.EventID == "" {
			continue
		}
		if !lowSince.IsZero() && latest.OccurredAt.Sub(lowSince) >= 2*time.Minute {
			e.add("S02", latest, scope, "shrink", "P2", false, []string{fmt.Sprintf("观察窗口内容量由%.0f降至%.0f，持续至少2分钟", baseline, latest.N("eligible"))}, nil)
		} else if lowSince.IsZero() && fresh(latest) {
			e.add("S02", latest, scope, "shrink", "P2", true, []string{"可调度容量已回到观察基线附近，仍需稳定窗口"}, nil)
		}
	}
}
