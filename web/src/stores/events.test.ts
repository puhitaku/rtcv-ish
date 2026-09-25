import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { mockFetch } from '@/test/fetch'
import { settingsFixture, statusFixture } from '@/test/fixtures'
import { startEvents, type EventSourceLike } from './events'
import { useLogStore } from './log'
import { useSettingsStore } from './settings'
import { useStashStore } from './stash'
import { useStatusStore } from './status'
import { useStockpileStore } from './stockpile'
import { useDomainsStore } from './domains'

class FakeEventSource implements EventSourceLike {
  static last: FakeEventSource | null = null
  onopen: ((ev: Event) => unknown) | null = null
  onerror: ((ev: Event) => unknown) | null = null
  listeners = new Map<string, ((ev: MessageEvent) => unknown)[]>()
  closed = false

  constructor(public url: string) {
    FakeEventSource.last = this
  }

  addEventListener(type: string, l: (ev: MessageEvent) => unknown) {
    this.listeners.set(type, [...(this.listeners.get(type) ?? []), l])
  }

  close() {
    this.closed = true
  }

  emit(type: string, data: unknown) {
    const ev = new MessageEvent(type, { data: JSON.stringify(data) })
    for (const l of this.listeners.get(type) ?? []) l(ev)
  }
}

const flush = () => new Promise((r) => setTimeout(r, 0))

function stashKey(key: string) {
  return {
    key,
    parentKey: 'p',
    alias: key,
    note: '',
    game: { title: 'G', code: 'C', romPath: '/r', system: 'nds' },
    selectedDomains: ['RAM'],
    unitCount: 1,
    createdAt: '2026-01-01T00:00:00Z',
  }
}

describe('SSE event handling', () => {
  let fetchMock: ReturnType<typeof mockFetch>
  let es: FakeEventSource
  let stop: () => void

  beforeEach(() => {
    setActivePinia(createPinia())
    fetchMock = mockFetch({
      'GET /api/stash': () => [stashKey('k1'), stashKey('k2')],
      'GET /api/stockpile': () => [stashKey('s1')],
      'GET /api/settings': () => ({ ...settingsFixture(), intensity: 42 }),
      'GET /api/domains': () => [],
    })
    stop = startEvents((url) => new FakeEventSource(url))
    es = FakeEventSource.last!
  })

  afterEach(() => {
    stop()
    vi.unstubAllGlobals()
  })

  it('opens /api/events and closes on stop', () => {
    expect(es.url).toBe('/api/events')
    stop()
    expect(es.closed).toBe(true)
  })

  it('updates status and frame directly without fetching', async () => {
    const st = useStatusStore()
    es.emit('status', statusFixture())
    expect(st.connected).toBe(true)
    expect(st.game?.title).toBe('GAME')
    expect(st.frame).toBe(10)
    es.emit('frame', { frame: 1234 })
    expect(st.frame).toBe(1234)
    await flush()
    expect(fetchMock.calls).toHaveLength(0)
  })

  it('refetches the resource named by a changed event', async () => {
    es.emit('stash', {})
    await flush()
    expect(fetchMock.calls.map((c) => `${c.method} ${c.path}`)).toEqual(['GET /api/stash'])
    expect(useStashStore().keys.map((k) => k.key)).toEqual(['k1', 'k2'])

    es.emit('stockpile', {})
    es.emit('settings', {})
    await flush()
    expect(useStockpileStore().keys).toHaveLength(1)
    expect(useSettingsStore().settings?.intensity).toBe(42)
  })

  it('logs log and blast events', () => {
    es.emit('log', { level: 'warn', msg: 'hello' })
    es.emit('blast', { count: 3, engine: 'nightmare', elapsedMs: 1.5 })
    const entries = useLogStore().entries
    expect(entries.map((e) => [e.level, e.msg])).toEqual([
      ['warn', 'hello'],
      ['info', 'blast: 3 units (nightmare, 1.5 ms)'],
    ])
  })

  it('refetches domains when the loaded game changes', async () => {
    const domains = useDomainsStore()
    const spy = vi.spyOn(domains, 'fetch')
    es.emit('status', statusFixture())
    es.emit('status', statusFixture())
    await flush()
    expect(spy).not.toHaveBeenCalled()
    es.emit('status', statusFixture({ game: { ...statusFixture().game!, romPath: '/other.nds' } }))
    await flush()
    expect(fetchMock.calls.some((c) => c.path === '/api/domains')).toBe(true)
  })

  it('tracks the stream state and refetches everything on open', async () => {
    const st = useStatusStore()
    es.onopen?.(new Event('open'))
    expect(st.live).toBe(true)
    await flush()
    const paths = new Set(fetchMock.calls.map((c) => c.path))
    for (const p of ['/api/status', '/api/settings', '/api/stash', '/api/savestates', '/api/lists'])
      expect(paths.has(p)).toBe(true)
    es.onerror?.(new Event('error'))
    expect(st.live).toBe(false)
  })

  it('ignores malformed data', async () => {
    const l = es.listeners.get('status')![0]!
    l(new MessageEvent('status', { data: '{not json' }))
    await flush()
    expect(useStatusStore().status).toBeNull()
  })
})
