import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import PlatformUsageBreakdown from '../PlatformUsageBreakdown.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('PlatformUsageBreakdown', () => {
  it('shows totals and platform details in RMB', () => {
    const wrapper = mount(PlatformUsageBreakdown, {
      props: {
        today: 1.25,
        total: 4.5,
        byPlatform: [{ platform: 'openai', today_actual_cost: 1.25, total_actual_cost: 4.5 }],
      },
      global: { stubs: { Icon: true } },
    })
    expect(wrapper.text()).toContain('¥1.2500')
    expect(wrapper.text()).toContain('¥4.5000')
    expect(wrapper.text()).not.toContain('$')
  })
})
