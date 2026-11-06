import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import PromoCodesView from '../PromoCodesView.vue'

const { list, create, update } = vi.hoisted(() => ({ list: vi.fn(), create: vi.fn(), update: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { promo: { list, create, update } } }))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<div><slot /></div>' } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() }) }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: vi.fn() }) }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }),
}))

const code = { id: 1, code: 'GIFT', bonus_amount: 1.123456789, max_uses: 0, used_count: 0,
  expires_at: null, created_at: '2026-01-01T00:00:00Z', status: 'active', notes: '' }
const mountView = () => mount(PromoCodesView, { global: { stubs: {
  TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /></div>' },
  DataTable: { props: ['data'], template: '<div v-for="row in data" :key="row.id"><slot name="cell-bonus_amount" :value="row.bonus_amount" /><slot name="cell-actions" :row="row" /></div>' },
  BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
  Pagination: true, ConfirmDialog: true, Select: true, Icon: true,
} } })

describe('PromoCodesView yuan bonus amounts', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    list.mockResolvedValue({ items: [code], total: 1, pages: 1 })
    create.mockResolvedValue(code)
    update.mockResolvedValue(code)
  })

  it('shows yuan and preserves precise amounts when an existing code is saved', async () => {
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain('¥1.12')
    await wrapper.get('button[title="common.edit"]').trigger('click')
    const input = wrapper.get('#edit-promo-form input[type="number"]')
    expect((input.element as HTMLInputElement).value).toBe('1.123456789')
    expect((input.element as HTMLInputElement).validity.stepMismatch).toBe(false)
    await wrapper.get('#edit-promo-form').trigger('submit')
    await flushPromises()
    expect(update).toHaveBeenCalledWith(code.id, expect.objectContaining({ bonus_amount: code.bonus_amount }))
    wrapper.unmount()
  })

  it('submits a new yuan bonus in the existing API units', async () => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').find((button) => button.text() === 'admin.promo.createCode')!.trigger('click')
    await wrapper.get('#create-promo-form input[type="number"]').setValue('8.125')
    await wrapper.get('#create-promo-form').trigger('submit')
    await flushPromises()
    expect(create).toHaveBeenCalledWith(expect.objectContaining({ bonus_amount: 8.125 }))
    wrapper.unmount()
  })
})
