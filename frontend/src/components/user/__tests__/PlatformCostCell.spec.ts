import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import PlatformCostCell from '../PlatformCostCell.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('PlatformCostCell', () => {
  it('shows platform costs in RMB', () => {
    const wrapper = mount(PlatformCostCell, {
      props: { usage: { today_actual_cost: 1.25, total_actual_cost: 4.5 } },
    })
    expect(wrapper.text()).toContain('¥1.2500')
    expect(wrapper.text()).toContain('¥4.5000')
    expect(wrapper.text()).not.toContain('$')
  })
})
