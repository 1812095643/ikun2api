import { describe, expect, it } from 'vitest'
import { getOutputRate } from '../outputRate'

describe('输出速率口径', () => {
  it('扣除首字等待后按实际生成时长计算，支持切换总耗时', () => {
    const row = { output_tokens: 1000, duration_ms: 12000, first_token_ms: 2000 }
    expect(getOutputRate(row, 'generation')).toBe(100)
    expect(getOutputRate(row, 'total')).toBeCloseTo(83.333333)
  })

  it.each([null, undefined])('缺少首字记录 %s 时回退总耗时', firstToken => {
    const row = { output_tokens: 500, duration_ms: 10000, first_token_ms: firstToken }
    expect(getOutputRate(row, 'generation')).toBe(50)
    expect(getOutputRate(row, 'total')).toBe(50)
  })

  it('首字耗时等于总耗时时回退总耗时', () => {
    const row = { output_tokens: 500, duration_ms: 10000, first_token_ms: 10000 }
    expect(getOutputRate(row, 'generation')).toBe(50)
    expect(getOutputRate(row, 'total')).toBe(50)
  })

  it.each([0, -1, null, undefined, NaN, Infinity])('总耗时 %s 不产生无穷或负数速率', duration => {
    expect(getOutputRate({ output_tokens: 100, duration_ms: duration, first_token_ms: 0 }, 'generation')).toBeNull()
  })

  it.each([2000, -1, NaN, Infinity])('首字耗时 %s 无法形成正生成时长时不计算', firstToken => {
    expect(getOutputRate({ output_tokens: 100, duration_ms: 1000, first_token_ms: firstToken }, 'generation')).toBeNull()
  })

  it('零输出和零首字等待可以正常计算', () => {
    expect(getOutputRate({ output_tokens: 0, duration_ms: 1000, first_token_ms: 0 }, 'generation')).toBe(0)
  })

})
