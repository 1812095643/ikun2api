import { describe, expect, it } from 'vitest'
import { defaultBalanceDisplay, type UpstreamBalance } from '@/api/upstreamBalances'
import { mergeUpstreamOrder, orderUpstreamBalances } from '../upstreamBalanceList'
const row = (id: string, balance: number | null, unit = 'USD') => ({ id, name: id, url: `https://${id}.example`, snapshot: { balance, unit } }) as UpstreamBalance

describe('上游列表排序', () => {
  it('只替换拖动的可见子集，保留隐藏项和其他分页的位置', () => {
    expect(mergeUpstreamOrder(['a', 'hidden', 'b', 'c', 'page2'], ['c', 'a', 'b'])).toEqual(['c', 'hidden', 'a', 'b', 'page2'])
    expect(mergeUpstreamOrder(['a', 'b', 'c'], ['c', 'c', 'foreign', 'a'])).toEqual(['c', 'b', 'a'])
  })
  it('保留手动排序，新发现的上游稳定排在后面且不修改输入', () => {
    const items = [row('a', 0), row('c', 3), row('b', 2)]
    expect(orderUpstreamBalances(items, { ...defaultBalanceDisplay(), order: ['b', 'a'] }).map(v => v.id)).toEqual(['b', 'a', 'c'])
    expect(items.map(v => v.id)).toEqual(['a', 'c', 'b'])
  })
  it('余额排序保留负数和零，把未知值放最后，同币种比较', () => {
    const items = [row('unknown', null), row('positive', 20), row('zero', 0), row('negative', -2)]
    expect(orderUpstreamBalances(items, { ...defaultBalanceDisplay(), sort_by: 'balance_asc' }).map(v => v.id)).toEqual(['negative', 'zero', 'positive', 'unknown'])
    expect(orderUpstreamBalances(items, { ...defaultBalanceDisplay(), sort_by: 'balance_desc' }).map(v => v.id)).toEqual(['positive', 'zero', 'negative', 'unknown'])
  })
})
