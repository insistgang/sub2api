import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import EndpointDistributionChart from '../EndpointDistributionChart.vue'

vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('vue-chartjs', () => ({ Doughnut: { props: ['data', 'options'], template: '<div />' } }))

describe('EndpointDistributionChart currency', () => {
  it('uses CNY in cost rows and tooltip with the configured 1:1 convention', () => {
    const wrapper = mount(EndpointDistributionChart, {
      props: { endpointStats: [{ endpoint: '/v1/responses', requests: 1, total_tokens: 100, cost: 0.4, actual_cost: 0.2 }], metric: 'actual_cost', enableBreakdown: false },
      global: { stubs: { LoadingSpinner: true } },
    })
    expect(wrapper.text()).toContain('¥0.200')
    expect(wrapper.text()).toContain('¥0.400')
    expect(wrapper.text()).not.toContain('$')
    const options = (wrapper.vm as any).$?.setupState.doughnutOptions
    expect(options.plugins.tooltip.callbacks.label({ label: '/v1/responses', raw: 0.2, dataset: { data: [0.2] } })).toBe('/v1/responses: ¥0.200 (100.0%)')
  })
})
