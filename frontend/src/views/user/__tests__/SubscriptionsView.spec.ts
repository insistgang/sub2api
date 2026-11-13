import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import SubscriptionsView from '../SubscriptionsView.vue'

const { getMySubscriptions, push, showError } = vi.hoisted(() => ({
  getMySubscriptions: vi.fn(), push: vi.fn(), showError: vi.fn(),
}))

vi.mock('@/api/subscriptions', () => ({ default: { getMySubscriptions } }))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<div><slot /></div>' } }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, cachedPublicSettings: {} }) }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))

const subscription = () => ({
  id: 1, group_id: 2, status: 'active', expires_at: null,
  daily_usage_usd: 2.25, weekly_usage_usd: 12.5, monthly_usage_usd: 99.125,
  daily_window_start: null, weekly_window_start: null, monthly_window_start: null,
  group: {
    name: 'Test subscription', platform: 'openai', rate_multiplier: 0.5,
    daily_limit_usd: 10, weekly_limit_usd: 50, monthly_limit_usd: 200,
  },
})

const mountView = () => mount(SubscriptionsView, {
  global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } },
})

describe('SubscriptionsView yuan quotas', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getMySubscriptions.mockResolvedValue([subscription()])
  })

  it('shows all usage and limit amounts in yuan while retaining quota percentages', async () => {
    const wrapper = mountView()
    await flushPromises()
    const text = wrapper.text()
    expect(text).toContain('¥2.25 / ¥10.00')
    expect(text).toContain('¥12.50 / ¥50.00')
    expect(text).toContain('¥99.13 / ¥200.00')
    expect(text).not.toContain('$')
    const widths = wrapper.findAll('[style]').map((element) => element.attributes('style'))
    expect(widths).toContain('width: 22.5%;')
    expect(widths).toContain('width: 25%;')
    expect(widths).toContain('width: 49.5625%;')
    wrapper.unmount()
  })

  it('keeps unlimited windows free of amount and progress displays', async () => {
    const item = subscription()
    item.group.daily_limit_usd = 0
    item.group.weekly_limit_usd = 0
    item.group.monthly_limit_usd = 0
    getMySubscriptions.mockResolvedValue([item])
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).not.toContain('¥')
    expect(wrapper.findAll('[style]')).toHaveLength(0)
    wrapper.unmount()
  })
})
