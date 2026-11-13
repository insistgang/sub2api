import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import UserOrdersView from '../UserOrdersView.vue'
import Select from '@/components/common/Select.vue'
import Pagination from '@/components/common/Pagination.vue'

const api = vi.hoisted(() => ({ getMyOrders: vi.fn(), getRefundEligibleProviders: vi.fn() }))
const router = vi.hoisted(() => ({ push: vi.fn() }))
vi.mock('@/api/payment', () => ({ paymentAPI: api }))
vi.mock('@/stores', () => ({ useAppStore: () => ({ showError: vi.fn() }) }))
vi.mock('vue-router', () => ({ useRouter: () => router }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key })
}))
enableAutoUnmount(afterEach)

beforeEach(() => {
  vi.clearAllMocks()
  api.getMyOrders.mockResolvedValue({ data: { items: [], total: 100 } })
  api.getRefundEligibleProviders.mockResolvedValue({ data: { provider_instance_ids: [] } })
})

async function openOrders() {
  const wrapper = mount(UserOrdersView, {
    global: { stubs: {
      AppLayout: { template: '<div><slot /></div>' },
      OrderTable: true, BaseDialog: true, Icon: true, Pagination: true, teleport: true
    } }
  })
  await flushPromises()
  wrapper.getComponent(Pagination).vm.$emit('update:page', 4)
  await flushPromises()
  return wrapper
}

describe('order status filtering', () => {
  it('loads the first page with the selected status', async () => {
    const wrapper = await openOrders()
    const select = wrapper.getComponent(Select)
    await select.get('button').trigger('click')
    await select.findAll('[role="option"]').find(option => option.text() === 'payment.status.pending')!.trigger('click')
    await flushPromises()
    expect(api.getMyOrders).toHaveBeenLastCalledWith({ page: 1, page_size: 20, status: 'PENDING' })
    expect(wrapper.getComponent(Pagination).props('page')).toBe(1)
    expect(api.getMyOrders).toHaveBeenCalledTimes(3)
  })

  it('keeps the current page on manual refresh', async () => {
    const wrapper = await openOrders()
    await wrapper.get('[title="common.refresh"]').trigger('click')
    await flushPromises()
    expect(api.getMyOrders).toHaveBeenLastCalledWith({ page: 4, page_size: 20, status: undefined })
  })

  it('includes manual review and rejection states in order filters', async () => {
    const wrapper = await openOrders()
    const select = wrapper.getComponent(Select)
    await select.get('button').trigger('click')
    const options = select.findAll('[role="option"]').map(option => option.text())
    expect(options).toContain('payment.status.pending_review')
    expect(options).toContain('payment.status.rejected')
  })
})

describe('manual order history navigation', () => {
  it('provides a link to review a manual order in its personal QR page', async () => {
    api.getMyOrders.mockResolvedValue({
      data: {
        items: [{
          id: 101, user_id: 10, amount: 2, pay_amount: 14.3, fee_rate: 0,
          payment_type: 'wxpay', out_trade_no: 'manual-101', status: 'PENDING_REVIEW',
          order_type: 'balance', created_at: '2030-01-01T10:00:00Z',
          expires_at: '2030-01-01T10:30:00Z', refund_amount: 0, is_manual: true,
        }],
        total: 1,
      },
    })
    const OrderTableStub = defineComponent({
      props: ['orders'],
      setup(props, { slots }) {
        return () => h('div', (props.orders as Array<{ id: number }>).map(row =>
          h('div', { key: row.id }, slots.actions?.({ row })),
        ))
      },
    })
    router.push.mockReset()

    const wrapper = mount(UserOrdersView, {
      global: { stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        OrderTable: OrderTableStub,
        BaseDialog: true, Icon: true, Pagination: true, teleport: true,
      } },
    })
    await flushPromises()
    await wrapper.get('[data-testid="manual-order-view"]').trigger('click')

    expect(router.push).toHaveBeenCalledWith('/payment/manual/101')
  })
})
