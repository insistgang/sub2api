import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import UserDashboardCharts from '../UserDashboardCharts.vue'
import UserDashboardRecentUsage from '../UserDashboardRecentUsage.vue'
import type { ModelStat, UsageLog } from '@/types'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))
vi.mock('vue-chartjs', () => ({ Doughnut: { template: '<div />' } }))
vi.mock('@/utils/displayCurrency', () => ({
  usdToCny: (amount: number) => amount * 5,
  cnyToUsd: (amount: number) => amount / 5,
}))

describe('user dashboard currency display', () => {
  it('converts model costs to CNY in the model table', () => {
    const wrapper = mount(UserDashboardCharts, {
      props: {
        loading: false,
        startDate: '',
        endDate: '',
        granularity: 'day',
        trend: [],
        models: [{ model: 'gpt-test', requests: 1, input_tokens: 1, output_tokens: 1, cache_creation_tokens: 0, cache_read_tokens: 0, total_tokens: 2, cost: 2.5, actual_cost: 2.5 } as ModelStat],
      },
      global: { stubs: { DateRangePicker: true, Select: true, Doughnut: true, TokenUsageTrend: true } },
    })

    expect(wrapper.text()).toContain('¥12.5000')
    expect(wrapper.text()).not.toContain('¥2.5000')
  })

  it('converts recent usage costs to CNY', () => {
    const wrapper = mount(UserDashboardRecentUsage, {
      props: {
        loading: false,
        data: [{ id: 1, model: 'gpt-test', created_at: '2026-01-01T00:00:00Z', total_cost: 2.5, input_tokens: 1, output_tokens: 1 } as UsageLog],
      },
      global: { stubs: { Icon: true, LoadingSpinner: true, EmptyState: true, RouterLink: true } },
    })

    expect(wrapper.text()).toContain('¥12.5000')
    expect(wrapper.text()).not.toContain('¥2.5000')
  })
})
