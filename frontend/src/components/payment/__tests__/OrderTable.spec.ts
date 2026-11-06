import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import OrderTable from '../OrderTable.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) =>
      params?.amount ? `${key} ${params.amount}` : key,
  }),
}))

const order = {
  id: 1, user_id: 1, amount: 100, bonus_amount: 20, pay_amount: 80,
  currency: 'USD', fee_rate: 0, payment_type: 'stripe', out_trade_no: 'order-1',
  status: 'COMPLETED', order_type: 'balance', refund_amount: 0,
  created_at: '2026-11-06T00:00:00Z', expires_at: '2026-11-06T00:30:00Z',
}

function render(showUser = false, overrides: Partial<typeof order> = {}) {
  return mount(OrderTable, {
    props: { orders: [{ ...order, ...overrides }], loading: false, showUser },
    global: { stubs: {
      DataTable: { props: ['data'], template: '<div><slot name="cell-pay_amount" :value="data[0].pay_amount" :row="data[0]" /></div>' },
      OrderStatusBadge: true,
    } },
  }).text()
}

describe('user order credited currency', () => {
  it.each([
    ['USD', 'balance', true],
    ['HKD', 'balance', true],
    ['JPY', 'balance', true],
    ['CNY', 'balance', false],
    ['RMB', 'balance', false],
    ['', 'balance', false],
    ['USD', 'subscription', false],
  ])('shows equal numeric credited balance only when currency differs: %s/%s', (currency, order_type, visible) => {
    const text = render(false, { amount: 10, pay_amount: 10, bonus_amount: 0, currency, order_type })
    expect(text.includes('payment.orders.creditedAmount')).toBe(visible)
    if (visible) expect(text).toContain('¥10.00')
  })
  it('shows credited and bonus balances in CNY while preserving the actual USD payment', () => {
    const text = render()
    expect(text).toContain('$80.00')
    expect(text).toContain('¥100.00')
    expect(text).toContain('¥20.00')
  })

  it('also shows administrator credited balances in CNY without changing the payment', () => {
    const text = render(true)
    expect(text).toContain('$80.00')
    expect(text).toContain('¥100.00')
    expect(text).toContain('¥20.00')
  })
})
