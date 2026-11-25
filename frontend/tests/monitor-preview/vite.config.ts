import { defineConfig, type Plugin } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'
import * as fake from './mock'

// MONITOR_PREVIEW_MOCK=1 serves fake monitor API data so the panel can be viewed
// without a running monitor. Playwright smoke tests intercept requests themselves.
function mockMonitorAPI(): Plugin {
  const profile = { queue_warn_seconds: 10, queue_fail_seconds: 30, first_warn_seconds: 30, first_fail_seconds: 60, stall_warn_seconds: 30, stall_fail_seconds: 60, task_sla_seconds: 0 }
  let config = { version: 3, actor_id: 'admin:1', idempotency_key: 'k', shadow: false, site_id: 'preview', rules: {}, profiles: { default: profile }, balance_low_usd: null, subscription_low_ratio: null, feishu_targets: ['oc_ops1'], email_targets: [], detail_base_url: 'https://token.example.com', payment_grace_seconds: 300, billing_grace_seconds: 120, owner_overrides: [], legacy_handoffs: [] }
  let channels = { source: 'env', feishu: { domain: 'feishu', app_id: 'cli_a1b2c3d4e5f6' }, feishu_secret_set: true, email: { host: '', port: 0, security: 'ssl', username: '', from: '' }, email_password_set: false, updated_at: '0001-01-01T00:00:00Z' }
  return {
    name: 'mock-monitor-api',
    configureServer(server) {
      server.middlewares.use('/api/v1/admin/ops/monitor', (req, res) => {
        let body = ''
        req.on('data', c => { body += c })
        req.on('end', () => {
          const path = (req.url || '').split('?')[0]
          const ok = (data: unknown) => { res.setHeader('Content-Type', 'application/json'); res.end(JSON.stringify({ code: 0, message: 'success', data })) }
          const fail = (message: string) => { res.statusCode = 400; res.setHeader('Content-Type', 'application/json'); res.end(JSON.stringify({ code: 400, message })) }
          const transports = { feishu: channels.feishu_secret_set, email: !!channels.email.host }
          if (path === '/summary') return ok({ build: 'preview', config, incidents: fake.incidents, deliveries: fake.deliveries, sources: fake.sources(), capabilities: [], rules: [{ id: 'U01', title: '上游429或5xx异常', advice: '检查供应商状态', requires: [], recovery_samples: 5, recovery_seconds: 300 }, { id: 'U02', title: '上游授权、权限或接口异常', advice: '核对账号授权', requires: [], recovery_samples: 5, recovery_seconds: 300 }, { id: 'F01', title: '请求发生流内或应用语义失败', advice: '检查流内错误', requires: [], recovery_samples: 5, recovery_seconds: 300 }, { id: 'R01', title: '请求重试或换号频繁', advice: '核对各次尝试原因', requires: [], recovery_samples: 5, recovery_seconds: 300 }, { id: 'S01', title: '分组与模型无可用候选账号', advice: '检查候选过滤原因', requires: [], recovery_samples: 1, recovery_seconds: 0 }, { id: 'L03', title: '多个请求持续等待有效输出', advice: '对照首字分布', requires: [], recovery_samples: 1, recovery_seconds: 0 }, { id: 'Q01', title: '用户资金来源额度耗尽', advice: '核对钱包', requires: [], recovery_samples: 1, recovery_seconds: 0 }, { id: 'M01', title: '监控证据来源缺失或不兼容', advice: '先恢复采集', requires: [], recovery_samples: 1, recovery_seconds: 0 }], coverage_gaps: ['钱包独立金额流水尚未提供，不能证明所有钱包扣费金额均已对账。', 'WS/二进制逐帧进度尚不支持。'], transports, checked_at: new Date().toISOString(), list_limit: 1000, names: fake.names })
          if (path === '/live') return ok(fake.live())
          if (path.startsWith('/incidents/') && path.endsWith('/control')) return ok(fake.detail(path.split('/')[2]))
          if (path.startsWith('/incidents/')) return ok(fake.detail(path.split('/')[2]))
          if (path === '/config' && req.method === 'PUT') { config = { ...JSON.parse(body), version: config.version + 1 }; return ok(config) }
          if (path === '/channels' && req.method === 'GET') return ok(channels)
          if (path === '/channels' && req.method === 'PUT') {
            const input = JSON.parse(body)
            if (input.feishu.app_id && !input.feishu.app_id.startsWith('cli_')) return fail('App ID 格式不正确，应以 cli_ 开头')
            channels = { source: 'page', feishu: { domain: input.feishu.domain, app_id: input.feishu.app_id }, feishu_secret_set: !!input.feishu.app_id, email: { ...input.email, password: undefined }, email_password_set: !!input.email.host, updated_by: 'admin:1', updated_at: new Date().toISOString() } as typeof channels
            return ok(channels)
          }
          if (path === '/channels/feishu/chats') return ok({ bot_name: '心链监控', chats: [{ chat_id: 'oc_ops1', name: '运维告警群' }, { chat_id: 'oc_ops2', name: '值班群' }] })
          if (path === '/channels/test') { const input = JSON.parse(body); return input.channel === 'email' && !channels.email.host ? fail('请先填写并保存 SMTP 设置') : ok({ sent: true }) }
          res.statusCode = 404; res.end('{}')
        })
      })
    },
  }
}

export default defineConfig({ plugins: [vue(), ...(process.env.MONITOR_PREVIEW_MOCK === '1' ? [mockMonitorAPI()] : [])], resolve: { alias: { '@': resolve(__dirname, '../../src'), 'vue-i18n': 'vue-i18n/dist/vue-i18n.runtime.esm-bundler.js' } }, server: { host: '127.0.0.1', port: 5191, strictPort: true } })
