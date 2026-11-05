import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import ManualPaymentView from '../ManualPaymentView.vue'
import type { ManualOrderDetails } from '@/types/payment'

const api = vi.hoisted(() => ({ getManualOrder: vi.fn(), submitManualOrder: vi.fn() }))
const push = vi.hoisted(() => vi.fn())
const showError = vi.hoisted(() => vi.fn())
const showSuccess = vi.hoisted(() => vi.fn())
const refreshUser = vi.hoisted(() => vi.fn())
const params = vi.hoisted(() => ({ id: '101' }))

vi.mock('@/api/payment', () => ({ paymentAPI: api }))
vi.mock('vue-router', () => ({
  useRoute: () => ({ params }),
  useRouter: () => ({ push }),
}))
vi.mock('@/stores', () => ({
  useAppStore: () => ({ showError, showSuccess }),
  useAuthStore: () => ({ refreshUser }),
}))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))
enableAutoUnmount(afterEach)

const qrImage = 'data:image/png;base64,aGVsbG8='

function details(overrides: Partial<ManualOrderDetails> = {}): ManualOrderDetails {
  return {
    order: {
      id: 101,
      user_id: 10,
      amount: 2,
      pay_amount: 14.3,
      currency: 'CNY',
      fee_rate: 0,
      bonus_amount: 0,
      payment_type: 'wxpay',
      out_trade_no: 'example-order',
      status: 'PENDING',
      order_type: 'balance',
      created_at: '2030-01-01T10:00:00Z',
      expires_at: '2030-01-01T10:30:00Z',
      refund_amount: 0,
      is_manual: true,
    },
    qr_code_img: qrImage,
    payee_name: 'Owner',
    instructions: 'Scan to pay',
    submission: {},
    review_note: '',
    reviewed_at: '',
    ...overrides,
  }
}

async function mountManualView(initial = details()) {
  api.getManualOrder.mockReset().mockResolvedValue({ data: initial })
  api.submitManualOrder.mockReset()
  push.mockReset()
  showError.mockReset()
  showSuccess.mockReset()
  refreshUser.mockReset().mockResolvedValue({ balance: 2 })
  const wrapper = mount(ManualPaymentView, {
    global: { stubs: {
      AppLayout: { template: '<div><slot /></div>' },
      BaseDialog: { props: ['show', 'title'], template: '<div v-if="show" role="dialog" aria-modal="true"><h2>{{ title }}</h2><slot /><slot name="footer" /></div>' },
    } },
  })
  await flushPromises()
  return wrapper
}

describe('ManualPaymentView', () => {
  beforeEach(() => {
    params.id = '101'
  })

  it('renders the personal QR as an image resource, never as QR content to encode', async () => {
    const wrapper = await mountManualView()

    const image = wrapper.get('[data-testid="manual-qr-image"]')
    expect(image.element.tagName).toBe('IMG')
    expect(image.attributes('src')).toBe(qrImage)
    expect(wrapper.find('canvas').exists()).toBe(false)
    expect(wrapper.text()).toContain('¥14.30')
    expect(wrapper.text()).toContain('$2.00')
  })

  it('keeps the original QR bytes in a readable preview, enlarge dialog, and save link', async () => {
    const wrapper = await mountManualView()

    const preview = wrapper.get('[data-testid="manual-qr-image"]')
    expect(preview.attributes('src')).toBe(qrImage)
    expect(preview.classes()).toContain('max-h-[70vh]')
    expect(wrapper.get('[data-testid="manual-qr-save"]').attributes('href')).toBe(qrImage)
    expect(wrapper.get('[data-testid="manual-qr-save"]').attributes('download')).toBeDefined()
    await wrapper.get('[data-testid="manual-qr-enlarge"]').trigger('click')

    const dialog = wrapper.get('[role="dialog"]')
    expect(dialog.attributes('aria-modal')).toBe('true')
    expect(wrapper.get('[data-testid="manual-qr-original"]').attributes('src')).toBe(qrImage)
    expect(wrapper.get('[data-testid="manual-qr-original-save"]').attributes('href')).toBe(qrImage)
    expect(wrapper.find('canvas').exists()).toBe(false)
  })

  it('submitting payment details remains under review and does not claim success', async () => {
    const wrapper = await mountManualView()
    await wrapper.get('[data-testid="manual-qr-enlarge"]').trigger('click')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(true)
    api.submitManualOrder.mockResolvedValue({ data: details({
      order: { ...details().order, status: 'PENDING_REVIEW' },
      submission: { transaction_no: 'buyer-ref', note: 'paid', submitted_at: '2030-01-01T10:05:00Z' },
    }) })
    await wrapper.get('[data-testid="manual-transaction-no"]').setValue('buyer-ref')
    await wrapper.get('[data-testid="manual-note"]').setValue('paid')
    await wrapper.get('[data-testid="manual-submit"]').trigger('click')
    await flushPromises()

    expect(api.submitManualOrder).toHaveBeenCalledWith(101, { transaction_no: 'buyer-ref', note: 'paid' })
    expect(wrapper.text()).toContain('payment.manual.awaitingReview')
    expect(wrapper.text()).not.toContain('payment.result.success')
    expect(wrapper.find('[data-testid="manual-qr-image"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="manual-qr-enlarge"]').exists()).toBe(false)
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    expect(showSuccess).not.toHaveBeenCalled()
    expect(refreshUser).not.toHaveBeenCalled()
    expect(push).not.toHaveBeenCalled()
  })

  it('allows an expired order to be reported with the original quote warning', async () => {
    const wrapper = await mountManualView(details({
      order: { ...details().order, status: 'EXPIRED' },
    }))

    expect(wrapper.text()).toContain('payment.manual.expiredOriginalQuoteWarning')
    expect(wrapper.get('[data-testid="manual-submit"]').attributes('disabled')).toBeUndefined()
  })

  it('re-reads status after an ambiguous submit error without showing paid success', async () => {
    const wrapper = await mountManualView()
    api.submitManualOrder.mockRejectedValueOnce(new Error('connection lost'))
    api.getManualOrder.mockResolvedValueOnce({ data: details({
        order: { ...details().order, status: 'PENDING_REVIEW' },
        submission: { transaction_no: 'buyer-ref', submitted_at: '2030-01-01T10:05:00Z' },
      }) })
    await wrapper.get('[data-testid="manual-transaction-no"]').setValue('buyer-ref')
    await wrapper.get('[data-testid="manual-submit"]').trigger('click')
    await flushPromises()

    expect(api.getManualOrder).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('payment.manual.awaitingReview')
    expect(wrapper.text()).not.toContain('payment.result.success')
    expect(showError).not.toHaveBeenCalledWith('payment.manual.submitAmbiguous')
    expect(refreshUser).not.toHaveBeenCalled()
  })

  it('shows the ambiguity message only when the fresh status is still unsubmitted', async () => {
    const wrapper = await mountManualView()
    api.submitManualOrder.mockRejectedValueOnce(new Error('connection lost'))
    api.getManualOrder.mockResolvedValueOnce({ data: details() })

    await wrapper.get('[data-testid="manual-submit"]').trigger('click')
    await flushPromises()

    expect(api.getManualOrder).toHaveBeenCalledTimes(2)
    expect(showError).toHaveBeenCalledWith('payment.manual.submitAmbiguous')
    expect(showSuccess).not.toHaveBeenCalled()
    expect(refreshUser).not.toHaveBeenCalled()
  })

  it('polls until persisted completion, refreshes the auth balance once, then stops', async () => {
    vi.useFakeTimers()
    const wrapper = await mountManualView(details({
      order: { ...details().order, status: 'PENDING_REVIEW' },
    }))
    const completedDetails = details({
      order: { ...details().order, status: 'COMPLETED' },
    })
    api.getManualOrder
      .mockResolvedValueOnce({ data: completedDetails })
      .mockResolvedValue({ data: completedDetails })

    expect(refreshUser).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(5_000)
    await flushPromises()

    expect(api.getManualOrder).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('payment.status.completed')
    expect(refreshUser).toHaveBeenCalledTimes(1)
    await wrapper.get('[data-testid="manual-refresh"]').trigger('click')
    await flushPromises()
    expect(refreshUser).toHaveBeenCalledTimes(1)
    expect(vi.getTimerCount()).toBe(0)
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('offers manual refresh and does not overlap a slow poll request', async () => {
    vi.useFakeTimers()
    const wrapper = await mountManualView(details({
      order: { ...details().order, status: 'PENDING_REVIEW' },
    }))
    let resolvePoll!: (value: { data: ManualOrderDetails }) => void
    api.getManualOrder.mockReturnValueOnce(new Promise((resolve) => { resolvePoll = resolve }))

    await vi.advanceTimersByTimeAsync(5_000)
    expect(api.getManualOrder).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(10_000)
    expect(api.getManualOrder).toHaveBeenCalledTimes(2)
    resolvePoll({ data: details({ order: { ...details().order, status: 'PAID' } }) })
    await flushPromises()
    expect(wrapper.get('[data-testid="manual-refresh"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('[data-testid="manual-refresh"]').trigger('click')
    await flushPromises()
    expect(api.getManualOrder).toHaveBeenCalledTimes(3)
    wrapper.unmount()
    expect(vi.getTimerCount()).toBe(0)
    vi.useRealTimers()
  })

  it('clears scheduled status polling when the page unmounts', async () => {
    vi.useFakeTimers()
    const wrapper = await mountManualView(details({
      order: { ...details().order, status: 'PENDING_REVIEW' },
    }))
    expect(vi.getTimerCount()).toBe(1)
    wrapper.unmount()
    expect(vi.getTimerCount()).toBe(0)
    vi.useRealTimers()
  })
})
