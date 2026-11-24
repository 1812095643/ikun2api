package detectors

import (
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"math"
	"time"
)

func (e *evaluation) tasks() {
	type task struct {
		accepted, completed, terminal, billed, last domain.Observation
		metadata                                    []domain.Observation
	}
	tasks := map[string]*task{}
	for _, o := range e.rows {
		if o.TaskID == "" && (o.Kind == "billing.metadata_failed" || o.Kind == "billing.failed") && o.Quality != domain.Stale && o.Quality != domain.Unsupported {
			e.add("B03", o, cohort(o), "metadata_without_task", "P1", false, []string{"系统日志确认计费快照或资金归属记录失败"}, []string{"任务ID缺失，不能关联完成状态、确认漏费或自动补扣"})
		}
		if o.TaskID == "" || o.Quality == domain.Stale || o.Quality == domain.Unsupported {
			continue
		}
		switch o.Kind {
		case "media.accepted", "media.pending", "media.completed", "media.failed", "media.cancelled", "billing.applied", "billing.failed", "billing.metadata_failed":
		default:
			continue
		}
		scope := taskScope(o)
		t := tasks[scope]
		if t == nil {
			t = &task{}
			tasks[scope] = t
		}
		switch o.Kind {
		case "media.accepted":
			if t.accepted.EventID == "" {
				t.accepted = o
			}
		case "media.pending":
			t.last = o
		case "media.completed":
			if t.completed.EventID == "" {
				t.completed = o
			}
			t.terminal = o
			t.last = o
		case "media.failed", "media.cancelled":
			t.terminal = o
			t.last = o
		case "billing.applied":
			t.billed = o
		case "billing.failed", "billing.metadata_failed":
			t.metadata = append(t.metadata, o)
		}
	}
	for scope, t := range tasks {
		if t.terminal.Kind == "media.failed" {
			e.add("A01", t.terminal, scope, "task_state", "P1", false, []string{"已有上游或任务服务明确失败终态"}, nil)
		} else if t.terminal.EventID != "" {
			if fresh(t.terminal) {
				e.add("A01", t.terminal, scope, "task_state", "P2", true, []string{"已有明确任务终态=" + t.terminal.Kind}, nil)
			}
		} else if t.accepted.EventID != "" {
			sla := time.Duration(e.cfg.Profile(t.accepted.Model).TaskSLASeconds) * time.Second
			if sla <= 0 && t.accepted.N("sla_ms") > 0 {
				sla = time.Duration(t.accepted.N("sla_ms")) * time.Millisecond
			}
			if sla > 0 && e.now.Sub(t.accepted.OccurredAt) > sla {
				facts := []string{"受理时间已超过任务SLA，尚未观察到终态"}
				missing := []string{"缺少近期被动查询证据，不能断言上游任务仍在运行"}
				severity := "P2"
				if t.last.Kind == "media.pending" && e.now.Sub(t.last.OccurredAt) < time.Minute {
					facts = append(facts, "最近一分钟已有上游pending证据")
					missing = nil
					severity = "P1"
				}
				e.add("A01", e.timed(t.accepted, "sla"), scope, "task_state", severity, false, facts, missing)
			}
		}
		grace := time.Duration(e.cfg.BillingGraceSeconds) * time.Second
		if grace <= 0 {
			grace = 120 * time.Second
		}
		if t.completed.N("deliverable") == 1 && t.completed.N("billing_expected") == 1 {
			if t.billed.EventID != "" {
				if fresh(t.billed) {
					e.add("B01", t.billed, scope, "unbilled", "P1", true, []string{"任务已匹配幂等计费成功确认"}, nil)
				}
			} else if e.now.Sub(t.completed.OccurredAt) > grace {
				e.add("B01", e.timed(t.completed, "billing_grace"), scope, "unbilled", "P1", false, []string{fmt.Sprintf("有明确完成及可交付结果；超过%.0f秒宽限尚未匹配计费确认", grace.Seconds())}, []string{"计费证据可能丢失，需只读核对流水后才能确认漏费"})
			}
		}
		for _, o := range t.metadata {
			e.add("B03", o, scope, "metadata", "P1", false, []string{"任务计费证据写入或读取失败=" + o.Reason}, []string{"该事实不能单独证明任务完成或资金损失"})
		}
		if t.billed.EventID != "" && fresh(t.billed) {
			e.add("B03", t.billed, scope, "metadata", "P1", true, []string{"该任务已确认计费成功；保留此前快照缺失记录"}, nil)
		}
	}
	for _, o := range e.rows {
		if o.Quality == domain.Stale || o.Quality == domain.Unsupported {
			continue
		}
		if o.Kind == "billing.hold" && o.N("reserved") == 1 && o.N("expires_at_ms") > 0 && e.now.After(time.UnixMilli(int64(o.N("expires_at_ms")))) {
			e.add("B02", o, taskScope(o), "hold_expired", "P1", false, []string{"资金预占已超过记录的到期时间，仍为reserved状态"}, []string{"尚未核对后台清理任务与实际用户可用额度影响"})
		}
		if o.Kind == "billing.hold" && o.N("reserved") == 0 && fresh(o) {
			e.add("B02", o, taskScope(o), "hold_expired", "P1", true, []string{"预占已不再处于reserved状态"}, nil)
		}
		if o.Kind == "billing.reconciliation" && o.N("legitimate_refund") != 1 {
			mismatch := o.Has("expected") && o.Has("actual") && math.Abs(o.N("expected")-o.N("actual")) > 1e-6
			if o.N("confirmed_duplicate") == 1 || mismatch {
				e.add("B02", o, taskScope(o), "ledger_mismatch", "P1", false, []string{fmt.Sprintf("独立对账重复标记%.0f；预期%.8g，实际%.8g", o.N("confirmed_duplicate"), o.N("expected"), o.N("actual"))}, nil)
			} else if fresh(o) && o.N("reconciled") == 1 {
				e.add("B02", o, taskScope(o), "ledger_mismatch", "P1", true, []string{"独立对账已闭合"}, nil)
			}
		}
	}
}
