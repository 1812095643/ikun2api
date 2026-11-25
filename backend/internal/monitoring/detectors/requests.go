package detectors

import (
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"time"
)

func earlyHiddenReasoning(o domain.Observation, warningSeconds int) bool {
	return o.N("reasoning_started") == 1 && (!o.Has("reasoning_started_ms") || o.N("reasoning_started_ms") < float64(warningSeconds*1000))
}

func (e *evaluation) request(o domain.Observation) {
	if e.signature(o) {
		return
	}
	scope := cohort(o)
	req := requestKey(o)
	p := e.cfg.Profile(o.Model)
	if (o.Kind == "attempt.failed" || o.Kind == "request.error") && o.UpstreamStatus >= 400 {
		rule := "U02"
		switch o.UpstreamStatus {
		case 429, 502, 503, 504:
			rule = "U01"
		}
		severity := "P2"
		if o.Kind == "request.error" && o.N("intended_status") >= 400 {
			severity = "P1"
		}
		end, known := e.ends[req]
		if known && end.Outcome == "failed" {
			severity = "P1"
		}
		f := e.add(rule, o, scope, fmt.Sprintf("status:%d", o.UpstreamStatus), severity, false, []string{fmt.Sprintf("上游尝试返回%d；账号%d", o.UpstreamStatus, o.AccountID)}, []string{"状态码不能单独证明余额耗尽、封号或供应商内部故障"})
		if f != nil && known && end.Outcome == "success" {
			f.Explanation.Facts = append(f.Explanation.Facts, "本请求后续尝试已成功；原账号恢复尚未验证")
		}
	}
	if o.Kind == "attempt.failed" || o.Kind == "request.error" {
		switch o.Reason {
		case "dns", "connect", "tls", "proxy", "timeout", "transport_unknown":
			e.add("U03", o, scope, o.Reason, "P1", false, []string{"传输错误层级=" + o.Reason}, []string{"更深层网络根因尚无证据"})
		}
	}
	if o.Reason == "key_daily_limit" {
		return
	}
	if o.Kind == "request.error" {
		switch o.Reason {
		case "pool_empty":
			e.add("S01", o, poolScope(o), "empty", "P1", false, []string{"网关选号返回无可用账号"}, []string{"旧记录没有完整候选计数及排除原因，不能推断额度耗尽或代理故障"})
		case "slot_timeout":
			e.add("L01", o, scope, "legacy_concurrency", "P1", false, []string{"网关并发槽获取被拒绝或等待超时"}, []string{"未记录用户槽或账号槽的完整等待分段"})
		case "upstream_failure", "request_failure":
			if o.Reason == "upstream_failure" && o.N("intended_status") < 400 {
				e.add("U02", o, scope, "legacy_status_unknown", "P2", false, []string{"旧日志记录上游尝试错误，但未保存上游状态码"}, []string{"外层200不能区分重试成功与流内失败；未确认客户端最终结果"})
				break
			}
			e.add("F01", o, scope, "request_failure", "P1", false, []string{fmt.Sprintf("数据库记录请求错误；客户端状态%.0f，上游状态未记录", o.N("intended_status"))}, []string{"不能凭外层200认定业务成功；旧日志未提供完整协议终态与更深层原因"})
		case "client_request_rejected", "model_unavailable":
			e.add("F02", o, poolScope(o), o.Reason, "P2", false, []string{fmt.Sprintf("网关拒绝请求，分类=%s，状态%.0f", o.Reason, o.N("intended_status"))}, []string{"需要核对请求参数、模型映射及路由权限；不能认定上游故障"})
		}
	}
	if o.Kind == "usage.recorded" {
		if o.Has("client_first_token_ms") && o.N("client_first_token_ms") >= float64(p.FirstWarnSeconds*1000) {
			facts := []string{fmt.Sprintf("已完成请求端到端首字耗时%.1f秒", o.N("client_first_token_ms")/1000)}
			if o.Has("upload_ms") {
				facts = append(facts, fmt.Sprintf("请求上传耗时%.1f秒", o.N("upload_ms")/1000))
			}
			if o.Has("pre_forward_ms") {
				facts = append(facts, fmt.Sprintf("转发前总耗时%.1f秒（包含上传与排队）", o.N("pre_forward_ms")/1000))
			}
			if o.Has("first_token_ms") {
				facts = append(facts, fmt.Sprintf("最后一次上游转发首字耗时%.1f秒", o.N("first_token_ms")/1000))
			}
			e.add("L02", o, scope, "completed_first_output", "P2", false, facts, []string{"来自用量记录；不能单凭上传耗时判断客户端网络或服务器接收端原因"})
		}
		if o.N("switches") >= 2 {
			e.add("R01", o, scope, "request_retries", "P2", false, []string{fmt.Sprintf("已完成请求记录%.0f次换号", o.N("switches"))}, nil)
		}
	}
	if o.Kind == "request.active" && e.now.Sub(o.ObservedAt) <= 15*time.Second && o.N("realtime") != 1 {
		warn := func(rule, cause string, ms float64, low, high int) {
			if ms < float64(low*1000) {
				return
			}
			severity := "P2"
			if ms >= float64(high*1000) {
				severity = "P1"
			}
			e.add(rule, o, req, cause, severity, false, []string{fmt.Sprintf("在途%s持续%.1f秒", cause, ms/1000)}, []string{"具体耗时分段仅以已有字段为准；未采集部分未知"})
		}
		if o.N("queue_active") == 1 {
			warn("L01", "queue", o.N("queue_ms"), p.QueueWarnSeconds, p.QueueFailSeconds)
		}
		if o.N("has_output") != 1 && !earlyHiddenReasoning(o, p.FirstWarnSeconds) && o.Quality == domain.Fresh {
			warn("L02", "first_output", o.N("elapsed_ms"), p.FirstWarnSeconds, p.FirstFailSeconds)

		}
	}
	if o.Kind == "stream.failed" && o.Reason != "client_write_failed" {
		e.add("F01", o, scope, "request_failure", "P1", false, []string{"响应流出现明确错误事件；外层HTTP状态可能仍为200"}, nil)
	}
	if o.Kind == "request.finished" {
		if o.Reason == "output_budget_exhausted" {
			f := e.add("F02", o, scope, "output_budget_exhausted", "P2", false, []string{"上游明确以max_tokens结束；已观测流未出现正文或工具输出", "该请求的输出token预算耗尽，不等于用户余额或上游账户额度耗尽"}, []string{"未读取请求正文，不能判断预算由客户端还是上游限制"})
			if f != nil {
				f.Explanation.Conclusion = "输出预算耗尽，未返回正文或工具结果"
				f.Explanation.Advice = []string{"提高请求max_tokens或降低思考强度，再检查是否正常返回答案"}
			}
		}
		if o.Outcome == "failed" && o.ClientStatus >= 500 {
			e.add("F01", o, scope, "request_failure", "P1", false, []string{fmt.Sprintf("请求最终HTTP状态%d，未标记为成功", o.ClientStatus)}, []string{"未确认该错误发生在网关内部还是上游"})
		}
		if o.Reason == "missing_terminal" || o.Reason == "empty_output" || o.Reason == "wrong_protocol" || o.N("model_mismatch") == 1 {
			e.add("F02", o, scope, "protocol_integrity", "P1", false, []string{"终态/输出检查=" + o.Reason}, []string{"需核对已配置模型映射，不能仅凭上游模型名称差异判错误"})
		}
		if fresh(o) && o.N("first_effective_ms") >= float64(p.FirstWarnSeconds*1000) && o.N("has_output") == 1 && !earlyHiddenReasoning(o, p.FirstWarnSeconds) {
			e.add("L02", o, scope, "completed_first_output", "P2", false, []string{fmt.Sprintf("首个有效输出耗时%.1f秒", o.N("first_effective_ms")/1000)}, nil)
		}
		if o.Outcome == "success" && fresh(o) {
			if o.Has("first_effective_ms") && o.N("first_effective_ms") < float64(p.FirstWarnSeconds*1000) {
				e.add("L02", o, scope, "completed_first_output", "P2", true, []string{"同渠道模型出现及时首字的成功请求，仍需稳定健康窗口"}, nil)
			}
			for _, r := range []struct{ id, cause string }{{"L01", "queue"}, {"L02", "first_output"}} {
				e.add(r.id, o, req, r.cause, "P2", true, []string{"该请求已出现明确成功终态；不表示原渠道整体恢复"}, nil)
			}
		}
	}
	if o.N("attempts") >= 3 || o.N("switches") >= 2 {
		if o.Kind == "request.finished" {
			e.add("R01", o, scope, "request_retries", "P2", false, []string{fmt.Sprintf("尝试数%.0f，换号数%.0f", o.N("attempts"), o.N("switches"))}, nil)
		}
	}
	if o.Kind == "request.finished" && o.Outcome == "success" && fresh(o) && o.Has("attempts") && o.N("attempts") < 3 && o.N("switches") < 2 {
		e.add("R01", o, scope, "request_retries", "P2", true, []string{"该渠道出现无过量重试的成功样本，仍需稳定窗口"}, nil)
	}
	if o.Source == "gateway" && o.Kind == "request.finished" && o.Outcome == "unknown" && o.Quality == domain.Partial {
		lifecycle := o
		lifecycle.Quality = domain.Fresh
		for _, r := range []struct{ id, cause string }{{"L01", "queue"}, {"L02", "first_output"}} {
			e.add(r.id, lifecycle, req, r.cause, "P2", true, []string{"请求已结束，在途等待不再持续；响应结果因协议证据不足仍未知"}, []string{"结束不代表业务成功或渠道恢复"})
		}
	}
	if (o.Kind == "request.finished" && o.Outcome == "failed" || o.Kind == "request.cancelled") && fresh(o) {
		for _, r := range []struct{ id, cause string }{{"L01", "queue"}, {"L02", "first_output"}} {
			e.add(r.id, o, req, r.cause, "P2", true, []string{"该请求等待已结束，结果=" + o.Outcome + "；不代表请求成功或渠道恢复"}, nil)
		}
	}

	if o.Kind == "account.selected_quarantined" {
		if o.Has("selected_at_ms") && o.Has("quarantine_since_ms") && o.N("selected_at_ms") >= o.N("quarantine_since_ms") {
			e.add("R02", o, fmt.Sprintf("account:%d", o.AccountID), "selected_quarantined", "P1", false, []string{"隔离已生效后仍被选择；保护门已拒绝本次转发", "隔离世代=" + o.TaskID}, nil)
		}
	}
}
