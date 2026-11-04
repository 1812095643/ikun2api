import { describe, it, expect } from 'vitest'
import { applyMenuLabels, resolveMenuLabel } from './menuPages'
import { resolveRouteDocumentTitle } from '@/router/title'
describe('menu names', () => {
 it('uses stable identities and recursively renames parents and children', () => {
  const input = [{ path: '/admin/channels', label: '渠道管理', children: [{ path: '/admin/channels/pricing', label: '渠道定价' }] }]
  const result = applyMenuLabels(input, { '/admin/channels': '供应渠道', '/admin/channels/pricing': '模型价格' })
  expect(result[0].label).toBe('供应渠道'); expect(result[0].children?.[0].label).toBe('模型价格')
  expect(input[0].label).toBe('渠道管理')
 })
 it('falls back to the original localized name after clearing', () => {
  expect(resolveMenuLabel('/news/?a=1', '时事新闻', { '/news': '公告资讯' })).toBe('公告资讯')
  expect(resolveMenuLabel('/news', 'News', { '/news': ' ' })).toBe('News')
 })
 it('uses the same override for document titles', () => {
  expect(resolveRouteDocumentTitle({ path: '/admin/account-vault', name: 'AdminAccountVault', params: {}, meta: { title: '账号资料库' } }, 'IKUN', [], { menuLabels: { '/admin/account-vault': '账号档案' } })).toBe('账号档案 - IKUN')
 })
})
