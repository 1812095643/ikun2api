import { mount, flushPromises } from '@vue/test-utils'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import MonitorPanel from '../MonitorPanel.vue'
import MonitorConfigForm from '../MonitorConfigForm.vue'
import MonitorIncidentDrawer from '../MonitorIncidentDrawer.vue'
import { incidentTitle, scopeParts, span, userOf } from '../monitorView'
import { monitorAPI, type MonitorIncident, type MonitorLive, type MonitorSummary } from '@/api/admin/monitor'
vi.mock('@/api/admin/monitor', async importOriginal => ({ ...await importOriginal<typeof import('@/api/admin/monitor')>(), monitorAPI: { summary: vi.fn(), live: vi.fn(), detail: vi.fn(), save: vi.fn(), control: vi.fn(), channels: vi.fn(), saveChannels: vi.fn(), feishuChats: vi.fn(), testChannel: vi.fn() } }))

const now = Date.now()
const iso = (ago: number) => new Date(now - ago).toISOString()
const names = { 'account:31': { name: 'apipros' }, 'group:23': { name: 'Claude Max' }, 'user:6': { name: '小明', email: 'xm@example.com' }, 'key:34': { name: '开发 Key', owner: 9 }, 'user:9': { name: '阿强' } }
const rules = [{ id: 'U02', title: '上游授权、权限或接口异常', advice: '', requires: [], recovery_samples: 5, recovery_seconds: 300 }, { id: 'F01', title: '请求发生流内或应用语义失败', advice: '', requires: [], recovery_samples: 5, recovery_seconds: 300 }]
const inc = (over: Partial<MonitorIncident>): MonitorIncident => ({ id: 'inc-1', fingerprint: 'f', rule_id: 'U02', rule_version: 2, scope: 'account:31/group:23/model:claude-opus-5-5', revision: 3, status: 'firing', severity: 'P1', first_seen: iso(3600000), last_seen: iso(60000), last_fault: iso(60000), count: 9, request_count: 9, user_count: 2, account_count: 1, acknowledged: false, silenced_until: '0001-01-01T00:00:00Z', quality: 'fresh', explanation: { conclusion: 'x', confidence: 'high', facts: ['上游尝试返回404；账号31'], inferences: [], missing_evidence: [], advice: ['核对账号授权'] }, ...over })
const summary = (incidents: MonitorIncident[] = []): MonitorSummary => ({ build: 't', config: { version: 1, actor_id: 'a', idempotency_key: 'k', shadow: false, site_id: 't', rules: {}, profiles: {}, balance_low_usd: null, subscription_low_ratio: null, feishu_targets: [], email_targets: [], detail_base_url: '', payment_grace_seconds: 300, billing_grace_seconds: 120, owner_overrides: [], legacy_handoffs: [] }, incidents, deliveries: [], sources: [{ source_id: 'gateway', last_success: iso(1000), quality: 'fresh', dropped: 0 }], capabilities: [], rules, coverage_gaps: ['钱包独立流水缺失'], transports: { feishu: true, email: false }, checked_at: iso(0), list_limit: 1000, names })
const live = (over: Partial<MonitorLive> = {}): MonitorLive => ({ from: iso(3600000), to: iso(0), buckets: Array.from({ length: 60 }, (_, n) => ({ at: iso((59 - n) * 60000), requests: 5, succeeded: n === 59 ? 3 : 5, failed: n === 59 ? 2 : 0, cancelled: 0, upstream_errors: 0 })), totals: { requests: 300, succeeded: 298, failed: 2, cancelled: 0, upstream_errors: 4 }, inflight_count: 1, inflight: [{ request: 'r', model: 'claude-opus-5-5', user_id: 0, key_id: 34, account_id: 31, group_id: 23, elapsed_ms: 70000, waiting_first_output: true }], problem_accounts: [{ account_id: 31, requests: 20, failed: 2, upstream_errors: 4, statuses: { 404: 4 } }], first_output_p50_ms: 3200, first_output_p95_ms: 9000, names, checked_at: iso(0), ...over })

beforeEach(() => { vi.clearAllMocks(); vi.useFakeTimers({ shouldAdvanceTime: true }) })
afterEach(() => { vi.useRealTimers(); document.body.innerHTML = '' })

describe('monitor display helpers', () => {
  it('turns scopes and keys into names', () => {
    expect(scopeParts(inc({}), names).map(p => p.label)).toEqual(['apipros', 'Claude Max', 'claude-opus-5-5'])
    expect(scopeParts(inc({ scope: 'account:77' }), names)[0].label).toBe('上游账号 #77')
    expect(scopeParts(inc({ scope: 'state_capacity' }), names)[0].label).toBe('监控存储容量')
    expect(userOf(names, 0, 34)?.label).toBe('阿强')
    expect(incidentTitle(inc({ rollup: true, scope: 'account:31' }), rules, names)).toBe('上游账号「apipros」出现异常')
    expect(span(2 * 3600000 + 15 * 60000)).toBe('2 小时 15 分')
  })
})

describe('monitor console', () => {
  it('leads with a plain status sentence, the live hour and named problems', async () => {
    vi.mocked(monitorAPI.summary).mockResolvedValue(summary([inc({}), inc({ id: 'child', parent_id: 'inc-1', rule_id: 'F01' })]))
    vi.mocked(monitorAPI.live).mockResolvedValue(live())
    const view = mount(MonitorPanel); await flushPromises()
    expect(view.get('[data-testid="headline"]').text()).toBe('近 1 小时有 1 个问题，其中 1 个严重，涉及 1 个上游账号')
    expect(view.text()).toContain('过去 60 分钟完成 300 个请求，失败 2 个（0.7%）')
    expect(view.text()).toContain('上游授权、权限或接口异常')
    expect(view.text()).toContain('apipros'); expect(view.text()).toContain('Claude Max')
    expect(view.text()).not.toContain('请求发生流内或应用语义失败') // merged child stays inside its rollup
    expect(view.text()).toContain('阿强'); expect(view.text()).toContain('还没有输出') // in-flight request by key owner
    expect(view.text()).toContain('404 ×4')
    expect(view.find('a[href="/admin/ops?open_error_details=1&error_type=upstream&account_id=31"]').exists()).toBe(true)
    expect(view.find('a[href$="account_id=31&status_code=404"]').exists()).toBe(true)
    expect(view.text()).not.toContain('心链词元')
    view.unmount()
  })
  it('only counts faults inside the chosen window and keeps state problems until recovery', async () => {
    const quietRules = [{ ...rules[0], quiet_close: true, quiet_close_seconds: 7200 }, { id: 'S03', title: '账号隔离或授权刷新失败', advice: '', requires: [], recovery_samples: 1, recovery_seconds: 0, quiet_close: true, quiet_close_seconds: 86400 }]
    const s = summary([
      inc({ id: 'now', last_fault: iso(5 * 60000) }),
      inc({ id: 'half', scope: 'account:32', severity: 'P2', last_fault: iso(40 * 60000) }),
      inc({ id: 'old', scope: 'account:33', last_fault: iso(7 * 3600000) }),
      inc({ id: 'watch', scope: 'account:34', status: 'recovering', last_fault: iso(3 * 3600000) }),
      inc({ id: 'quarantine', rule_id: 'S03', scope: 'account:35', last_fault: iso(9 * 3600000) }),
    ])
    s.rules = quietRules
    vi.mocked(monitorAPI.summary).mockResolvedValue(s); vi.mocked(monitorAPI.live).mockResolvedValue(live())
    localStorage.removeItem('ops-monitor-window')
    const view = mount(MonitorPanel); await flushPromises()
    expect(view.get('[data-testid="headline"]').text()).toBe('近 1 小时有 2 个问题，其中 1 个严重，涉及 2 个上游账号')
    expect(view.text()).toContain('另有 1 个较早开始的状态问题')
    expect(view.text()).toContain('较早开始、尚未确认恢复')
    expect(view.text()).not.toContain('恢复观察中')
    expect(view.findAll('button[aria-label^="查看："]').length).toBe(3) // 2 recent + quarantine
    await view.findAll('button').find(b => b.text() === '10 分钟')!.trigger('click')
    expect(view.get('[data-testid="headline"]').text()).toBe('近 10 分钟有 1 个问题，其中 1 个严重，涉及 1 个上游账号')
    expect(localStorage.getItem('ops-monitor-window')).toBe('10')
    view.unmount()
    localStorage.removeItem('ops-monitor-window')
  })
  it('leaves out rules switched off in settings', async () => {
    const s = summary([inc({}), inc({ id: 'quota', rule_id: 'Q02', scope: 'user:9/source:balance/', severity: 'P3' })])
    s.config.rules = { Q02: { enabled: false, notification_owner: 'monitor', version: 1 } }
    s.rules = [...rules, { id: 'Q02', title: '用户额度低于预警水位', advice: '', requires: [], recovery_samples: 1, recovery_seconds: 0 }]
    vi.mocked(monitorAPI.summary).mockResolvedValue(s); vi.mocked(monitorAPI.live).mockResolvedValue(live())
    const view = mount(MonitorPanel); await flushPromises()
    expect(view.text()).not.toContain('用户额度低于预警水位')
    expect(view.findAll('button[aria-label^="查看："]').length).toBe(1)
    view.unmount()
  })
  it('pages the problem list ten at a time', async () => {
    const many = Array.from({ length: 23 }, (_, n) => inc({ id: `i${n}`, scope: `account:${100 + n}` }))
    vi.mocked(monitorAPI.summary).mockResolvedValue(summary(many)); vi.mocked(monitorAPI.live).mockResolvedValue(live())
    const view = mount(MonitorPanel); await flushPromises()
    const rows = () => view.findAll('button[aria-label^="查看："]').length
    expect(rows()).toBe(10); expect(view.text()).toContain('第 1–10 条，共 23 条')
    await view.findAll('button').find(b => b.text() === '下一页')!.trigger('click'); await view.findAll('button').find(b => b.text() === '下一页')!.trigger('click')
    expect(rows()).toBe(3); expect(view.text()).toContain('3 / 3')
    view.unmount()
  })
  it('says healthy only with care and refreshes every 10 seconds', async () => {
    vi.mocked(monitorAPI.summary).mockResolvedValue(summary([]))
    vi.mocked(monitorAPI.live).mockResolvedValue(live({ totals: { requests: 10, succeeded: 10, failed: 0, cancelled: 0, upstream_errors: 0 } }))
    const view = mount(MonitorPanel); await flushPromises()
    expect(view.get('[data-testid="headline"]').text()).toBe('近 1 小时没有新问题')
    expect(view.text()).toContain('这不等于所有请求都正常')
    expect(monitorAPI.summary).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(10000); await flushPromises()
    expect(monitorAPI.summary).toHaveBeenCalledTimes(2); expect(monitorAPI.live).toHaveBeenCalledTimes(2)
    view.unmount()
  })
  it('keeps the last data and says so when the monitor is unreachable', async () => {
    vi.mocked(monitorAPI.summary).mockRejectedValue({ response: { data: { message: '独立监控暂不可用' } } })
    vi.mocked(monitorAPI.live).mockRejectedValue(new Error('x'))
    const view = mount(MonitorPanel); await flushPromises()
    expect(view.get('[role="alert"]').text()).toContain('监控服务暂时连不上')
    expect(view.text()).toContain('连接中断，正在重试'); expect(view.text()).not.toContain('运行正常')
    view.unmount()
  })
  it('flags stale data sources instead of claiming full coverage', async () => {
    const s = summary([]); s.sources = [{ source_id: 'postgres.errors', last_success: iso(10 * 60000), quality: 'partial', dropped: 0 }]
    vi.mocked(monitorAPI.summary).mockResolvedValue(s); vi.mocked(monitorAPI.live).mockResolvedValue(live())
    const view = mount(MonitorPanel); await flushPromises()
    expect(view.text()).toContain('错误日志 超过 2 分钟没有更新')
    view.unmount()
  })
})

describe('incident drawer', () => {
  it('shows affected users by name, the 24h chart and requires a reason to silence', async () => {
    const detail = { incident: inc({ rollup: true, scope: 'account:31', members: [{ rule: 'U02', model: 'claude-opus-5-5', count: 7, last: iso(60000) }], explanation: { conclusion: '', confidence: 'medium', facts: ['U02 上游授权 · 模型 x · 本轮命中 7 次', '最近一条（U02）：上游尝试返回404；账号31'], inferences: [], missing_evidence: ['状态码不能单独证明封号'], advice: ['核对账号授权'] } }), history: [inc({})], children: [inc({ id: 'c1', parent_id: 'inc-1' })], impact: { users: [6, 9], accounts: [31] }, hits: [{ at: iso(60000), healthy: false }, { at: iso(120000), healthy: true }], names }
    vi.mocked(monitorAPI.detail).mockResolvedValue(detail)
    vi.mocked(monitorAPI.control).mockResolvedValue({ ...detail, incident: { ...detail.incident, silenced_until: iso(-3600000) } })
    const view = mount(MonitorIncidentDrawer, { props: { id: 'inc-1', rules, names, tick: 0 }, attachTo: document.body }); await flushPromises()
    const text = document.body.textContent || ''
    expect(text).toContain('上游账号「apipros」出现异常')
    expect(text).toContain('小明'); expect(text).toContain('xm@example.com'); expect(text).toContain('阿强')
    expect(text).toContain('上游授权、权限或接口异常（claude-opus-5-5）：本轮 7 次'); expect(text).toContain('最近一次：上游尝试返回404')
    expect(text).not.toContain('本轮命中 7 次')
    expect(text).toContain('近 24 小时'); expect(text).toContain('异常 1 次')
    const button = (t: string) => [...document.querySelectorAll('[role=dialog] button')].find(b => b.textContent?.trim() === t) as HTMLButtonElement
    button('静默 1 小时').click(); await flushPromises()
    button('确认静默').click(); await flushPromises()
    expect(monitorAPI.control).not.toHaveBeenCalled(); expect(document.body.textContent).toContain('请写一句静默原因')
    const input = document.querySelector('[role=dialog] form input') as HTMLInputElement
    input.value = '已联系供应商'; input.dispatchEvent(new Event('input'))
    button('确认静默').click(); await flushPromises()
    expect(monitorAPI.control).toHaveBeenCalledWith(detail.incident, 'silence', '已联系供应商', expect.any(String))
    expect(view.emitted('changed')).toBeTruthy()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(view.emitted('close')).toBeTruthy()
    view.unmount()
  })
})

it('saves ownership overrides and handoffs through the parent draft', async () => {
  const { config, transports } = summary()
  vi.mocked(monitorAPI.save).mockImplementation(async next => next)
  const view = mount(MonitorConfigForm, { props: { config, rules, transports } })
  const click = (text: string) => view.findAll('button').find(button => button.text() === text)!.trigger('click')
  await click('添加接管范围')
  await click('添加旧规则映射')
  await view.get('input[placeholder="至少指定一项范围"]').setValue('account:31')
  await view.get('input[min="1"]').setValue(42)
  await view.get('form').trigger('submit')
  await flushPromises()
  expect(monitorAPI.save).toHaveBeenLastCalledWith(expect.objectContaining({
    owner_overrides: [expect.objectContaining({ scope: 'account:31' })],
    legacy_handoffs: [expect.objectContaining({ legacy_rule_id: 42 })],
  }))
  await click('删除范围')
  await click('删除旧规则映射')
  await view.get('form').trigger('submit')
  await flushPromises()
  expect(monitorAPI.save).toHaveBeenLastCalledWith(expect.objectContaining({ owner_overrides: [], legacy_handoffs: [] }))
  expect(config.owner_overrides).toEqual([])
  expect(config.legacy_handoffs).toEqual([])
  view.unmount()
})
