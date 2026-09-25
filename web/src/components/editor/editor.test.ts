import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import BlastEditorPanel from '@/components/panels/BlastEditorPanel.vue'
import EngineConfig from '@/components/engine/EngineConfig.vue'
import { COLUMNS } from './columns'
import { mockFetch } from '@/test/fetch'
import { settingsFixture } from '@/test/fixtures'
import { FIELD_HELP } from '@/lib/fieldHelp'
import { newUnit } from '@/lib/layer'
import { BASES_KEY, DEFAULT_BASES, setBase, type NumField } from '@/lib/numBase'
import { useEditorStore } from '@/stores/editor'
import { deepMerge, useSettingsStore } from '@/stores/settings'
import type { Settings, Unit } from '@/api/types'

const tid = (id: string) => `[data-testid="${id}"]`

beforeEach(() => {
  setActivePinia(createPinia())
  localStorage.clear()
  for (const [k, b] of Object.entries(DEFAULT_BASES)) setBase(k as NumField, b)
  document.body.innerHTML = ''
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

function unit(p: Partial<Unit> = {}): Unit {
  return { ...newUnit('RAM', 0x1234), ...p }
}

function mountEditor(units: Unit[]) {
  mockFetch({})
  const ed = useEditorStore()
  ed.target = { kind: 'new' }
  ed.replace({ note: '', units }, false)
  ed.selection = [0]
  return { ed, w: mount(BlastEditorPanel, { attachTo: document.body }) }
}

describe('field help', () => {
  it('has a text for every column', () => {
    for (const c of COLUMNS) expect(FIELD_HELP[c.id], c.id).toBeTruthy()
  })

  it('shows a tooltip panel on hover after a delay and on focus', async () => {
    vi.useFakeTimers()
    const { w } = mountEditor([unit()])
    const label = w.findAll('[data-help-tip]').find((x) => x.text() === 'Lifetime')!
    await label.trigger('mouseenter')
    expect(document.querySelector(tid('help-tip'))).toBeNull()
    vi.advanceTimersByTime(400)
    await flushPromises()
    const tip = document.querySelector(tid('help-tip'))!
    expect(tip.textContent).toContain('0 means forever')
    expect(label.attributes('aria-describedby')).toBe(tip.id)
    await label.trigger('mouseleave')
    expect(document.querySelector(tid('help-tip'))).toBeNull()

    // Keyboard: the label is focusable and shows its help on focus.
    expect(label.attributes('tabindex')).toBe('0')
    await label.trigger('focusin')
    vi.advanceTimersByTime(400)
    await flushPromises()
    expect(document.querySelector(tid('help-tip'))?.textContent).toContain('0 means forever')
    await label.trigger('keydown', { key: 'Escape' })
    expect(document.querySelector(tid('help-tip'))).toBeNull()
    w.unmount()
  })

  it('labels checkboxes and table headers', () => {
    const { w } = mountEditor([unit()])
    const flag = w.find(tid('be-prop-generatedUsingValueList')).element.closest('[data-help-tip]')
    expect(flag).not.toBeNull()
    expect(flag!.getAttribute('tabindex')).toBeNull()
    expect(w.find(tid('be-th-executeFrame')).find('[data-help-tip]').exists()).toBe(true)
    w.unmount()
  })
})

describe('hex/decimal toggle', () => {
  it('switches display and parsing per field and the table follows', async () => {
    const { ed, w } = mountEditor([unit({ lifetime: 16, address: 0x20 })])
    const life = w.find(tid('be-prop-lifetime'))
    expect((life.element as HTMLInputElement).value).toBe('16')
    expect(w.find(tid('be-cell-lifetime')).text()).toBe('16')
    expect(w.find(tid('be-cell-address')).text()).toBe('00000020')

    await w.find(tid('be-base-lifetime')).trigger('click')
    expect(w.find(tid('be-base-lifetime')).text()).toBe('hex')
    expect((life.element as HTMLInputElement).value).toBe('10')
    expect(w.find(tid('be-cell-lifetime')).text()).toBe('10')
    expect(JSON.parse(localStorage.getItem(BASES_KEY)!).lifetime).toBe('hex')

    await life.setValue('0x1F')
    await life.trigger('change')
    expect(ed.layer.units[0]!.lifetime).toBe(31)
    await life.setValue('ff')
    await life.trigger('change')
    expect(ed.layer.units[0]!.lifetime).toBe(255)

    await w.find(tid('be-base-address')).trigger('click')
    expect(w.find(tid('be-cell-address')).text()).toBe('32')
    const addr = w.find(tid('be-prop-address'))
    await addr.setValue('100')
    await addr.trigger('change')
    expect(ed.layer.units[0]!.address).toBe(100)

    await w.find(tid('be-base-tilt')).trigger('click')
    const tilt = w.find(tid('be-prop-tilt'))
    await tilt.setValue('-0x10')
    await tilt.trigger('change')
    expect(ed.layer.units[0]!.tilt).toBe('-16')
    expect((tilt.element as HTMLInputElement).value).toBe('-10')
    w.unmount()
  })
})

describe('column width caps', () => {
  it('caps cells with an ellipsis and keeps the full text in the title', () => {
    const long = 'ab'.repeat(64)
    const note = 'a very long note that goes on and on and on'
    const { w } = mountEditor([unit({ precision: 64, value: long, note })])
    const v = w.find(tid('be-cell-value'))
    expect((v.element as HTMLElement).style.maxWidth).toBe('16ch')
    expect(v.classes()).toContain('be-cell')
    expect(v.attributes('title')).toBe(long.toUpperCase())
    const n = w.find(tid('be-cell-note'))
    expect((n.element as HTMLElement).style.maxWidth).toBe('24ch')
    expect(n.attributes('title')).toBe(note)
    for (const c of COLUMNS) expect(c.maxCh, c.id).toBeLessThanOrEqual(24)
    w.unmount()
  })
})

describe('address range', () => {
  it('enables, validates and PATCHes hex bounds', async () => {
    let server: Settings = settingsFixture()
    const { calls } = mockFetch({
      'PATCH /api/settings': (c) => (server = deepMerge(server, c.body)),
    })
    const store = useSettingsStore()
    store.settings = settingsFixture()
    const w = mount(EngineConfig)
    const start = w.find(tid('address-range-start'))
    expect((start.element as HTMLInputElement).disabled).toBe(true)
    expect(w.find(tid('address-range-hint')).text()).toContain('off')

    await w.find(tid('address-range-enabled')).setValue(true)
    await flushPromises()
    expect(calls.at(-1)!.body).toEqual({ addressRange: { enabled: true } })
    expect((start.element as HTMLInputElement).disabled).toBe(false)

    await start.setValue('0x1000')
    await start.trigger('change')
    await flushPromises()
    expect(calls.at(-1)!.body).toEqual({ addressRange: { start: 0x1000, end: 0x400000 } })
    expect(w.find(tid('address-range-hint')).text()).toContain('0x3FF000 bytes')

    const n = calls.length
    await start.setValue('500000')
    await start.trigger('change')
    expect(calls).toHaveLength(n)
    expect(start.classes()).toContain('invalid')
    expect(w.find(tid('address-range-hint')).text()).toBe('start must be below end')
  })
})

describe('toolbar layout', () => {
  /** Button labels of a row, with "|" for each separator. */
  function row(w: ReturnType<typeof mountEditor>['w'], id: string) {
    return [...w.find(tid(id)).element.querySelectorAll('button, [data-testid="be-sep"]')].map(
      (e) => (e.tagName === 'BUTTON' ? e.textContent!.trim() : '|'),
    )
  }

  it('groups layer actions and unit editing', () => {
    const { w } = mountEditor([unit()])
    expect(row(w, 'be-row-layer')).toEqual([
      'Apply Corruption',
      'Load + Corrupt',
      'To Stash',
      'To Stockpile',
      '|',
      'New',
      'Load .bl',
      'Save .bl',
      'Revert',
      'Save',
    ])
    expect(row(w, 'be-row-units')).toEqual([
      'Add row',
      'Duplicate',
      'Break down',
      'Remove selected',
      'Remove disabled',
      '|',
      'Enable all',
      '|',
      'Disable all',
      'Disable 50%',
      'Invert Disabled',
      '|',
      'Bake to VALUE',
      'Sanitize duplicates',
      '|',
      '▲',
      '▼',
      '|',
      'Open in Memory',
    ])
    expect(w.find(tid('be-load-corrupt')).attributes('title')).toBe(
      'Only for a stash or stockpile item',
    )
  })
})
