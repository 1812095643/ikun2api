import { describe, expect, it } from 'vitest'
import { createI18n } from 'vue-i18n'
import zh from '@/i18n/locales/zh/admin/audit'
import en from '@/i18n/locales/en/admin/audit'
import { auditActionLabel } from '../auditAction'

describe('审计动作翻译', () => {
  const i18n = createI18n({ legacy: false, locale: 'zh', messages: { zh: { admin: zh }, en: { admin: en } } })
  it('把截图中的自动推导动作翻译为实际操作', () => {
    expect(auditActionLabel('workspace.navigation.read.create', i18n.global.t)).toBe('标记导航消息已读')
    expect(auditActionLabel('usage.dashboard.api_keys_usage.create', i18n.global.t)).toBe('查看 API Key 用量')
    expect(auditActionLabel('admin.users.update', i18n.global.t)).toBe('更新用户')
    expect(auditActionLabel('auth.login.2fa', i18n.global.t)).toBe('两步验证登录')
    expect(auditActionLabel('admin.accounts.credentials.read', i18n.global.t)).toBe('查看账号凭据')
    expect(auditActionLabel('payment.orders.create', i18n.global.t)).toBe('创建支付订单')
    expect(auditActionLabel('admin.workspace.downstreams.apply.create', i18n.global.t)).toBe('更新下游设置')
  })
  it('切换语言后重新翻译，未知动作不裸露技术键', () => {
    i18n.global.locale.value = 'en'
    expect(auditActionLabel('admin.accounts.import', i18n.global.t)).toBe('Import account')
    expect(auditActionLabel('workspace.navigation.read.create', i18n.global.t)).toBe('Mark navigation message as read')
    expect(auditActionLabel('future.unknown.create', i18n.global.t)).toBe('Other operation')
  })
  it('正确区分定时计划预览、保存、取消和连接诊断', () => {
    i18n.global.locale.value = 'zh'
    expect(auditActionLabel('admin.accounts.schedulable_timers.preview.create', i18n.global.t)).toBe('预览定时启停计划')
    expect(auditActionLabel('accounts.schedulable_timers.create', i18n.global.t)).toBe('保存定时启停计划')
    expect(auditActionLabel('accounts.schedulable_timers.open.delete', i18n.global.t)).toBe('取消定时启停计划')
    expect(auditActionLabel('admin.ops.connections.read', i18n.global.t)).toBe('查看连接诊断')
  })

})
