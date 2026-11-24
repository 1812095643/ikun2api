package detectors

import (
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
)

func (e *evaluation) system(o domain.Observation) {
	scope := o.TaskID
	if scope == "" {
		scope = o.Source
	}
	switch o.Kind {
	case "source.gap", "source.unavailable":
		e.add("M01", o, scope, "source_state", "P1", false, []string{"证据来源状态=" + o.Kind, "分类=" + o.Reason}, []string{"当前业务状态未知；历史缓存丢失不能保证补齐"})
	case "source.recovered":
		if fresh(o) {
			e.add("M01", o, scope, "source_state", "P1", true, []string{"来源恢复可读取；此前丢失的历史证据仍可能无法还原"}, nil)
		}
	case "dependency.health":
		if o.Has("healthy") && (o.N("healthy") == 0 || fresh(o)) {
			e.add("H01", o, scope, "dependency", "P1", o.N("healthy") == 1 && fresh(o), []string{fmt.Sprintf("本机依赖%s可达标记=%.0f", scope, o.N("healthy"))}, []string{"不可达不能单独区分进程退出、网络限制或资源阻塞"})
		}
	case "host.resources":
		checks := []struct {
			key   string
			low   bool
			limit float64
		}{{"disk_free_percent", true, 10}, {"inode_free_percent", true, 10}, {"memory_used_percent", false, 90}, {"cpu_sustained_percent", false, 90}, {"oom_delta", false, 0}, {"cert_remaining_seconds", true, 7 * 86400}, {"proxy_remaining_seconds", true, 86400}}
		for _, c := range checks {
			if !o.Has(c.key) {
				continue
			}
			bad := o.N(c.key) > c.limit
			if c.low {
				bad = o.N(c.key) < c.limit
			}
			if bad || fresh(o) {
				e.add("H02", o, scope, c.key, "P1", !bad, []string{fmt.Sprintf("%s=%.3f，阈值%.3f", c.key, o.N(c.key), c.limit)}, nil)
			}
		}
	case "delivery.health":
		if o.N("permanent_failure") == 1 || o.N("failed_attempts") >= 3 || o.N("oldest_pending_seconds") > 60 {
			e.add("M02", o, scope, "delivery_state", "P1", false, []string{fmt.Sprintf("发送失败次数%.0f；最老待发%.0f秒；永久失败%.0f", o.N("failed_attempts"), o.N("oldest_pending_seconds"), o.N("permanent_failure"))}, nil)
		} else if o.N("queue_cleared") == 1 && fresh(o) {
			e.add("M02", o, scope, "delivery_state", "P1", true, []string{"超时待发队列已清理；不代表失败消息已送达，请核对逐目标投递状态"}, nil)
		} else if o.N("healthy") == 1 && fresh(o) {
			e.add("M02", o, scope, "delivery_state", "P1", true, []string{"投递链路已恢复平台接收"}, nil)
		}
	}
}
