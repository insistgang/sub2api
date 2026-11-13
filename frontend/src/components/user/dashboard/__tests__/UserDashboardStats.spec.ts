import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'

// t() 回显 key；带参数时附上 JSON，便于断言计数
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) =>
        params ? `${key}:${JSON.stringify(params)}` : key,
    }),
  }
})

// Exercise the components with a non-identity rate so missing conversions are observable.
vi.mock('@/utils/displayCurrency', () => ({
  usdToCny: (amount: number) => amount * 5,
  cnyToUsd: (amount: number) => amount / 5,
}))

import UserDashboardStats from '../UserDashboardStats.vue'
import type { UserDashboardStats as UserStatsType } from '@/api/usage'

function makeStats(over: Partial<UserStatsType> = {}): UserStatsType {
  return {
    total_api_keys: 1,
    active_api_keys: 1,
    total_requests: 0,
    total_input_tokens: 0,
    total_output_tokens: 0,
    total_cache_creation_tokens: 0,
    total_cache_read_tokens: 0,
    total_tokens: 0,
    total_cost: 0,
    total_actual_cost: 0,
    today_requests: 0,
    today_input_tokens: 0,
    today_output_tokens: 0,
    today_cache_creation_tokens: 0,
    today_cache_read_tokens: 0,
    today_tokens: 0,
    today_cost: 0,
    today_actual_cost: 0,
    average_duration_ms: 0,
    rpm: 0,
    tpm: 0,
    by_platform: [],
    ...over,
  }
}

function mountStats(stats: UserStatsType, isSimple = false, balance = 0) {
  return mount(UserDashboardStats, {
    props: { stats, balance, isSimple, platformQuotas: null },
    global: { stubs: { Icon: true } },
  })
}

describe('UserDashboardStats', () => {
  it('今日消费与总计只展示标准价，不出现实际扣费金额', () => {
    const w = mountStats(
      makeStats({
        today_cost: 2.25,
        total_cost: 96.72,
        today_actual_cost: 0.45,
        total_actual_cost: 21.1128,
      })
    )
    expect(w.text()).toContain('¥11.2500')
    expect(w.text()).toContain('¥483.6000')
    expect(w.text()).not.toContain('¥2.2500')
    expect(w.text()).not.toContain('¥96.7200')
    expect(w.text()).not.toContain('¥0.4500')
    expect(w.text()).not.toContain('¥21.1128')
  })

  it('displays the balance as CNY using the display conversion rate', () => {
    const w = mountStats(makeStats(), false, 2)
    expect(w.text()).toContain('¥10.00')
  })

  it('价格不再使用美元符号', () => {
    const w = mountStats(makeStats({ today_cost: 1, total_cost: 2 }))
    expect(w.text()).not.toContain('$')
    expect(w.text()).toContain('¥')
  })

  it('不再渲染按平台拆分区块', () => {
    const w = mountStats(
      makeStats({ total_actual_cost: 1, today_actual_cost: 1, by_platform: [{
        platform: 'openai',
        total_requests: 1,
        total_tokens: 10,
        total_actual_cost: 1,
        today_requests: 1,
        today_tokens: 10,
        today_actual_cost: 1,
      }] })
    )
    expect(w.html()).not.toContain('dashboard.platformBreakdown')
    expect(w.findAll('[data-testid="platform-card"]')).toHaveLength(0)
  })
})
