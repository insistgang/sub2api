import { describe, expect, it } from 'vitest'

import {
  getPaymentVisibleMethodSourceOptions,
  normalizePaymentVisibleMethodSource,
} from '@/api/admin/settings'

describe('admin settings payment visible method helpers', () => {
  it('normalizes aliases into canonical source keys per visible method', () => {
    expect(normalizePaymentVisibleMethodSource('alipay', 'official')).toBe('official_alipay')
    expect(normalizePaymentVisibleMethodSource('alipay', 'alipay_direct')).toBe('official_alipay')
    expect(normalizePaymentVisibleMethodSource('alipay', 'easypay')).toBe('easypay_alipay')

    expect(normalizePaymentVisibleMethodSource('wxpay', 'official')).toBe('official_wxpay')
    expect(normalizePaymentVisibleMethodSource('wxpay', 'wechat')).toBe('official_wxpay')
    expect(normalizePaymentVisibleMethodSource('wxpay', 'easypay')).toBe('easypay_wxpay')
    expect(normalizePaymentVisibleMethodSource('alipay', 'manual_alipay')).toBe('manual_alipay')
    expect(normalizePaymentVisibleMethodSource('wxpay', 'manual_wxpay')).toBe('manual_wxpay')
  })

  it('rejects unknown or cross-method source values', () => {
    expect(normalizePaymentVisibleMethodSource('alipay', 'official_wxpay')).toBe('')
    expect(normalizePaymentVisibleMethodSource('wxpay', 'official_alipay')).toBe('')
    expect(normalizePaymentVisibleMethodSource('alipay', 'unknown')).toBe('')
    expect(normalizePaymentVisibleMethodSource('wxpay', null)).toBe('')
  })

  it('exposes method-scoped source options instead of arbitrary strings', () => {
    expect(getPaymentVisibleMethodSourceOptions('alipay')).toEqual([
      {
        value: '',
        labelZh: '未配置',
        labelEn: 'Not configured',
      },
      {
        value: 'official_alipay',
        labelZh: '支付宝官方',
        labelEn: 'Official Alipay',
      },
      {
        value: 'easypay_alipay',
        labelZh: '易支付支付宝',
        labelEn: 'EasyPay Alipay',
      },
      {
        value: 'manual_alipay',
        labelZh: '个人支付宝收款码',
        labelEn: 'Personal Alipay QR',
      },
    ])

    expect(getPaymentVisibleMethodSourceOptions('wxpay')).toEqual([
      {
        value: '',
        labelZh: '未配置',
        labelEn: 'Not configured',
      },
      {
        value: 'official_wxpay',
        labelZh: '微信官方',
        labelEn: 'Official WeChat Pay',
      },
      {
        value: 'easypay_wxpay',
        labelZh: '易支付微信',
        labelEn: 'EasyPay WeChat Pay',
      },
      {
        value: 'manual_wxpay',
        labelZh: '个人微信收款码',
        labelEn: 'Personal WeChat QR',
      },
    ])

    expect(getPaymentVisibleMethodSourceOptions('alipay').map(option => option.value)).toContain('manual_alipay')
  })

  it('offers manual QR sources only for their matching visible method', () => {
    expect(normalizePaymentVisibleMethodSource('alipay', 'manual_alipay')).toBe('manual_alipay')
    expect(normalizePaymentVisibleMethodSource('wxpay', 'manual_wxpay')).toBe('manual_wxpay')
    expect(normalizePaymentVisibleMethodSource('alipay', 'manual_wxpay')).toBe('')
    expect(normalizePaymentVisibleMethodSource('wxpay', 'manual_alipay')).toBe('')
    expect(getPaymentVisibleMethodSourceOptions('alipay').map(option => option.value)).toContain('manual_alipay')
    expect(getPaymentVisibleMethodSourceOptions('wxpay').map(option => option.value)).toContain('manual_wxpay')
  })
})
