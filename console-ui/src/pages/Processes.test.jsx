import { describe, expect, it } from 'vitest'
import { fmtBytes, fmtRate } from './Processes'

describe('process resource formatting', () => {
  it('distinguishes a measured zero rate from unavailable data', () => {
    expect(fmtBytes(0)).toBe('0 B')
    expect(fmtRate(0, 'available')).toBe('0 B/s')
    expect(fmtRate(null, 'available')).toBe('采样中')
    expect(fmtRate(0, 'unavailable')).toBe('无数据')
    expect(fmtRate(0, 'unsupported')).toBe('不支持')
  })
})
