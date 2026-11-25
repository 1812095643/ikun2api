// Fake monitor API data for the isolated preview (MONITOR_PREVIEW_MOCK=1).
const min = 60000
const iso = (msAgo: number) => new Date(Date.now() - msAgo).toISOString()
const explanation = (conclusion: string, facts: string[], advice: string[], missing: string[] = []) => ({ conclusion, confidence: missing.length ? 'medium' : 'high', facts, inferences: [], missing_evidence: missing, advice })

export const names = {
  'user:6': { name: '小明', email: 'xiaoming@example.com' }, 'user:9': { name: '阿强', email: 'aqiang@example.com' }, 'user:12': { name: 'studio-team' },
  'key:34': { name: '开发 Key', owner: 9 }, 'account:31': { name: 'apipros' }, 'account:33': { name: 'yeahzi-生图' }, 'account:58': { name: 'claude-max-02' },
  'group:23': { name: 'Claude Max' }, 'group:27': { name: 'Claude 默认' }, 'group:25': { name: 'GPT 通用' },
}

const base = { rule_version: 2, revision: 12, acknowledged: false, silenced_until: '0001-01-01T00:00:00Z', quality: 'fresh', account_count: 1 }
export const incidents = [
  { ...base, id: 'inc-acct31', fingerprint: 'rollup:account:31', rule_id: 'U02', scope: 'account:31', rollup: true, status: 'firing', severity: 'P1', first_seen: iso(135 * min), last_seen: iso(2 * min), last_fault: iso(2 * min), count: 854, request_count: 854, user_count: 2,
    members: [{ rule: 'U02', model: 'claude-opus-5-5', count: 610, last: iso(2 * min) }, { rule: 'F01', model: 'claude-opus-5-5', count: 190, last: iso(4 * min) }, { rule: 'R01', model: 'claude-sonnet-5', count: 54, last: iso(9 * min) }],
    explanation: explanation('上游账号 #31 异常汇总（3 类问题，2 个模型）', ['U02 上游授权、权限或接口异常 · 模型 claude-opus-5-5 · 本轮命中 610 次', 'F01 请求发生流内或应用语义失败 · 模型 claude-opus-5-5 · 本轮命中 190 次', '最近一条（U02）：上游尝试返回404；账号31'], ['核对账号授权、模型权限和上游错误码；仅凭401/403不能判断封号。', '检查流内错误和终态，HTTP200本身不是成功证据。'], ['状态码不能单独证明余额耗尽、封号或供应商内部故障']) },
  { ...base, id: 'inc-group23', fingerprint: 'rollup:group:23:S01', rule_id: 'S01', scope: 'group:23', rollup: true, status: 'firing', severity: 'P1', first_seen: iso(18 * min), last_seen: iso(1 * min), last_fault: iso(1 * min), count: 41, request_count: 41, user_count: 3, members: [{ rule: 'S01', model: 'claude-fable-5-2', count: 41, last: iso(min) }],
    explanation: explanation('分组 #23 突然无可用候选账号（1 个模型）', ['S01 分组与模型无可用候选账号 · 模型 claude-fable-5-2 · 本轮命中 41 次', '最近一条（S01）：候选总数2，可调度0，模型限流过滤0，临时不可调度2'], ['检查候选过滤原因、路由分组、临时隔离与授权；不要只反复重试。']) },
  { ...base, id: 'inc-l03', fingerprint: 'l03', rule_id: 'L03', scope: 'account:58/group:27/model:claude-opus-5-5', status: 'firing', severity: 'P2', acknowledged: true, first_seen: iso(40 * min), last_seen: iso(6 * min), last_fault: iso(6 * min), count: 7, request_count: 7, user_count: 1,
    explanation: explanation('多个请求持续等待有效输出', ['10 分钟内 7 个请求首个输出超过 60 秒', '同模型其他账号中位 4.2 秒'], ['对照同模型其他账号的首字分布；不要仅按请求最终成功判断体验。']) },
  { ...base, id: 'inc-q01', fingerprint: 'q01', rule_id: 'Q01', scope: 'user:12', status: 'recovering', severity: 'P3', first_seen: iso(300 * min), last_seen: iso(20 * min), last_fault: iso(50 * min), count: 3, request_count: 0, user_count: 1, explanation: explanation('用户资金来源额度耗尽', ['钱包余额 0.00 美元'], ['核对该用户当前使用的钱包或套餐；确认充值或切换资金来源。']) },
  { ...base, id: 'inc-m01', fingerprint: 'm01', rule_id: 'M01', scope: 'gateway', status: 'resolved', severity: 'P1', first_seen: iso(90 * min), last_seen: iso(70 * min), last_fault: iso(88 * min), count: 2, request_count: 0, user_count: 0, explanation: explanation('监控证据来源缺失或不兼容', ['来源恢复可读取'], ['先恢复采集并核对版本']) },
  { ...base, id: 'inc-acct33', fingerprint: 'rollup:account:33', rule_id: 'U01', scope: 'account:33', rollup: true, status: 'resolved', severity: 'P2', first_seen: iso(400 * min), last_seen: iso(300 * min), last_fault: iso(330 * min), count: 34, request_count: 34, user_count: 1, members: [{ rule: 'U01', model: 'gpt-6.2-sol', count: 34, last: iso(330 * min) }], explanation: explanation('上游账号 #33 异常汇总', ['上游尝试返回503'], ['检查供应商状态']) },
  { ...base, id: 'inc-child1', parent_id: 'inc-acct31', fingerprint: 'c1', rule_id: 'U02', scope: 'account:31/group:23/model:claude-opus-5-5', status: 'firing', severity: 'P1', first_seen: iso(135 * min), last_seen: iso(2 * min), last_fault: iso(2 * min), count: 610, request_count: 610, user_count: 2, explanation: explanation('上游授权、权限或接口异常', ['上游尝试返回404；账号31'], ['核对账号授权']) },
  { ...base, id: 'inc-child2', parent_id: 'inc-acct31', fingerprint: 'c2', rule_id: 'F01', scope: 'account:31/group:27/model:claude-opus-5-5', status: 'recovering', severity: 'P1', first_seen: iso(120 * min), last_seen: iso(4 * min), last_fault: iso(4 * min), count: 190, request_count: 190, user_count: 1, explanation: explanation('请求发生流内或应用语义失败', ['响应流出现明确错误事件'], ['检查流内错误']) },
]

export function live() {
  const now = Date.now(); const start = Math.floor(now / min) * min - 59 * min
  const buckets = Array.from({ length: 60 }, (_, n) => {
    const requests = Math.max(0, Math.round(6 + 4 * Math.sin(n / 6) + (n % 7)))
    const failed = n > 40 ? (n % 3 === 0 ? 2 : n % 5 === 0 ? 1 : 0) : n === 12 ? 1 : 0
    const cancelled = n % 11 === 0 ? 1 : 0
    return { at: new Date(start + n * min).toISOString(), requests, failed, cancelled, succeeded: Math.max(0, requests - failed - cancelled), upstream_errors: n > 40 && n % 2 === 0 ? 3 : 0 }
  })
  const totals = buckets.reduce((t, b) => ({ requests: t.requests + b.requests, succeeded: t.succeeded + b.succeeded, failed: t.failed + b.failed, cancelled: t.cancelled + b.cancelled, upstream_errors: t.upstream_errors + b.upstream_errors }), { requests: 0, succeeded: 0, failed: 0, cancelled: 0, upstream_errors: 0 })
  return { from: buckets[0].at, to: new Date(now).toISOString(), buckets, totals, inflight_count: 3, first_output_p50_ms: 3400, first_output_p95_ms: 14800, names, checked_at: new Date(now).toISOString(),
    inflight: [
      { request: 'r1', model: 'claude-opus-5-5', user_id: 6, key_id: 0, account_id: 58, group_id: 27, elapsed_ms: 73000, waiting_first_output: true },
      { request: 'r2', model: 'claude-sonnet-5', user_id: 0, key_id: 34, account_id: 31, group_id: 23, elapsed_ms: 21000, waiting_first_output: false },
      { request: 'r3', model: 'gpt-6.2-sol', user_id: 12, key_id: 0, account_id: 0, group_id: 25, elapsed_ms: 2400, waiting_first_output: true },
    ],
    problem_accounts: [{ account_id: 31, requests: 96, failed: 14, upstream_errors: 31, statuses: { '404': 22, '503': 6, 传输错误: 3 } }, { account_id: 58, requests: 120, failed: 0, upstream_errors: 4, statuses: { '429': 4 } }] }
}

export function detail(id: string) {
  const incident = incidents.find(i => i.id === id) || incidents[0]
  const hits = [] as { at: string; healthy: boolean }[]
  for (let n = 0; n < 160; n++) hits.push({ at: iso((n % 9) * 3 * min + (n < 120 ? 0 : 600) * min), healthy: n % 13 === 0 })
  return { incident, history: Array.from({ length: 12 }, (_, n) => ({ ...incident, revision: 12 - n, status: n % 4 === 1 ? 'recovering' : n === 11 ? 'candidate' : 'firing', last_seen: iso(n * 11 * min) })),
    children: incidents.filter(c => c.parent_id === incident.id), impact: { users: [6, 9, 12], accounts: [31] }, hits, names }
}

export const deliveries = [
  { id: 'd1', incident_id: 'inc-acct31', revision: 3, channel: 'feishu', target: 'oc_a1b2c3d4e5f6a7b8', kind: 'firing', state: 'accepted', attempts: 1, accepted_at: iso(130 * min), next_attempt: iso(130 * min) },
  { id: 'd2', incident_id: 'inc-group23', revision: 1, channel: 'feishu', target: 'oc_a1b2c3d4e5f6a7b8', kind: 'firing', state: 'accepted', attempts: 1, accepted_at: iso(17 * min), next_attempt: iso(17 * min) },
  { id: 'd3', incident_id: 'inc-group23', revision: 1, channel: 'email', target: 'ops@example.com', kind: 'firing', state: 'dead', attempts: 8, error_class: 'smtp_transport_or_rejected', accepted_at: '0001-01-01T00:00:00Z', next_attempt: iso(10 * min) },
  { id: 'd4', incident_id: 'inc-m01', revision: 4, channel: 'feishu', target: 'oc_a1b2c3d4e5f6a7b8', kind: 'update', state: 'accepted', attempts: 1, accepted_at: iso(70 * min), next_attempt: iso(70 * min) },
]
export const sources = () => ['gateway', 'postgres.errors', 'postgres.attempts', 'postgres.usage', 'postgres.accounts', 'postgres.balances', 'local.health', 'postgres.usage.reconcile'].map((source_id, n) => ({ source_id, last_success: iso(source_id === 'postgres.balances' ? 6 * min : n * 2000), quality: source_id.startsWith('postgres') ? 'partial' : 'fresh', dropped: 0 }))
