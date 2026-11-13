import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import AccountStatsModal from '../AccountStatsModal.vue'
import type { Account } from '@/types'

const { getStats } = vi.hoisted(() => ({ getStats: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { accounts: { getStats } } }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))
vi.mock('vue-chartjs', () => ({ Line: { name: 'Line', props: ['data', 'options'], template: '<div />' } }))

const summary = {
  total_cost: 3.25, total_user_cost: 2.5, total_standard_cost: 5,
  avg_daily_cost: 0.5, avg_daily_user_cost: 0.25,
  total_requests: 4, avg_daily_requests: 2, total_tokens: 100,
  avg_daily_tokens: 50, actual_days_used: 2, days: 30, avg_duration_ms: 10,
  today: { label: 'Today', cost: 0.5, user_cost: 0.25, requests: 2, tokens: 50 },
  highest_cost_day: { label: 'Today', cost: 0.5, user_cost: 0.25, requests: 2 },
  highest_request_day: { label: 'Today', cost: 0.5, user_cost: 0.25, requests: 2 },
}

async function mountModal() {
  const wrapper = mount(AccountStatsModal, {
    props: { show: false, account: { id: 1, name: 'API account', status: 'active' } as Account },
    global: { stubs: { BaseDialog: { template: '<div><slot /></div>' }, Icon: true, LoadingSpinner: true, ModelDistributionChart: true, EndpointDistributionChart: true } },
  })
  await wrapper.setProps({ show: true })
  await flushPromises()
  return wrapper
}

describe('AccountStatsModal currency', () => {
  beforeEach(() => {
    getStats.mockResolvedValue({ history: [{ label: 'Today', actual_cost: 0.5, user_cost: 0.25, requests: 2 }], summary, models: [], endpoints: [], upstream_endpoints: [] })
  })

  it('renders internal account, user and standard costs in CNY', async () => {
    const wrapper = await mountModal()
    expect(wrapper.text()).toContain('¥3.25')
    expect(wrapper.text()).toContain('¥2.50')
    expect(wrapper.text()).toContain('¥5.00')
    expect(wrapper.text()).not.toContain('$')
    expect(getStats).toHaveBeenCalledWith(1, 30)
  })

  it('uses CNY chart data labels, tooltips and cost axis without affecting request counts', async () => {
    const wrapper = await mountModal()
    const chart = wrapper.findComponent({ name: 'Line' })
    const data = chart.props('data')
    const options = chart.props('options')
    expect(data.datasets[0].label).toBe('usage.accountBilled (CNY)')
    expect(data.datasets[0].data).toEqual([0.5])
    expect(data.datasets[1].label).toBe('usage.userBilled (CNY)')
    expect(data.datasets[1].data).toEqual([0.25])
    expect(data.datasets[2].data).toEqual([2])
    expect(options.plugins.tooltip.callbacks.label({ dataset: data.datasets[0], raw: 0.5 })).toBe('usage.accountBilled (CNY): ¥0.500')
    expect(options.plugins.tooltip.callbacks.label({ dataset: data.datasets[2], raw: 2 })).toBe('admin.accounts.stats.requests: 2')
    expect(options.scales.y.ticks.callback(0.5)).toBe('¥0.500')
    expect(options.scales.y.title.text).toBe('usage.accountBilled (CNY)')
  })
})
