import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import EnginePanel from './panels/EnginePanel.vue'
import SavestateManager from './harvester/SavestateManager.vue'
import TopBar from './TopBar.vue'
import { mockFetch } from '@/test/fetch'
import { settingsFixture, statusFixture } from '@/test/fixtures'
import { useLogStore } from '@/stores/log'
import { deepMerge, useSettingsStore } from '@/stores/settings'
import { useStatusStore } from '@/stores/status'
import { useUiStore } from '@/stores/ui'
import type { Settings } from '@/api/types'

const tid = (id: string) => `[data-testid="${id}"]`

beforeEach(() => {
  setActivePinia(createPinia())
  localStorage.clear()
})
afterEach(() => vi.unstubAllGlobals())

describe('EnginePanel', () => {
  it('switches engine parameter blocks via PATCH settings', async () => {
    let server: Settings = settingsFixture()
    const { calls } = mockFetch({
      'PATCH /api/settings': (c) => (server = deepMerge(server, c.body)),
    })
    useSettingsStore().settings = settingsFixture()
    const w = mount(EnginePanel)

    expect(w.find(tid('engine-params-nightmare')).exists()).toBe(true)
    expect(w.find(tid('nightmare-algo')).exists()).toBe(true)

    await w.find(tid('engine-select')).setValue('custom')
    await flushPromises()
    expect(calls.at(-1)).toMatchObject({ method: 'PATCH', body: { engine: 'custom' } })
    expect(w.find(tid('engine-params-custom')).exists()).toBe(true)
    expect(w.find(tid('custom-engine-form')).exists()).toBe(true)
    expect(w.find(tid('nightmare-algo')).exists()).toBe(false)

    // Store source swaps value settings for store settings.
    await w.find(tid('custom-source-store')).setValue(true)
    await flushPromises()
    expect(w.find(tid('custom-store-type')).exists()).toBe(true)
    expect(w.find(tid('custom-value-source')).exists()).toBe(false)

    await w.find(tid('engine-select')).setValue('cluster')
    await flushPromises()
    expect(w.find(tid('cluster-method')).exists()).toBe(true)

    await w.find(tid('engine-select')).setValue('pipe')
    await flushPromises()
    expect(w.find(tid('lock-units')).exists()).toBe(true)
    expect(w.find(tid('clear-units')).text()).toBe('Clear pipes')
  })

  it('edits min/max for the current precision as decimal strings', async () => {
    let server: Settings = { ...settingsFixture(), precision: 8 }
    const { calls } = mockFetch({
      'PATCH /api/settings': (c) => (server = deepMerge(server, c.body)),
    })
    useSettingsStore().settings = { ...settingsFixture(), precision: 8 }
    const w = mount(EnginePanel)
    const max = w.find(tid('nightmare-range-max'))
    expect((max.element as HTMLInputElement).value).toBe('18446744073709551615')
    await max.setValue('18446744073709551616')
    await max.trigger('change')
    expect(calls).toHaveLength(0)
    expect(max.classes()).toContain('invalid')
    await max.setValue('9007199254740993')
    await max.trigger('change')
    await flushPromises()
    expect(calls[0]!.body).toEqual({
      nightmare: { ranges: { '8': { min: '0', max: '9007199254740993' } } },
    })
  })

  it('uses an uncapped number box next to the slider', async () => {
    const { calls } = mockFetch({
      'PATCH /api/settings': (c) => deepMerge(settingsFixture(), c.body),
    })
    useSettingsStore().settings = settingsFixture()
    const w = mount(EnginePanel)
    const n = w.find(tid('intensity-number'))
    await n.setValue('1000000')
    await n.trigger('change')
    await flushPromises()
    expect(calls[0]!.body).toEqual({ intensity: 1000000 })
  })
})

describe('SavestateManager', () => {
  it('saves in SAVE mode and loads in LOAD mode', async () => {
    const { calls } = mockFetch({
      'POST /api/savestates/1': () => ({ slot: 1, key: 'state1', label: '' }),
      'POST /api/savestates/1/load': () => undefined,
      'PATCH /api/savestates/1': (c) => ({ slot: 1, key: 'state1', ...(c.body as object) }),
    })
    useStatusStore().set(statusFixture())
    const ui = useUiStore()
    const w = mount(SavestateManager)

    expect(w.findAll('[data-testid^="slot-"][data-filled]')).toHaveLength(10)

    // LOAD mode on an empty slot only selects it.
    await w.find(tid('slot-1-button')).trigger('click')
    await flushPromises()
    expect(calls).toHaveLength(0)
    expect(ui.selectedSlot).toBe(1)

    await w.find(tid('savestate-mode')).trigger('click')
    expect(w.find(tid('savestate-mode')).text()).toBe('SAVE')
    await w.find(tid('slot-1-button')).trigger('click')
    await flushPromises()
    expect(calls.map((c) => `${c.method} ${c.path}`)).toEqual(['POST /api/savestates/1'])
    expect(w.find(tid('slot-1')).attributes('data-filled')).toBe('true')

    await w.find(tid('savestate-mode')).trigger('click')
    await w.find(tid('slot-1-button')).trigger('click')
    await flushPromises()
    expect(calls.at(-1)).toMatchObject({ method: 'POST', path: '/api/savestates/1/load' })

    await w.find(tid('slot-1-label')).setValue('boss room')
    await flushPromises()
    expect(calls.at(-1)).toMatchObject({ method: 'PATCH', body: { label: 'boss room' } })
  })

  it('pages through 50 slots and disables saving without a ROM', async () => {
    mockFetch({})
    useStatusStore().set(statusFixture({ connected: false }))
    const w = mount(SavestateManager)
    await w.find(tid('savestate-mode')).trigger('click')
    const b = w.find(tid('slot-1-button'))
    expect(b.attributes('disabled')).toBeDefined()
    expect(b.attributes('title')).toBe('Emulator not connected')
    for (let i = 0; i < 4; i++) await w.find(tid('slot-page-next')).trigger('click')
    expect(w.find(tid('slot-page')).text()).toBe('5 / 5')
    expect(w.find(tid('slot-50')).exists()).toBe(true)
    expect(w.find(tid('slot-page-next')).attributes('disabled')).toBeDefined()
  })
})

describe('TopBar', () => {
  it('loads the last backup without dropping it', async () => {
    const { calls } = mockFetch({ 'POST /api/protection/last': () => undefined })
    const st = useStatusStore()
    st.set(statusFixture())
    const w = mount(TopBar)

    const last = w.find(tid('protection-last'))
    expect(last.attributes('disabled')).toBeDefined()
    expect(last.attributes('title')).toBe('No backups yet')

    st.set(statusFixture({ protectionBackups: 2 }))
    await flushPromises()
    expect(last.attributes('disabled')).toBeUndefined()
    await last.trigger('click')
    await flushPromises()
    expect(calls.map((c) => `${c.method} ${c.path}`)).toEqual(['POST /api/protection/last'])
    expect(useLogStore().entries.at(-1)?.msg).toBe('game protection: loaded last backup')
  })
})
