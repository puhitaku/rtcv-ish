import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { defineComponent, h } from 'vue'
import { SHORTCUTS, useShortcuts } from './shortcuts'
import BlastTools from '@/components/harvester/BlastTools.vue'
import { mockFetch } from '@/test/fetch'
import { settingsFixture, statusFixture } from '@/test/fixtures'
import { useDialogStore } from '@/stores/dialog'
import { useLogStore } from '@/stores/log'
import { deepMerge, useSettingsStore } from '@/stores/settings'
import { useSavestatesStore } from '@/stores/savestates'
import { useStatusStore } from '@/stores/status'
import { useUiStore } from '@/stores/ui'
import type { Settings } from '@/api/types'

const Host = defineComponent({
  props: { tools: Boolean },
  setup(props) {
    useShortcuts()
    return () =>
      h('div', [
        props.tools ? h(BlastTools) : null,
        h('input', { id: 'field' }),
        h('button', { id: 'btn' }, 'x'),
      ])
  },
})

let w: VueWrapper | undefined

function press(key: string, target: EventTarget = document.body, init: KeyboardEventInit = {}) {
  target.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, ...init }))
  return flushPromises()
}

function setup(backups = 1) {
  let server: Settings = settingsFixture()
  const fetch = mockFetch({
    'POST /api/blast': () => ({ units: [] }),
    'PATCH /api/settings': (c) => (server = deepMerge(server, c.body)),
    'POST /api/protection/back': () => undefined,
    'POST /api/protection/last': () => undefined,
    'POST /api/protection/backup': () => undefined,
  })
  useStatusStore().set(statusFixture({ protectionBackups: backups }))
  useSettingsStore().settings = settingsFixture()
  w = mount(Host, { attachTo: document.body })
  return fetch
}

const paths = (calls: { method: string; path: string }[]) =>
  calls.map((c) => `${c.method} ${c.path}`)

beforeEach(() => setActivePinia(createPinia()))
afterEach(() => {
  w?.unmount()
  w = undefined
  vi.unstubAllGlobals()
})

describe('useShortcuts', () => {
  it('runs the top bar actions with the same log lines', async () => {
    const { calls } = setup()
    const log = useLogStore()

    await press('m')
    await press('a')
    await press('p')
    await press('b')
    expect(log.entries.at(-1)?.msg).toBe('game protection: back')
    await press('l')
    expect(log.entries.at(-1)?.msg).toBe('game protection: loaded last backup')
    await press('n')
    expect(log.entries.at(-1)?.msg).toBe('game protection: backup taken')

    expect(paths(calls)).toEqual([
      'POST /api/blast',
      'PATCH /api/settings',
      'PATCH /api/settings',
      'POST /api/protection/back',
      'POST /api/protection/last',
      'POST /api/protection/backup',
    ])
    expect(calls[1]!.body).toEqual({ autoCorrupt: true })
    expect(calls[2]!.body).toEqual({ gameProtection: { enabled: true } })

    // The toggle reads the current state.
    await press('a')
    expect(calls.at(-1)!.body).toEqual({ autoCorrupt: false })
  })

  it('ignores keys while typing, with modifiers, or with a dialog open', async () => {
    const { calls } = setup()
    const input = document.getElementById('field') as HTMLInputElement
    input.focus()
    await press('m', input)
    input.blur()

    await press('m', document.body, { ctrlKey: true })
    await press('n', document.body, { metaKey: true })

    const dlg = useDialogStore()
    void dlg.ask('name?')
    await press('m')
    dlg.close(null)

    expect(calls).toHaveLength(0)

    await press('m', document.getElementById('btn')!)
    expect(paths(calls)).toEqual(['POST /api/blast'])
  })

  it('respects the disabled states', async () => {
    const { calls } = setup(0)
    // No backups: Back and Last are disabled, Now is not.
    await press('b')
    await press('l')
    expect(calls).toHaveLength(0)
    await press('n')
    expect(paths(calls)).toEqual(['POST /api/protection/backup'])

    // No ROM: everything needing the game is disabled.
    const st = useStatusStore()
    st.set(statusFixture({ protectionBackups: 3, game: { ...st.game!, state: 'noRom' } }))
    await press('m')
    await press('b')
    await press('n')
    expect(calls).toHaveLength(1)

    // Settings not loaded: the toggles are disabled.
    useSettingsStore().settings = null
    await press('a')
    await press('p')
    expect(calls).toHaveLength(1)
  })

  it('skips emulator actions while busy or unresponsive', async () => {
    const { calls } = setup()
    const st = useStatusStore()
    st.set(statusFixture({ protectionBackups: 1, busy: { operation: 'loadRom', sinceMs: 0 } }))
    for (const k of ['m', 'b', 'l', 'n']) await press(k)
    st.set(statusFixture({ protectionBackups: 1, unresponsive: true }))
    for (const k of ['m', 'b', 'l', 'n']) await press(k)
    expect(paths(calls)).toEqual([])
  })
})

describe('tab-scoped shortcuts', () => {
  function setupHarvester() {
    const fetch = mockFetch({
      'POST /api/stash/corrupt': () => ({
        key: 'k1',
        parentKey: 's1',
        alias: '',
        note: '',
        game: { title: 'g', code: '', romPath: '/r/g.nds', system: 'nds' },
        selectedDomains: [],
        unitCount: 3,
        createdAt: '2026-01-01T00:00:00Z',
      }),
    })
    useStatusStore().set(statusFixture())
    useSettingsStore().settings = settingsFixture()
    w = mount(Host, { props: { tools: true }, attachTo: document.body })
    return fetch
  }

  it('lists c, f and r with their tabs', () => {
    const scoped = SHORTCUTS.filter((s) => s.tab).map((s) => `${s.key}:${s.tab}:${s.action}`)
    expect(scoped).toEqual(['c:harvester:corrupt', 'f:memory:freeze', 'r:memory:refresh'])
    expect(new Set(SHORTCUTS.map((s) => s.key)).size).toBe(SHORTCUTS.length)
  })

  it('c corrupts only on the Harvester tab, respecting the disabled reason', async () => {
    const { calls } = setupHarvester()
    const ui = useUiStore()
    const log = useLogStore()
    const main = w!.find('[data-testid="gh-main"]')

    // No slot selected: the Corrupt button is disabled and so is c.
    expect(main.attributes('title')).toBe('Select a savestate slot')
    await press('c')
    expect(calls).toHaveLength(0)

    useSavestatesStore().slots = [{ slot: 1, key: 's1', label: '' }]
    ui.selectedSlot = 1
    await flushPromises()
    expect(main.attributes('title')).toBe('Corrupt (c)')

    // Another tab: ignored.
    ui.panel = 'memory'
    await press('c')
    expect(calls).toHaveLength(0)

    // While typing: ignored.
    ui.panel = 'harvester'
    const input = document.getElementById('field') as HTMLInputElement
    input.focus()
    await press('c', input)
    input.blur()
    expect(calls).toHaveLength(0)

    await press('c')
    expect(paths(calls)).toEqual(['POST /api/stash/corrupt'])
    expect(calls[0]!.body).toEqual({ slot: 1, loadBefore: true })
    expect(log.entries.at(-1)?.msg).toBe('corrupted: k1 (3 units)')
  })

  it('does nothing when the tab component is not mounted', async () => {
    const { calls } = setup()
    useUiStore().panel = 'harvester'
    await press('c')
    await press('f')
    await press('r')
    expect(calls).toHaveLength(0)
  })
})
