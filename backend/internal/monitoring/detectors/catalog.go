package detectors

type Definition struct {
	ID              string   `json:"id"`
	Title           string   `json:"title"`
	Advice          string   `json:"advice"`
	RecoverySamples int      `json:"recovery_samples"`
	RecoverySeconds int      `json:"recovery_seconds"`
	Requires        []string `json:"requires"`
}

var Catalog = []Definition{
	{"Q01", "用户资金来源额度耗尽", "核对该用户当前使用的钱包或套餐；确认充值或切换资金来源。", 1, 0, []string{"quota.snapshot"}},
	{"Q02", "用户额度低于预警水位", "检查实际剩余额度与使用趋势，按需提醒补充额度。", 1, 0, []string{"quota.snapshot", "configured_threshold"}},
	{"Q03", "已支付订单尚未完成权益发放", "核对支付终态、订单与权益流水；确认幂等状态后再人工处理。", 1, 0, []string{"payment.snapshot"}},
	{"Q04", "正常 Key 日限额（仅归档）", "正常主动限制，不发送故障通知。", 1, 0, []string{"explicit_key_daily_code"}},
	{"Q05", "上游明确报告额度不足", "核对供应商该账号的额度窗口或余额，不要把普通429当余额耗尽。", 5, 300, []string{"explicit_provider_quota_code"}},
	{"U01", "上游429或5xx异常", "检查供应商状态、账号限流窗口和重试结果；已切换成功不代表原账号恢复。", 5, 300, []string{"attempt_status"}},
	{"U02", "上游授权、权限或接口异常", "核对账号授权、模型权限和上游错误码；仅凭401/403不能判断封号。", 5, 300, []string{"attempt_status"}},
	{"U03", "上游连接或传输异常", "按已确认的DNS、连接、TLS、代理或超时层级检查链路。", 5, 300, []string{"transport_reason"}},
	{"S01", "分组与模型无可用候选账号", "检查候选过滤原因、路由分组、临时隔离与授权；不要只反复重试。", 1, 0, []string{"pool_counts"}},
	{"S02", "账号池可用容量持续下降", "核对近期账号状态与隔离变化，确认剩余容量是否能承载请求。", 5, 120, []string{"pool_counts_history"}},
	{"S03", "账号隔离或授权刷新失败", "检查隔离触发证据、代理探测或授权刷新；冷却到期不等于已恢复。", 1, 0, []string{"account_lifecycle"}},
	{"L01", "请求排队过长", "区分用户槽与账号槽，检查并发占用、候选容量和排队时长。", 1, 0, []string{"active_queue"}},
	{"L02", "单次首字等待记录（不单独通知）", "由L03判断持续影响；核对上传、排队、转发前耗时和上游输出；心跳不代表有效输出。", 1, 0, []string{"active_first_output"}},
	{"L03", "多个请求持续等待有效输出", "对照同模型其他账号的首字分布；不要仅按请求最终成功判断体验。", 1, 0, []string{"client_first_output_samples"}},
	{"L04", "同渠道多个请求疑似输出停顿", "10分钟至少3个不同请求且占可评估请求20%才提醒；核对输出间隔和上游链路，不将隐藏思考或证据缺失判为故障。", 1, 0, []string{"active_progress"}},
	{"F01", "请求发生流内或应用语义失败", "检查流内错误和终态，HTTP200本身不是成功证据。", 5, 300, []string{"protocol_terminal"}},
	{"F02", "输出为空、协议不完整或模型异常", "核对实际协议、终态和模型映射；合法推理与工具调用不是空答案。", 5, 300, []string{"protocol_terminal"}},
	{"R01", "请求重试或换号频繁", "核对各次尝试原因、实际用户等待时间及调度策略。", 5, 300, []string{"attempts_and_switches"}},
	{"R02", "调度命中已隔离账号", "核对选择时点与隔离生效世代；保护门拒绝不等于已经向该账号转发。", 1, 0, []string{"selection_quarantine_generation"}},
	{"C01", "客户端取消增多且伴随体验下降", "对照慢请求和失败趋势；需要客户端证据才能区分主动取消或网络问题。", 5, 300, []string{"cancellation_baseline"}},
	{"A01", "异步任务延迟、失败或状态未知", "检查已有任务终态和查询证据；不要新增可能计费的主动状态轮询。", 1, 0, []string{"task_lifecycle", "configured_sla"}},
	{"B01", "确认完成的任务尚未匹配计费", "只读核对任务与计费流水、宽限和幂等键；不要自动补扣。", 1, 0, []string{"deliverable", "billing_confirmation"}},
	{"B02", "账务对账或预占释放异常", "核对有效扣费、冲正与退款、预占期限；不要把重试事件当重复扣费。", 1, 0, []string{"reconciliation_or_holds"}},
	{"B03", "任务资金归属或计费快照缺失", "核对具体写入失败及任务终态；缺快照不等于视频已完成或已漏费。", 1, 0, []string{"billing_metadata"}},
	{"H01", "本机服务或依赖不可用", "检查本机进程及依赖；当前证据不等于整机宕机结论。", 1, 0, []string{"local_health"}},
	{"H02", "本机资源不足或配置将到期", "检查资源用量、磁盘或已配置的证书和代理期限。", 3, 120, []string{"local_resources"}},
	{"M01", "监控证据来源缺失或不兼容", "先恢复采集并核对版本；证据缺失期间不能断言业务正常。", 1, 0, []string{"source_health"}},
	{"M02", "通知链路失败或积压", "检查发送权限、凭据和飞书限流；平台接收不等于人已阅读。", 1, 0, []string{"delivery_health"}},
}

func DefinitionFor(id string) Definition {
	for _, d := range Catalog {
		if d.ID == id {
			return d
		}
	}
	return Definition{ID: id}
}
