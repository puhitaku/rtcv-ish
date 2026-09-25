import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import EngineConfig from './engine/EngineConfig.vue'
import SavestateManager from './harvester/SavestateManager.vue'
import StashHistory from './harvester/StashHistory.vue'
import StockpileManager from './harvester/StockpileManager.vue'
import { appended } from './harvester/useScrollOnAppend'
import SettingsPanel from './panels/SettingsPanel.vue'
import EngineSection from './EngineSection.vue'
import SideBar from './SideBar.vue'
import TopBar from './TopBar.vue'
import { mockFetch } from '@/test/fetch'
import { settingsFixture, statusFixture } from '@/test/fixtures'
import { useLogStore } from '@/stores/log'
import { deepMerge, useSettingsStore } from '@/stores/settings'
import { useStashStore } from '@/stores/stash'
import { useStatusStore } from '@/stores/status'
import { useStockpileStore } from '@/stores/stockpile'
import { useUiStore } from '@/stores/ui'
import type { Settings, StashKey } from '@/api/types'

const tid = (id: string) => `[data-testid="${id}"]`

beforeEach(() => {
  setActivePinia(createPinia())
  localStorage.clear()
})
afterEach(() => vi.unstubAllGlobals())

describe('EngineConfig', () => {
  it('switches engine parameter blocks via PATCH settings', async () => {
    let server: Settings = settingsFixture()
    const { calls } = mockFetch({
      'PATCH /api/settings': (c) => (server = deepMerge(server, c.body)),
    })
    useSettingsStore().settings = settingsFixture()
    const w = mount(EngineConfig)

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
    const w = mount(EngineConfig)
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
    const w = mount(EngineConfig)
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
    const posts = calls.filter((c) => c.method === 'POST')
    expect(posts.map((c) => `${c.method} ${c.path}`)).toEqual(['POST /api/protection/last'])
    expect(useLogStore().entries.at(-1)?.msg).toBe('game protection: loaded last backup')
  })
})

describe('TopBar layout', () => {
  it('links the logo to the repository in a new tab', () => {
    mockFetch({})
    const w = mount(TopBar)
    const a = w.find(tid('logo-link'))
    expect(a.element.tagName).toBe('A')
    expect(a.text()).toBe('rtcv-ish')
    expect(a.attributes('href')).toBe('https://github.com/puhitaku/rtcv-ish')
    expect(a.attributes('target')).toBe('_blank')
    expect(a.attributes('rel')).toBe('noopener')
    expect(a.classes()).toContain('no-underline')
    expect(a.classes()).toContain('hover:underline')
  })

  it('reserves no fixed-width slots on the left side', () => {
    mockFetch({})
    useStatusStore().set(statusFixture())
    const w = mount(TopBar)
    expect(w.find(tid('busy-slot')).exists()).toBe(false)
    for (const id of ['quick-actions', 'emulator-label', 'frame-counter', 'qa-pause']) {
      expect(w.find(tid(id)).attributes('class') ?? '').not.toMatch(/\b(min-)?w-\[/)
    }
    expect(w.find(tid('frame-counter')).classes()).toContain('tabular-nums')
  })
})

describe('TopBar busy / unresponsive', () => {
  afterEach(() => vi.useRealTimers())

  it('hides the busy indicator for operations shorter than 1 s', async () => {
    vi.useFakeTimers()
    mockFetch({})
    const st = useStatusStore()
    st.set(statusFixture())
    const w = mount(TopBar)

    st.set(statusFixture({ busy: { operation: 'blast', sinceMs: 0 } }))
    await flushPromises()
    expect(w.find(tid('busy-indicator')).exists()).toBe(false)
    expect(w.find(tid('manual-blast')).attributes('disabled')).toBeDefined()

    vi.advanceTimersByTime(999)
    st.tick()
    await flushPromises()
    expect(w.find(tid('busy-indicator')).exists()).toBe(false)

    vi.advanceTimersByTime(1)
    st.tick()
    await flushPromises()
    const busy = w.find(tid('busy-indicator'))
    expect(busy.text()).toBe('blast 1s')
    // Right-aligned, immediately left of Manual Blast.
    expect(busy.element.nextElementSibling?.getAttribute('data-testid')).toBe('manual-blast')

    st.set(statusFixture())
    await flushPromises()
    expect(w.find(tid('busy-indicator')).exists()).toBe(false)
  })

  it('shows the busy operation with elapsed seconds and disables operations', async () => {
    vi.useFakeTimers()
    mockFetch({})
    const st = useStatusStore()
    st.set(statusFixture())
    const w = mount(TopBar)
    expect(w.find(tid('busy-indicator')).exists()).toBe(false)
    expect(w.find(tid('manual-blast')).attributes('disabled')).toBeUndefined()

    st.set(statusFixture({ busy: { operation: 'loadRom', sinceMs: 12_400 } }))
    await flushPromises()
    expect(w.find(tid('busy-indicator')).text()).toBe('loadRom 12s')
    const blast = w.find(tid('manual-blast'))
    expect(blast.attributes('disabled')).toBeDefined()
    expect(blast.attributes('title')).toBe('Emulator is busy: loadRom')
    expect(w.find(tid('protection-now')).attributes('disabled')).toBeDefined()

    vi.advanceTimersByTime(2000)
    st.tick()
    await flushPromises()
    expect(w.find(tid('busy-indicator')).text()).toBe('loadRom 14s')
    expect(w.find(tid('emulator-label')).text()).toBe('fake 1')

    st.set(statusFixture())
    await flushPromises()
    expect(w.find(tid('busy-indicator')).exists()).toBe(false)
    expect(w.find(tid('manual-blast')).attributes('disabled')).toBeUndefined()
  })

  it('shows an unresponsive chip in the warning color', async () => {
    mockFetch({})
    const st = useStatusStore()
    st.set(statusFixture({ unresponsive: true }))
    const w = mount(TopBar)
    const chip = w.find(tid('connection-status'))
    expect(w.find(tid('emulator-label')).text()).toBe('unresponsive')
    expect(chip.classes()).toContain('text-warn')
    expect(chip.attributes('title')).toContain('stopped answering')
    expect(chip.find('.bg-warn').exists()).toBe(true)
    expect(w.find(tid('manual-blast')).attributes('title')).toBe(
      'Emulator is unresponsive; disconnect or quit it',
    )
  })

  it('keeps Disconnect and Quit / kill clickable while stuck', async () => {
    const { calls } = mockFetch({
      'GET /api/emulators': [],
      'POST /api/emulator/quit': () => undefined,
      'POST /api/emulator/disconnect': () => statusFixture({ connected: false }),
    })
    const st = useStatusStore()
    st.set(statusFixture({ busy: { operation: 'loadRom', sinceMs: 1000 } }))
    const w = mount(TopBar)
    await w.find(tid('connection-status')).trigger('click')
    await flushPromises()

    const quit = w.find(tid('emu-quit'))
    expect(quit.text()).toBe('Quit emulator')
    expect(quit.attributes('disabled')).toBeUndefined()
    expect(w.find(tid('disconnect-button')).attributes('disabled')).toBeUndefined()
    expect(w.find(tid('emu-reset')).attributes('disabled')).toBeDefined()
    expect(w.find(tid('rom-load')).attributes('title')).toBe('Emulator is busy: loadRom')

    st.set(statusFixture({ busy: { operation: 'loadRom', sinceMs: 6000 } }))
    await flushPromises()
    expect(quit.text()).toBe('Quit / kill')
    expect(quit.attributes('title')).toContain('force-kills an emulator it launched')
    expect(quit.attributes('title')).toContain('3 s')

    st.set(statusFixture({ unresponsive: true }))
    await flushPromises()
    expect(quit.text()).toBe('Quit / kill')

    await quit.trigger('click')
    await w.find(tid('disconnect-button')).trigger('click')
    await flushPromises()
    const posts = calls.filter((c) => c.method === 'POST').map((c) => c.path)
    expect(posts).toEqual(['/api/emulator/quit', '/api/emulator/disconnect'])
    expect(st.connected).toBe(false)
  })
})

describe('freeze mode setting', () => {
  it('is in the Settings panel step settings and PATCHes freezeMode', async () => {
    const { calls } = mockFetch({
      'PATCH /api/settings': (c) => deepMerge(settingsFixture(), c.body),
    })
    useSettingsStore().settings = settingsFixture()
    useStatusStore().set(statusFixture())
    const w = mount(SettingsPanel)
    const sel = w.find(tid('settings-freeze-mode'))
    expect((sel.element as HTMLSelectElement).value).toBe('hard')
    expect(sel.findAll('option').map((o) => o.text())).toEqual([
      'per frame',
      'per scanline',
      'hard',
    ])
    expect(w.find(tid('settings-freeze-mode-fallback')).exists()).toBe(false)
    await sel.setValue('scanline')
    await flushPromises()
    expect(calls.at(-1)).toMatchObject({ method: 'PATCH', body: { freezeMode: 'scanline' } })
    expect(useSettingsStore().settings!.freezeMode).toBe('scanline')
  })

  it('shows the fallback when the emulator lacks the mode', () => {
    useSettingsStore().settings = settingsFixture()
    const s = statusFixture()
    const caps = { ...s.emulator!.capabilities, hardUnits: false, scanlineUnits: false }
    useStatusStore().set({ ...s, emulator: { ...s.emulator!, capabilities: caps } })
    const w = mount(SettingsPanel)
    const opts = w.find(tid('settings-freeze-mode')).findAll('option')
    expect(opts.map((o) => o.text())).toEqual([
      'per frame',
      'per scanline (unsupported)',
      'hard (unsupported)',
    ])
    expect(w.find(tid('settings-freeze-mode-fallback')).text()).toContain('using per frame')
  })

  it('is in the Freeze, Hellgenie, Pipe and Custom engine blocks', () => {
    for (const engine of ['freeze', 'hellgenie', 'pipe', 'custom'] as const) {
      setActivePinia(createPinia())
      useSettingsStore().settings = { ...settingsFixture(), engine }
      const w = mount(EngineConfig)
      expect(w.find(tid('freeze-mode')).exists(), engine).toBe(true)
      w.unmount()
    }
    useSettingsStore().settings = settingsFixture()
    expect(mount(EngineConfig).find(tid('freeze-mode')).exists()).toBe(false)
  })
})

describe('TopBar quick actions', () => {
  const emus = [
    { name: 'missing', path: '/nope', present: false },
    { name: 'melonDS', path: '/bin/melonDS', present: true },
  ]
  const posts = (calls: { method: string; path: string }[]) =>
    calls.filter((c) => c.method === 'POST').map((c) => c.path)

  it('launches the first available emulator without a ROM', async () => {
    const { calls } = mockFetch({
      'GET /api/emulators': emus,
      'POST /api/emulator/launch': () => statusFixture(),
    })
    const st = useStatusStore()
    st.set(statusFixture({ connected: false }))
    const w = mount(TopBar)
    await flushPromises()

    const launch = w.find(tid('qa-launch'))
    expect(launch.attributes('disabled')).toBeUndefined()
    await launch.trigger('click')
    await flushPromises()
    expect(calls.find((c) => c.path === '/api/emulator/launch')!.body).toEqual({
      name: 'melonDS',
    })
    expect(useLogStore().entries.at(-1)?.msg).toBe('launched melonDS')
    expect(launch.attributes('disabled')).toBeDefined()
    expect(launch.attributes('title')).toBe('Already connected')
  })

  it('disables Launch and Load ROM without an emulator to launch', async () => {
    mockFetch({ 'GET /api/emulators': [emus[0]] })
    useStatusStore().set(statusFixture({ connected: false }))
    const w = mount(TopBar)
    await flushPromises()
    for (const id of ['qa-launch', 'qa-load-rom', 'qa-pause', 'qa-reset']) {
      expect(w.find(tid(id)).attributes('disabled'), id).toBeDefined()
    }
    expect(w.find(tid('qa-launch')).attributes('title')).toContain('No bundled')
    expect(w.find(tid('qa-load-rom')).attributes('title')).toContain('not connected')
    expect(w.find(tid('qa-pause')).attributes('title')).toBe('Emulator not connected')
  })

  it('Load ROM launches with the picked ROM when disconnected', async () => {
    const { calls } = mockFetch({
      'GET /api/emulators': emus,
      'GET /api/browse': {
        path: '/r',
        parent: '/',
        entries: [{ name: 'a.nds', path: '/r/a.nds', dir: false, size: 1 }],
      },
      'POST /api/emulator/launch': () => statusFixture(),
    })
    useStatusStore().set(statusFixture({ connected: false }))
    const w = mount(TopBar, { attachTo: document.body })
    await flushPromises()

    await w.find(tid('qa-load-rom')).trigger('click')
    await flushPromises()
    expect(w.find(tid('file-picker')).exists()).toBe(true)
    await w.find('[data-name="a.nds"]').trigger('click')
    await flushPromises()
    expect(w.find(tid('file-picker')).exists()).toBe(false)
    expect(posts(calls)).toEqual(['/api/emulator/launch'])
    expect(calls.at(-1)!.body).toEqual({ name: 'melonDS', rom: '/r/a.nds' })
    expect(JSON.parse(localStorage.getItem('rtcvish.connect')!).rom).toBe('/r/a.nds')
    w.unmount()
  })

  it('Load ROM loads into the connected emulator; Pause/Resume and Reset follow the game', async () => {
    const game = statusFixture().game!
    const { calls } = mockFetch({
      'GET /api/emulators': emus,
      'GET /api/browse': {
        path: '/r',
        parent: '/',
        entries: [{ name: 'b.nds', path: '/r/b.nds', dir: false, size: 1 }],
      },
      'POST /api/emulator/rom': () => ({ ...game, romPath: '/r/b.nds' }),
      'POST /api/emulator/pause': () => ({ ...game, state: 'paused' }),
      'POST /api/emulator/resume': () => game,
      'POST /api/emulator/reset': () => game,
    })
    useStatusStore().set(statusFixture())
    const w = mount(TopBar)
    await flushPromises()

    await w.find(tid('qa-load-rom')).trigger('click')
    await flushPromises()
    await w.find('[data-name="b.nds"]').trigger('click')
    await flushPromises()
    expect(calls.find((c) => c.path === '/api/emulator/rom')!.body).toEqual({ path: '/r/b.nds' })

    const pause = w.find(tid('qa-pause'))
    expect(pause.text()).toBe('Pause')
    await pause.trigger('click')
    await flushPromises()
    expect(pause.text()).toBe('Resume')
    await pause.trigger('click')
    await flushPromises()
    expect(pause.text()).toBe('Pause')
    await w.find(tid('qa-reset')).trigger('click')
    await flushPromises()
    expect(posts(calls)).toEqual([
      '/api/emulator/rom',
      '/api/emulator/pause',
      '/api/emulator/resume',
      '/api/emulator/reset',
    ])
    expect(w.find(tid('qa-launch')).attributes('disabled')).toBeDefined()

    useStatusStore().set(statusFixture({ game: { ...game, state: 'noRom' } }))
    await flushPromises()
    expect(pause.attributes('disabled')).toBeDefined()
    expect(pause.attributes('title')).toBe('No ROM loaded')
    expect(w.find(tid('qa-reset')).attributes('disabled')).toBeDefined()
  })

  it('has no theme toggle', () => {
    mockFetch({})
    useStatusStore().set(statusFixture())
    expect(mount(TopBar).find(tid('theme-toggle')).exists()).toBe(false)
  })
})

describe('theme setting', () => {
  it('is in the Settings panel and persists', async () => {
    mockFetch({})
    const w = mount(SettingsPanel)
    const ui = useUiStore()
    expect(ui.theme).toBe('system')
    expect(w.find(tid('theme-system')).attributes('aria-pressed')).toBe('true')
    await w.find(tid('theme-dark')).trigger('click')
    await flushPromises()
    expect(ui.theme).toBe('dark')
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark')
    expect(localStorage.getItem('rtcvish.theme')).toBe('dark')
    expect(w.find(tid('theme-dark')).attributes('aria-pressed')).toBe('true')
    await w.find(tid('theme-system')).trigger('click')
    await flushPromises()
    expect(document.documentElement.hasAttribute('data-theme')).toBe(false)
  })
})

describe('layout', () => {
  it('starts on Harvester without an Engine tab', () => {
    expect(useUiStore().panel).toBe('harvester')
    const w = mount(SideBar)
    expect(w.findAll('nav button').map((b) => b.text())).toEqual([
      'Harvester',
      'Blast Editor',
      'Memory',
      'Settings',
    ])
    expect(w.find(tid('nav-engine')).exists()).toBe(false)
  })

  it('collapses the Engine section and remembers it', async () => {
    mockFetch({})
    useSettingsStore().settings = settingsFixture()
    const w = mount(EngineSection)
    const toggle = w.find(tid('engine-toggle'))
    const body = () => w.find(tid('engine-section-body'))
    expect(toggle.attributes('aria-expanded')).toBe('true')
    expect(body().attributes('style') ?? '').not.toContain('display: none')
    expect(w.find(tid('engine-config')).exists()).toBe(true)

    await toggle.trigger('click')
    expect(toggle.attributes('aria-expanded')).toBe('false')
    expect(body().attributes('style')).toContain('display: none')
    expect(w.find(tid('engine-summary')).text()).toContain('intensity')
    await flushPromises()
    expect(localStorage.getItem('rtcvish.engineOpen')).toBe('false')

    setActivePinia(createPinia())
    useSettingsStore().settings = settingsFixture()
    const w2 = mount(EngineSection)
    expect(w2.find(tid('engine-toggle')).attributes('aria-expanded')).toBe('false')
  })
})

describe('Harvester lists scroll on append', () => {
  const sk = (key: string): StashKey => ({
    key,
    parentKey: 's',
    alias: '',
    note: '',
    game: { title: 'g', code: '', romPath: '/r/g.nds', system: 'nds' },
    selectedDomains: [],
    unitCount: 1,
    createdAt: '2026-01-01T00:00:00Z',
  })

  /** jsdom has no layout: fake a scrollable element. */
  function fakeScroll(el: HTMLElement) {
    let top = 0
    Object.defineProperty(el, 'scrollHeight', { configurable: true, value: 500 })
    Object.defineProperty(el, 'scrollTop', {
      configurable: true,
      get: () => top,
      set: (v: number) => (top = v),
    })
  }

  it('detects appends only', () => {
    expect(appended(['a'], ['a', 'b'])).toBe(true)
    expect(appended([], ['a'])).toBe(true)
    expect(appended(['a', 'b'], ['a'])).toBe(false)
    expect(appended(['a', 'b'], ['b', 'a'])).toBe(false)
    expect(appended(['a'], ['b', 'c'])).toBe(false)
    expect(appended(['a'], ['a'])).toBe(false)
  })

  it('scrolls the stash list to the bottom when an entry is added', async () => {
    mockFetch({})
    useStatusStore().set(statusFixture())
    const stash = useStashStore()
    stash.keys = [sk('a'), sk('b')]
    const w = mount(StashHistory)
    const list = w.find(tid('stash-list')).element as HTMLElement
    fakeScroll(list)
    // Bounded: the list is positioned inside a filler, so it never grows the box.
    expect(list.classList).toContain('absolute')
    expect(list.classList).toContain('overflow-y-auto')

    stash.keys = [...stash.keys, sk('c')]
    await flushPromises()
    expect(list.scrollTop).toBe(500)

    // The user scrolls up; renames and removals do not jump.
    list.scrollTop = 100
    stash.keys = stash.keys.map((k) => (k.key === 'a' ? { ...k, alias: 'x' } : k))
    await flushPromises()
    expect(list.scrollTop).toBe(100)
    stash.keys = stash.keys.slice(1)
    await flushPromises()
    expect(list.scrollTop).toBe(100)

    stash.keys = [...stash.keys, sk('d')]
    await flushPromises()
    expect(list.scrollTop).toBe(500)
  })

  it('scrolls the stockpile table on append but not on reload', async () => {
    mockFetch({})
    useStatusStore().set(statusFixture())
    const sp = useStockpileStore()
    sp.keys = [sk('a')]
    const w = mount(StockpileManager)
    const box = w.find(tid('stockpile-scroll')).element as HTMLElement
    fakeScroll(box)

    sp.keys = [sk('x'), sk('y')]
    await flushPromises()
    expect(box.scrollTop).toBe(0)
    sp.keys = [...sp.keys, sk('z')]
    await flushPromises()
    expect(box.scrollTop).toBe(500)
  })
})
