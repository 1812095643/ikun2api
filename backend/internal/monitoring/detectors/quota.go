package detectors

import (
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"time"
)

func (e *evaluation) quota(o domain.Observation) {
	if o.Reason == "key_daily_limit" {
		return
	}
	if o.Reason == "user_balance_exhausted" || o.Reason == "key_window_limit" {
		e.add("Q01", o, fmt.Sprintf("user:%d/key:%d", o.UserID, o.KeyID), o.Reason, "P3", false, []string{"入口明确返回用户资金来源或Key窗口额度不足"}, []string{"实际可用的其他资金来源未验证"})
	}
	if o.Kind == "request.finished" && o.Outcome == "success" && fresh(o) && o.UserID > 0 && o.KeyID > 0 {
		for _, cause := range []string{"user_balance_exhausted", "key_window_limit"} {
			e.add("Q01", o, fmt.Sprintf("user:%d/key:%d", o.UserID, o.KeyID), cause, "P3", true, []string{"同一用户与Key已有资金校验通过后的成功请求"}, nil)
		}
	}
	if o.Reason == "upstream_quota_exhausted" {
		e.add("Q05", o, cohort(o), "provider_quota", "P1", false, []string{"供应商明确业务码指向额度不足"}, []string{"额度恢复时间与余额需供应商核实"})
	}
	if o.Kind == "quota.snapshot" && o.Has("remaining") {
		scope := fmt.Sprintf("user:%d/source:%s/%s", o.UserID, o.Reason, o.TaskID)
		remaining := o.N("remaining")
		facts := []string{fmt.Sprintf("资金来源=%s，当前剩余额度=%.8g", o.Reason, remaining)}
		if remaining <= 0 || fresh(o) {
			e.add("Q01", o, scope, "exhausted", "P3", remaining > 0, facts, nil)
		}
		var threshold *float64
		if o.Reason == "balance" {
			threshold = e.cfg.BalanceLow
		} else if o.Reason == "subscription" && e.cfg.SubscriptionLow != nil && o.N("total") > 0 {
			v := *e.cfg.SubscriptionLow * o.N("total")
			threshold = &v
		}
		if threshold != nil && *threshold > 0 {
			if remaining <= *threshold {
				e.add("Q02", o, scope, "low_water", "P3", false, append(facts, fmt.Sprintf("低水位=%.8g", *threshold)), nil)
			} else if remaining > *threshold*1.1 && fresh(o) {
				e.add("Q02", o, scope, "low_water", "P3", true, facts, nil)
			}
		}
	}
	if o.Kind == "quota.snapshot" && o.Reason == "subscription" {
		for _, window := range []string{"daily", "weekly", "monthly"} {
			metric := window + "_remaining"
			if !o.Has(metric) {
				continue
			}
			remaining := o.N(metric)
			if remaining <= 0 || fresh(o) {
				e.add("Q01", o, fmt.Sprintf("user:%d/source:subscription/%s/window:%s", o.UserID, o.TaskID, window), "exhausted", "P3", remaining > 0, []string{fmt.Sprintf("套餐%s窗口剩余额度%.8g", window, remaining)}, nil)
			}
		}
	}
	if latest, ok := e.latestPayments[o.Site+":"+taskScope(o)]; o.Kind == "payment.snapshot" && ok && latest.EventID == o.EventID && latest.Source == o.Source && o.N("paid") == 1 && o.N("paid_at_ms") > 0 {
		grace := time.Duration(e.cfg.PaymentGraceSeconds) * time.Second
		if grace <= 0 {
			grace = 300 * time.Second
		}
		if o.N("fulfilled") == 1 {
			if fresh(o) {
				e.add("Q03", o, taskScope(o), "fulfillment", "P1", true, []string{"订单已完成权益发放"}, nil)
			}
		} else if o.Has("fulfilled") && o.N("fulfilled") == 0 && e.now.Sub(time.UnixMilli(int64(o.N("paid_at_ms")))) > grace {
			e.add("Q03", e.timed(o, "grace"), taskScope(o), "fulfillment", "P1", false, []string{"已有最终支付成功证据，超过处理宽限仍未记录权益完成"}, []string{"尚未确定是任务延迟还是账务处理失败"})
		}
	}
}
