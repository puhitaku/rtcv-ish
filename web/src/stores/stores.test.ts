import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { ApiError, errorMessage } from '@/api/client'
import { errorResponse, mockFetch } from '@/test/fetch'
import { settingsFixture } from '@/test/fixtures'
import { act, useLogStore } from './log'
import { deepMerge, useSettingsStore } from './settings'
import { useStockpileStore } from './stockpile'
import { useSavestatesStore } from './savestates'
import { useEditorStore } from './editor'
import { newUnit } from '@/lib/layer'
import { mulberry32 } from '@/lib/prng'

beforeEach(() => setActivePinia(createPinia()))
afterEach(() => vi.unstubAllGlobals())

describe('settings store', () => {
  it('deep-merges patches', () => {
    const s = settingsFixture()
    const m = deepMerge(s, { nightmare: { ranges: { '1': { min: '1', max: '2' } } } })
    expect(m.nightmare.ranges['1']).toEqual({ min: '1', max: '2' })
    expect(m.nightmare.ranges['2']).toEqual(s.nightmare.ranges['2'])
    expect(m.nightmare.algo).toBe('random')
  })

  it('applies a PATCH optimistically and takes the server result', async () => {
    let resolve!: (r: Response) => void
    const { calls } = mockFetch({
      'PATCH /api/settings': () =>
        new Promise<Response>((r) => {
          resolve = r
        }),
    })
    const store = useSettingsStore()
    store.settings = settingsFixture()
    const p = store.patch({ engine: 'vector' })
    await new Promise((r) => setTimeout(r, 0))
    expect(store.settings.engine).toBe('vector')
    resolve(
      new Response(JSON.stringify({ ...settingsFixture(), engine: 'vector', intensity: 9 }), {
        headers: { 'Content-Type': 'application/json' },
      }),
    )
    await p
    expect(store.settings.intensity).toBe(9)
    expect(calls[0]!.body).toEqual({ engine: 'vector' })
  })

  it('reverts on error', async () => {
    mockFetch({
      'PATCH /api/settings': () => errorResponse(400, 'INVALID_ARGUMENT', 'bad alignment'),
    })
    const store = useSettingsStore()
    store.settings = settingsFixture()
    await expect(store.patch({ alignment: 7 })).rejects.toMatchObject({
      code: 'INVALID_ARGUMENT',
      status: 400,
      message: 'bad alignment',
    })
    expect(store.settings.alignment).toBe(0)
  })
})

describe('errors', () => {
  it('turns API error bodies into messages', () => {
    expect(errorMessage(new ApiError('no ROM', 'NO_ROM', 409))).toBe('no ROM')
    expect(errorMessage(new ApiError('nope', 'NOT_IMPLEMENTED', 501))).toBe(
      'nope (not implemented yet)',
    )
    expect(errorMessage(new Error('x'))).toBe('x')
  })

  it('act logs errors and shows a toast', async () => {
    mockFetch({
      'POST /api/blast': () => errorResponse(503, 'EMULATOR_DISCONNECTED', 'emulator disconnected'),
    })
    vi.useFakeTimers()
    const { useUnitsStore } = await import('./units')
    const r = await act(() => useUnitsStore().blast())
    expect(r).toBeUndefined()
    const log = useLogStore()
    expect(log.entries.at(-1)).toMatchObject({ level: 'error', msg: 'emulator disconnected' })
    expect(log.toasts).toHaveLength(1)
    vi.advanceTimersByTime(5000)
    expect(log.toasts).toHaveLength(0)
    vi.useRealTimers()
  })

  it('reports network failures', async () => {
    vi.stubGlobal('fetch', () => Promise.reject(new TypeError('Failed to fetch')))
    await expect(useSettingsStore().fetch()).rejects.toMatchObject({ code: 'NETWORK' })
  })
})

describe('stockpile store', () => {
  it('moves the selection and posts the new order', async () => {
    const { calls } = mockFetch({ 'POST /api/stockpile/reorder': () => [] })
    const sp = useStockpileStore()
    const k = (key: string) => ({
      key,
      parentKey: '',
      alias: key,
      note: '',
      game: { title: '', code: '', romPath: '', system: '' },
      selectedDomains: [],
      unitCount: 0,
      createdAt: '',
    })
    sp.keys = ['a', 'b', 'c', 'd'].map(k)
    await sp.move(['b', 'c'], -1)
    expect(calls[0]!.body).toEqual({ keys: ['b', 'c', 'a', 'd'] })
    sp.keys = ['a', 'b', 'c', 'd'].map(k)
    await sp.move(['a', 'c'], 1)
    expect(calls[1]!.body).toEqual({ keys: ['b', 'a', 'd', 'c'] })
  })
})

describe('savestates store', () => {
  it('starts with 50 empty slots and replaces a saved slot', async () => {
    mockFetch({ 'POST /api/savestates/3': () => ({ slot: 3, key: 'k', label: 'x' }) })
    const s = useSavestatesStore()
    expect(s.slots).toHaveLength(50)
    await s.save(3)
    expect(s.get(3)).toEqual({ slot: 3, key: 'k', label: 'x' })
  })
})

describe('editor store', () => {
  it('loads a stash layer, edits locally and PUTs it back', async () => {
    const units = Array.from({ length: 4 }, (_, i) => newUnit('RAM', i))
    const { calls } = mockFetch({
      'GET /api/stash/k1/layer': () => ({ note: '', units }),
      'PUT /api/stash/k1/layer': (c) => c.body as object,
    })
    const ed = useEditorStore()
    ed.setRand(mulberry32(7))
    await ed.open({ kind: 'stash', key: 'k1' })
    expect(ed.layer.units).toHaveLength(4)
    ed.disable50()
    expect(ed.dirty).toBe(true)
    expect(ed.layer.units.filter((u) => !u.enabled)).toHaveLength(2)
    await ed.save()
    expect(ed.dirty).toBe(false)
    const put = calls.find((c) => c.method === 'PUT')!
    expect((put.body as { units: unknown[] }).units).toHaveLength(4)
  })

  it('bakes a unit larger than one memory read in 64 KiB chunks', async () => {
    const size = 0x10000 * 2 + 0x100
    const byteAt = (a: number) => (a * 13) & 0xff
    const { calls } = mockFetch({
      'GET /api/memory/RAM': (c) => {
        const address = Number(c.query.address)
        const n = Number(c.query.size)
        if (n > 0x10000) return errorResponse(400, 'INVALID_ARGUMENT', 'size in 1..65536')
        let data = ''
        for (let i = 0; i < n; i++)
          data += byteAt(address + i)
            .toString(16)
            .padStart(2, '0')
        return { domain: 'RAM', address, data }
      },
    })
    const ed = useEditorStore()
    ed.replace({ note: '', units: [{ ...newUnit('RAM', 0x40), source: 'store', precision: size }] })
    await ed.bake()
    const reads = calls.map((c) => [Number(c.query.address), Number(c.query.size)])
    expect(reads).toEqual([
      [0x40, 0x10000],
      [0x40 + 0x10000, 0x10000],
      [0x40 + 0x20000, 0x100],
    ])
    const u = ed.layer.units[0]!
    expect(u).toMatchObject({ source: 'value', precision: size, address: 0x40 })
    expect(u.value).toHaveLength(size * 2)
    for (const off of [0, 0xffff, 0x10000, size - 1]) {
      expect(u.value.slice(off * 2, off * 2 + 2)).toBe(
        byteAt(0x40 + off)
          .toString(16)
          .padStart(2, '0'),
      )
    }
  })

  it('refuses to break down a huge unit', () => {
    const ed = useEditorStore()
    const l = {
      note: '',
      units: [{ ...newUnit('RAM', 0), source: 'store' as const, precision: 16 << 20 }],
    }
    ed.replace(l)
    expect(() => ed.breakDown()).toThrow(/limit/)
    expect(ed.layer.units).toHaveLength(1)
  })
})
