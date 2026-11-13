import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    get,
    post,
  },
}))

import { paymentAPI } from '@/api/payment'
import { adminPaymentAPI } from '@/api/admin/payment'

describe('payment api', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
    get.mockResolvedValue({ data: {} })
    post.mockResolvedValue({ data: {} })
  })

  it('keeps legacy public out_trade_no verification for upgrade compatibility', async () => {
    await paymentAPI.verifyOrderPublic('legacy-order-no')

    expect(post).toHaveBeenCalledWith('/payment/public/orders/verify', {
      out_trade_no: 'legacy-order-no',
    })
  })

  it('keeps signed public resume-token resolve endpoint', async () => {
    await paymentAPI.resolveOrderPublicByResumeToken('resume-token-123')

    expect(post).toHaveBeenCalledWith('/payment/public/orders/resolve', {
      resume_token: 'resume-token-123',
    })
  })

  it('loads and submits manual-payment details using the owned order routes', async () => {
    await paymentAPI.getManualOrder(101)
    await paymentAPI.submitManualOrder(101, { transaction_no: 'buyer-reference', note: 'Paid once' })

    expect(get).toHaveBeenCalledWith('/payment/orders/101/manual')
    expect(post).toHaveBeenCalledWith('/payment/orders/101/manual/submit', {
      transaction_no: 'buyer-reference',
      note: 'Paid once',
    })
  })

  it('uses admin manual order detail, approval, and rejection endpoints', async () => {
    await adminPaymentAPI.getManualOrder(101)
    await adminPaymentAPI.approveManualOrder(101, {
      transaction_no: 'verified-reference',
      received_amount: 7,
      confirm_received: true,
      note: 'Matched bank statement',
    })
    await adminPaymentAPI.rejectManualOrder(101, { reason: 'No matching payment' })

    expect(get).toHaveBeenCalledWith('/admin/payment/orders/101/manual')
    expect(post).toHaveBeenCalledWith('/admin/payment/orders/101/manual/approve', {
      transaction_no: 'verified-reference',
      received_amount: 7,
      confirm_received: true,
      note: 'Matched bank statement',
    })
    expect(post).toHaveBeenCalledWith('/admin/payment/orders/101/manual/reject', {
      reason: 'No matching payment',
    })
  })
})
