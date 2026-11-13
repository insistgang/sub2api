import { enableAutoUnmount, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import IntervalRow from '../IntervalRow.vue'
import type { IntervalFormEntry } from '../types'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
const interval: IntervalFormEntry = {
  min_tokens: 0, max_tokens: null, tier_label: '', sort_order: 0,
  input_price: null, output_price: null, cache_write_price: null, cache_read_price: null,
  input_multiplier: null, output_multiplier: null, cache_write_multiplier: null,
  cache_read_multiplier: null, per_request_price: null,
}

describe('pricing interval token bounds', () => {
  it.each(['1.0', '0.00', '0.0100'])('preserves the exact 1:1 decimal input %s', async (value) => {
    const wrapper = mount(IntervalRow, { props: { interval: { ...interval, input_price: value }, mode: 'token' } })
    const input = wrapper.findAll('input[type="number"]')[2]
    expect((input.element as HTMLInputElement).value).toBe(value)
    await input.setValue(value)
    expect(wrapper.emitted('update')?.at(-1)?.[0]).toMatchObject({ input_price: value })
  })

  it('shows video tier prices in CNY per second', () => {
    const wrapper = mount(IntervalRow, { props: { interval: { ...interval, per_request_price: '0.125' }, mode: 'video' } })
    expect(wrapper.text()).toContain('¥/s')
    expect((wrapper.findAll('input[type="number"]').at(-1)!.element as HTMLInputElement).value).toBe('0.125')
  })

  it('shows CNY price units and preserves 1:1 price edits and non-price multipliers', async () => {
    const wrapper = mount(IntervalRow, { props: { interval: { ...interval, input_price: '2.5' }, mode: 'token', enableMultipliers: true } })
    expect(wrapper.text()).toContain('¥/M')
    expect(wrapper.text()).not.toContain('$/M')
    const inputs = wrapper.findAll('input[type="number"]')
    expect((inputs[2].element as HTMLInputElement).value).toBe('2.5')
    await inputs[2].setValue('3.125')
    expect(wrapper.emitted('update')?.[0]?.[0]).toMatchObject({ input_price: '3.125' })
    await inputs[7].setValue('2')
    expect(wrapper.emitted('update')?.[1]?.[0]).toMatchObject({ input_multiplier: '2' })
  })

  it.each([
    ['1e6', 1000000, 1000000],
    ['2.72e5', 272000, 272000],
    ['128000', 128000, 128000],
    ['', 0, null],
  ])('parses %s without losing the exponent', async (value, min, max) => {
    const wrapper = mount(IntervalRow, { props: { interval, mode: 'token' } })
    const inputs = wrapper.findAll('input[type="number"]')
    await inputs[0].setValue(value)
    await inputs[1].setValue(value)
    expect(wrapper.emitted('update')?.[0]?.[0]).toMatchObject({ min_tokens: min })
    expect(wrapper.emitted('update')?.[1]?.[0]).toMatchObject({ max_tokens: max })
  })
})
