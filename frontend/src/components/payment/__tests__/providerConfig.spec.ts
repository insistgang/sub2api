import { describe, expect, it } from 'vitest'
import {
  PAYMENT_CURRENCY_OPTIONS,
  PROVIDER_SUPPORTED_TYPES,
  PROVIDER_CONFIG_FIELDS,
  isValidManualQrImageValue,
  isBuiltInAlipayMethod,
  isBuiltInWxpayMethod,
  parseEasyPayCustomMethods,
  serializeEasyPayCustomMethods,
} from '@/components/payment/providerConfig'

function findField(providerKey: string, key: string) {
  const fields = PROVIDER_CONFIG_FIELDS[providerKey] || []
  return fields.find(field => field.key === key)
}

describe('PROVIDER_CONFIG_FIELDS.wxpay', () => {
  it('keeps admin form validation aligned with backend-required credentials', () => {
    expect(findField('wxpay', 'publicKeyId')?.optional).toBeFalsy()
    expect(findField('wxpay', 'certSerial')?.optional).toBeFalsy()
  })

  it('only keeps the simplified visible credential set in the admin form', () => {
    expect(findField('wxpay', 'mpAppId')).toBeUndefined()
    expect(findField('wxpay', 'h5AppName')).toBeUndefined()
    expect(findField('wxpay', 'h5AppUrl')).toBeUndefined()
  })
})

describe('PROVIDER_CONFIG_FIELDS.airwallex', () => {
  it('adds currency config with CNY as the default', () => {
    const currency = findField('airwallex', 'currency')

    expect(currency?.defaultValue).toBe('CNY')
    expect(currency?.hintKey).toBe('admin.settings.payment.field_paymentCurrencyHint')
    expect(currency?.options).toBe(PAYMENT_CURRENCY_OPTIONS)
  })

  it('marks accountId as optional and explains when it can be left blank', () => {
    const accountId = findField('airwallex', 'accountId')

    expect(accountId?.optional).toBe(true)
    expect(accountId?.clearable).toBe(true)
    expect(accountId?.hintKey).toBe('admin.settings.payment.field_accountIdHint')
  })

  it('explains that apiBase must match the Airwallex key environment', () => {
    expect(findField('airwallex', 'apiBase')?.hintKey).toBe('admin.settings.payment.field_airwallexApiBaseHint')
  })
})

describe('PROVIDER_CONFIG_FIELDS.stripe', () => {
  it('adds currency config with CNY as the default', () => {
    const currency = findField('stripe', 'currency')

    expect(currency?.defaultValue).toBe('CNY')
    expect(currency?.hintKey).toBe('admin.settings.payment.field_paymentCurrencyHint')
    expect(currency?.options).toBe(PAYMENT_CURRENCY_OPTIONS)
  })
})

describe('manual QR provider configuration', () => {
  it('supports Alipay and WeChat with the exact manual configuration fields', () => {
    expect(PROVIDER_SUPPORTED_TYPES.manual).toEqual(['alipay', 'wxpay'])
    expect(PROVIDER_CONFIG_FIELDS.manual?.map(field => field.key)).toEqual([
      'alipayQRCode', 'wxpayQRCode', 'payeeName', 'instructions', 'currency',
    ])
    expect(findField('manual', 'currency')?.defaultValue).toBe('CNY')
    expect(findField('manual', 'alipayQRCode')?.type).toBe('qr_image')
    expect(findField('manual', 'wxpayQRCode')?.type).toBe('qr_image')
  })

  it('accepts only HTTPS URLs or PNG/JPEG data URLs within 300 KiB', () => {
    expect(isValidManualQrImageValue('https://pay.example.invalid/alipay.png')).toBe(true)
    expect(isValidManualQrImageValue('data:image/png;base64,aGVsbG8=')).toBe(true)
    expect(isValidManualQrImageValue('data:image/jpeg;base64,aGVsbG8=')).toBe(true)
    expect(isValidManualQrImageValue('http://pay.example.invalid/alipay.png')).toBe(false)
    expect(isValidManualQrImageValue('data:image/svg+xml;base64,PHN2Zz4=')).toBe(false)
    expect(isValidManualQrImageValue('javascript:alert(1)')).toBe(false)
    const tooLarge = 'data:image/png;base64,' + 'A'.repeat(Math.ceil((300 * 1024 + 1) / 3) * 4)
    expect(isValidManualQrImageValue(tooLarge)).toBe(false)
  })
})

describe('manual QR provider configuration', () => {
  it('supports Alipay and WeChat top-ups with the frozen manual provider keys', () => {
    expect(PROVIDER_SUPPORTED_TYPES.manual).toEqual(['alipay', 'wxpay'])
    expect(PROVIDER_CONFIG_FIELDS.manual?.map(field => field.key)).toEqual([
      'alipayQRCode', 'wxpayQRCode', 'payeeName', 'instructions', 'currency',
    ])
    expect(findField('manual', 'currency')?.defaultValue).toBe('CNY')
    expect(findField('manual', 'alipayQRCode')?.optional).toBe(true)
    expect(findField('manual', 'wxpayQRCode')?.optional).toBe(true)
    expect(findField('manual', 'alipayQRCode')?.type).toBe('qr_image')
    expect(findField('manual', 'wxpayQRCode')?.type).toBe('qr_image')
  })

  it('allows HTTPS image URLs or bounded PNG/JPEG data URLs only', () => {
    expect(isValidManualQrImageValue('https://pay.example.invalid/alipay.png')).toBe(true)
    expect(isValidManualQrImageValue('data:image/png;base64,aGVsbG8=')).toBe(true)
    expect(isValidManualQrImageValue('data:image/jpeg;base64,aGVsbG8=')).toBe(true)
    expect(isValidManualQrImageValue('http://pay.example.invalid/alipay.png')).toBe(false)
    expect(isValidManualQrImageValue('data:image/svg+xml;base64,PHN2Zz4=')).toBe(false)
    expect(isValidManualQrImageValue('javascript:alert(1)')).toBe(false)
    const tooLarge = 'data:image/png;base64,' + 'A'.repeat(Math.ceil((300 * 1024 + 1) / 3) * 4)
    expect(isValidManualQrImageValue(tooLarge)).toBe(false)
  })
})

describe('EasyPay custom methods config', () => {
  it('parses customMethods from the JSON string stored in provider config', () => {
    expect(parseEasyPayCustomMethods(
      '[{"type":"ldc","upstreamType":"epay","displayName":"LDC"},{"type":"usdt_trc20","upstreamType":"usdt","displayName":"USDT-TRC20"}]',
    )).toEqual([
      { type: 'ldc', upstreamType: 'epay', displayName: 'LDC' },
      { type: 'usdt_trc20', upstreamType: 'usdt', displayName: 'USDT-TRC20' },
    ])
  })

  it('serializes non-empty custom methods into the config string format', () => {
    expect(serializeEasyPayCustomMethods([
      { type: 'ldc', upstreamType: 'epay', displayName: 'LDC' },
      { type: '  ', upstreamType: 'ignored', displayName: 'Ignored' },
      { type: 'usdt_trc20', upstreamType: 'usdt', displayName: '' },
    ])).toBe('[{"type":"ldc","upstreamType":"epay","displayName":"LDC"},{"type":"usdt_trc20","upstreamType":"usdt","displayName":""}]')
  })

  it('returns an empty string for invalid or empty custom methods', () => {
    expect(parseEasyPayCustomMethods('not-json')).toEqual([])
    expect(serializeEasyPayCustomMethods([{ type: '', upstreamType: 'epay', displayName: 'LDC' }])).toBe('')
  })
})

describe('built-in payment method helpers', () => {
  it('only treats exact built-in aliases as Alipay or WeChat Pay', () => {
    expect(isBuiltInAlipayMethod('alipay')).toBe(true)
    expect(isBuiltInAlipayMethod('alipay_direct')).toBe(true)
    expect(isBuiltInAlipayMethod('card_alipay')).toBe(false)

    expect(isBuiltInWxpayMethod('wxpay')).toBe(true)
    expect(isBuiltInWxpayMethod('wxpay_direct')).toBe(true)
    expect(isBuiltInWxpayMethod('card_wxpay')).toBe(false)
  })
})
