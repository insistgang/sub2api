import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import AdminOrdersView from '../AdminOrdersView.vue'

const api = vi.hoisted(() => ({
  getOrders: vi.fn(), getOrder: vi.fn(), getManualOrder: vi.fn(),
  approveManualOrder: vi.fn(), rejectManualOrder: vi.fn(),
  cancelOrder: vi.fn(), retryRecharge: vi.fn(), refundOrder: vi.fn(), queryRefund: vi.fn(),
}))
const showError = vi.hoisted(() => vi.fn())
const showSuccess = vi.hoisted(() => vi.fn())

vi.mock('@/api/admin/payment', () => ({ adminPaymentAPI: api, default: api }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess }) }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))

const manualOrder = () => ({
  id: 101, user_id: 10, amount: 2, pay_amount: 14.3, currency: 'CNY', fee_rate: 0,
  bonus_amount: 0, payment_type: 'wxpay', out_trade_no: 'manual-101',
  status: 'PENDING_REVIEW', order_type: 'balance', created_at: '2030-01-01T10:00:00Z',
  expires_at: '2030-01-01T10:30:00Z', refund_amount: 0, is_manual: true,
})

function manualDetails(order = manualOrder()) {
  return {
    order,
    qr_code_img: 'data:image/png;base64,aGVsbG8=',
    payee_name: 'Owner',
    instructions: 'Scan to pay',
    submission: { transaction_no: 'buyer-ref', note: 'I paid', submitted_at: '2030-01-01T10:05:00Z' },
    review_note: '',
    reviewed_at: '',
  }
}

const OrderTableStub = defineComponent({
  props: ['orders'],
  setup(props, { slots }) {
    return () => h('div', (props.orders as Array<{ id: number }>).map(row =>
      h('div', { key: row.id }, slots.actions?.({ row })),
    ))
  },
})

const SelectStub = defineComponent({
  props: ['options'],
  template: '<div data-testid="select-options"><span v-for="option in options" :key="option.value" :data-value="option.value">{{ option.label }}</span></div>',
})

async function mountOrders() {
  api.getOrders.mockReset().mockResolvedValue({ data: { items: [manualOrder()], total: 1 } })
  api.getOrder.mockReset().mockResolvedValue({ data: { order: manualOrder(), auditLogs: [] } })
  api.getManualOrder.mockReset().mockResolvedValue({ data: manualDetails() })
  api.approveManualOrder.mockReset().mockResolvedValue({ data: manualDetails({ ...manualOrder(), status: 'COMPLETED' }) })
  api.rejectManualOrder.mockReset().mockResolvedValue({ data: manualDetails({ ...manualOrder(), status: 'REJECTED' }) })
  api.cancelOrder.mockReset()
  api.retryRecharge.mockReset()
  api.refundOrder.mockReset()
  api.queryRefund.mockReset()
  showError.mockReset()
  showSuccess.mockReset()
  const wrapper = mount(AdminOrdersView, {
    global: { stubs: {
      AppLayout: { template: '<div><slot /></div>' },
      BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' },
      OrderTable: OrderTableStub,
      Select: SelectStub,
      Icon: true,
      Pagination: true,
      AdminRefundDialog: true,
      OrderStatusBadge: { template: '<span><slot /></span>' },
    } },
  })
  await flushPromises()
  return wrapper
}

describe('AdminOrdersView manual payment review', () => {
  beforeEach(() => vi.clearAllMocks())

  it('filters pending-review and rejected manual orders', async () => {
    const wrapper = await mountOrders()

    expect(wrapper.find('[data-value="PENDING_REVIEW"]').exists()).toBe(true)
    expect(wrapper.find('[data-value="REJECTED"]').exists()).toBe(true)
  })

  it('requires transaction number, exact CNY receipt, and explicit confirmation before approval', async () => {
    const wrapper = await mountOrders()
    await wrapper.get('[data-testid="admin-order-detail"]').trigger('click')
    await flushPromises()

    expect(api.getManualOrder).toHaveBeenCalledWith(101)
    expect(wrapper.text()).toContain('¥14.30')
    expect(wrapper.text()).toContain('¥2.00 CNY')
    expect(wrapper.get('[data-testid="manual-admin-qr-image"]').attributes('src')).toBe('data:image/png;base64,aGVsbG8=')
    expect(wrapper.get('[data-testid="manual-approve"]').attributes('disabled')).toBeDefined()

    await wrapper.get('[data-testid="manual-transaction-no"]').setValue('verified-ref')
    await wrapper.get('[data-testid="manual-received-amount"]').setValue('13.30')
    await wrapper.get('[data-testid="manual-confirm-received"]').setValue(true)
    expect(wrapper.get('[data-testid="manual-approve"]').attributes('disabled')).toBeDefined()

    await wrapper.get('[data-testid="manual-received-amount"]').setValue('14.301')
    expect(wrapper.get('[data-testid="manual-approve"]').attributes('disabled')).toBeDefined()

    await wrapper.get('[data-testid="manual-received-amount"]').setValue('14.30')
    expect(wrapper.get('[data-testid="manual-approve"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('[data-testid="manual-approve"]').trigger('click')
    await flushPromises()

    expect(api.approveManualOrder).toHaveBeenCalledWith(101, {
      transaction_no: 'verified-ref',
      received_amount: 14.3,
      confirm_received: true,
      note: '',
    })
    expect(showSuccess).toHaveBeenCalled()
    expect(api.refundOrder).not.toHaveBeenCalled()
  })

  it('shows the audit trail for manual orders', async () => {
    const wrapper = await mountOrders()
    api.getOrder.mockResolvedValue({ data: { order: manualOrder(), auditLogs: [
      { id: 1, action: 'MANUAL_SUBMITTED', operator: 'user:10', detail: 'buyer-ref', created_at: '2030-01-01T10:05:00Z' },
    ] } })
    await wrapper.get('[data-testid="admin-order-detail"]').trigger('click')
    await flushPromises()

    expect(api.getOrder).toHaveBeenCalledWith(101)
    expect(wrapper.text()).toContain('MANUAL_SUBMITTED')
    expect(wrapper.text()).toContain('user:10')
  })

  it('requires a reason to reject and sends the manual reject request', async () => {
    const wrapper = await mountOrders()
    await wrapper.get('[data-testid="admin-order-detail"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="manual-reject"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="manual-reject-reason"]').setValue('No matching transfer')
    await wrapper.get('[data-testid="manual-reject"]').trigger('click')
    await flushPromises()

    expect(api.rejectManualOrder).toHaveBeenCalledWith(101, { reason: 'No matching transfer' })
  })
})
