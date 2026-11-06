import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import UseKeyModal from '../UseKeyModal.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: vi.fn() }) }))
vi.mock('file-saver', () => ({ saveAs: vi.fn() }))

function openModal(platform: string, baseUrl = 'https://example.com/v1') {
  return mount(UseKeyModal, {
    props: { show: true, apiKey: 'sk-fixture', baseUrl, platform },
    global: { stubs: { BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' }, Icon: { template: '<span />' } } }
  })
}

async function selectClient(wrapper: ReturnType<typeof openModal>, key: string) {
  const button = wrapper.find('nav[aria-label="Client"]').findAll('button').find(item => item.text().includes(key))
  expect(button).toBeDefined()
  await button!.trigger('click')
}

describe('API key client setup configuration', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('loads the Codex model catalog correctly when the site base ends in v1beta', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, text: async () => '{"models":[{"slug":"gpt-6.1-terra"}]}' })
    vi.stubGlobal('fetch', fetchMock)
    const wrapper = openModal('openai', 'https://example.com/api/v1beta/')
    await wrapper.get('[data-testid="codex-model-catalog-fetch"]').trigger('click')
    await flushPromises()
    expect(fetchMock).toHaveBeenCalledWith('https://example.com/api/v1/models?client_version=0.158.0', expect.anything())
    expect(wrapper.text()).toContain('https://example.com/api/v1/models')
  })

  it.each(['https://example.com', 'https://example.com/v1', 'https://example.com/v1/', 'https://example.com/v1beta'])(
    'uses a normalized Gemini CLI base URL and a supported model for %s', async baseUrl => {
      const wrapper = openModal('gemini', baseUrl)
      const content = wrapper.find('pre code').text()
      expect(content).toContain('GOOGLE_GEMINI_BASE_URL="https://example.com"')
      expect(content).toContain('GEMINI_MODEL="gemini-2.5-flash"')
      expect(content).toContain('GOOGLE_GENAI_API_VERSION="v1beta"')
      expect(content).not.toContain('gemini-2.0-flash')
    }
  )

  it('normalizes Antigravity Claude and Gemini paths for versioned site URLs', async () => {
    const wrapper = openModal('antigravity')
    expect(wrapper.find('pre code').text()).toContain('ANTHROPIC_BASE_URL="https://example.com/antigravity"')
    await selectClient(wrapper, 'keys.useKeyModal.cliTabs.geminiCli')
    const content = wrapper.find('pre code').text()
    expect(content).toContain('GOOGLE_GEMINI_BASE_URL="https://example.com/antigravity"')
    expect(content).toContain('GEMINI_MODEL="gemini-3.8-flash"')
    expect(content).toContain('GOOGLE_GENAI_API_VERSION="v1beta"')
  })

  it.each([
    ['Windows CMD', 'set GOOGLE_GEMINI_BASE_URL=https://example.com', 'set GOOGLE_GENAI_API_VERSION=v1beta', 'set GEMINI_MODEL=gemini-2.5-flash'],
    ['PowerShell', '$env:GOOGLE_GEMINI_BASE_URL="https://example.com"', '$env:GOOGLE_GENAI_API_VERSION="v1beta"', '$env:GEMINI_MODEL="gemini-2.5-flash"']
  ])('generates working Gemini CLI settings for %s', async (shell, expectedBase, expectedVersion, expectedModel) => {
    const wrapper = openModal('gemini')
    await wrapper.findAll('button').find(button => button.text().trim() === shell)!.trigger('click')
    const content = wrapper.find('pre code').text()
    expect(content).toContain(expectedBase)
    expect(content).toContain(expectedVersion)
    expect(content).toContain(expectedModel)
  })

  it('normalizes the base URL for native Anthropic examples', () => {
    const wrapper = openModal('anthropic')
    expect(wrapper.find('pre code').text()).toContain('ANTHROPIC_BASE_URL="https://example.com"')
  })

  it('prefers a Kimi model present in the current group mappings', async () => {
    const wrapper = openModal('kimi')
    await selectClient(wrapper, 'keys.useKeyModal.cliTabs.codexCli')
    expect(wrapper.findAll('pre code').map(item => item.text()).join('\n')).toContain('model = "k3.1-pro"')
  })

  it.each([
    ['kimi', 'k3.1-pro'],
    ['zhipu', 'glm-4.7'],
    ['deepseek', 'deepseek-v4-pro'],
    ['minimax', 'MiniMax-M3'],
    ['opencode_go', 'glm-5.3']
  ])('uses an OpenAI-compatible OpenCode provider for %s without advertising GPT models', async (platform, model) => {
    const wrapper = openModal(platform)
    await selectClient(wrapper, 'keys.useKeyModal.cliTabs.opencode')
    const config = JSON.parse(wrapper.find('pre code').text())
    const providers = Object.values(config.provider) as Array<{ npm?: string; options: { baseURL: string }; models: Record<string, unknown> }>
    expect(providers).toHaveLength(1)
    expect(providers[0]!.npm).toBe('@ai-sdk/openai-compatible')
    expect(providers[0]!.options.baseURL).toBe('https://example.com/v1')
    expect(providers[0]!.models).toHaveProperty(model)
    expect(providers[0]!.models).not.toHaveProperty('gpt-6.1-terra')
  })

  it('uses current Grok defaults and the supported 2.0 image model', () => {
    const content = openModal('grok').findAll('pre code').map(item => item.text()).join('\n')
    expect(content).toContain('default = "grok-4.7"')
    expect(content).toContain('[model."grok-4.7"]')
    expect(content).toContain('image_gen_model_override = "grok-imagine-image-2.0"')
  })

  it('includes current Antigravity Gemini and Claude models in OpenCode examples', async () => {
    const wrapper = openModal('antigravity')
    await selectClient(wrapper, 'keys.useKeyModal.cliTabs.opencode')
    const configs = wrapper.findAll('pre code').map(item => JSON.parse(item.text()))
    const gemini = configs.find(config => config.provider['antigravity-gemini'])!.provider['antigravity-gemini'].models
    const claude = configs.find(config => config.provider['antigravity-claude'])!.provider['antigravity-claude'].models
    expect(gemini).toHaveProperty('gemini-3.8-flash')
    expect(gemini).toHaveProperty('gemini-4-argon-high')
    expect(claude).toHaveProperty('claude-sonnet-5-5-high')
    expect(claude).toHaveProperty('claude-opus-5-5-high')
  })

  it('provides an escaped JSON payload for the System One Windows CMD example', async () => {
    const wrapper = openModal('typesafe')
    await wrapper.findAll('button').find(button => button.text().trim() === 'Windows CMD')!.trigger('click')
    expect(wrapper.find('pre code').text()).toContain('\\"model\\":\\"jev-latest\\"')
  })
})
