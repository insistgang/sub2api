import { describe, expect, it } from 'vitest'
import { cnyToUsd, usdToCny } from '../displayCurrency'

describe('display currency conversion', () => {
  it('uses the configured one-to-one display rate in both directions', () => {
    expect(usdToCny(12.345678)).toBe(12.345678)
    expect(cnyToUsd(12.345678)).toBe(12.345678)
    expect(cnyToUsd(usdToCny(0))).toBe(0)
  })
})
