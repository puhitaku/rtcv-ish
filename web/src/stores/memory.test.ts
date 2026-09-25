import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { mockFetch } from '@/test/fetch'
import { settingsFixture, statusFixture } from '@/test/fixtures'
import type { EmuUnit } from '@/api/types'
import { handleEvent } from './events'
import { useLogStore } from './log'
import { freezeLayer, useMemoryStore } from './memory'
import { useSettingsStore } from './settings'
import { useStatusStore } from './status'
import { useUnitsStore } from './units'

function emuUnit(over: Partial<EmuUnit> = {}): EmuUnit {
  return {
    id: 1,
    domain: 'RAM',
    address: 0x10,
    size: 2,
    value: 'aabb',
    tilt: 0,
    delay: 0,
    lifetime: 0,
    loop: false,
    loopDelay: 0,
    mode: 'hard',
    ...over,
  }
}

beforeEach(() => {
  setActivePinia(createPinia())
  useStatusStore().set(statusFixture())
  useSettingsStore().settings = settingsFixture()
})
afterEach(() => vi.unstubAllGlobals())

describe('memory store frozen cells', () => {
  it('derives frozen cells from the server units (infinite value units only)', async () => {
    mockFetch({
      'GET /api/blast/units': [
        emuUnit({ id: 1, address: 0x10, size: 2, mode: 'hard' }),
        emuUnit({ id: 2, address: 0x20, lifetime: 5, mode: 'frame' }),
        emuUnit({
          id: 3,
          address: 0x30,
          value: undefined,
          store: { domain: 'RAM', address: 0, continuous: true },
          mode: 'scanline',
        }),
        emuUnit({ id: 4, domain: 'VRAM', address: 0x10 }),
      ],
    })
    const mem = useMemoryStore()
    await useUnitsStore().refresh()
    expect(mem.frozenAt('RAM', 0x10)?.id).toBe(1)
    expect(mem.frozenAt('RAM', 0x11)?.mode).toBe('hard')
    expect(mem.frozenAt('RAM', 0x12)).toBeUndefined()
    expect(mem.frozenAt('RAM', 0x20)).toBeUndefined()
    expect(mem.frozenAt('RAM', 0x30)).toBeUndefined()
    expect(mem.frozenAt('VRAM', 0x10)?.id).toBe(4)
  })

  it('freezes with an infinite unit and shows it once the server has it', async () => {
    let server: EmuUnit[] = []
    const { calls } = mockFetch({
      'GET /api/memory/RAM': { domain: 'RAM', address: 0x40, data: '1234' },
      'POST /api/blast/apply': () => {
        server = [emuUnit({ id: 9, address: 0x40, value: '1234' })]
        return undefined
      },
      'GET /api/blast/units': () => server,
    })
    const mem = useMemoryStore()
    mem.setDomain('RAM')
    await mem.freeze(0x40, 2)
    const apply = calls.find((c) => c.path === '/api/blast/apply')!
    expect(apply.body).toMatchObject({
      backup: false,
      layer: {
        units: [{ domain: 'RAM', address: 0x40, precision: 2, value: '1234', lifetime: 0 }],
      },
    })
    expect(mem.frozenAt('RAM', 0x41)?.id).toBe(9)
  })

  it('unfreezes by removing only the units at the address', async () => {
    let server = [emuUnit({ id: 1, address: 0x10 }), emuUnit({ id: 2, address: 0x20 })]
    const { calls } = mockFetch({
      'GET /api/blast/units': () => server,
      'DELETE /api/blast/units/1': () => {
        server = server.filter((u) => u.id !== 1)
        return undefined
      },
    })
    const mem = useMemoryStore()
    mem.setDomain('RAM')
    await useUnitsStore().refresh()
    await mem.unfreeze(0x11)
    expect(calls.map((c) => `${c.method} ${c.path}`)).toEqual([
      'GET /api/blast/units',
      'DELETE /api/blast/units/1',
      'GET /api/blast/units',
    ])
    expect(mem.frozenAt('RAM', 0x10)).toBeUndefined()
    expect(mem.frozenAt('RAM', 0x20)?.id).toBe(2)
  })

  it('uses the freezeMode setting, falling back to what the emulator supports', () => {
    const mem = useMemoryStore()
    expect(mem.freezeMode).toBe('hard')
    const caps = statusFixture().emulator!.capabilities
    useStatusStore().set(
      statusFixture({
        emulator: { ...statusFixture().emulator!, capabilities: { ...caps, hardUnits: false } },
      }),
    )
    expect(mem.freezeMode).toBe('scanline')
    useSettingsStore().settings = { ...settingsFixture(), freezeMode: 'frame' }
    expect(mem.freezeMode).toBe('frame')
  })

  it('builds lifetime-0 value units in memory order', () => {
    const l = freezeLayer([{ domain: 'RAM', address: 4, size: 2, data: '0102' }])
    expect(l.units[0]).toMatchObject({ lifetime: 0, value: '0102', precision: 2, source: 'value' })
  })
})

describe('units event', () => {
  it('refetches units while a view shows them, so a load unfreezes at once', async () => {
    let server = [emuUnit()]
    const { calls } = mockFetch({ 'GET /api/blast/units': () => server })
    const units = useUnitsStore()
    const mem = useMemoryStore()
    const stop = units.watch()
    await units.refresh()
    expect(mem.frozenAt('RAM', 0x10)).toBeDefined()

    server = []
    await handleEvent('units', JSON.stringify({ reason: 'load', cleared: 1 }))
    expect(mem.frozenAt('RAM', 0x10)).toBeUndefined()
    expect(useLogStore().entries.at(-1)).toMatchObject({
      level: 'info',
      msg: '1 scheduled unit cleared by savestate load',
    })

    await handleEvent('units', JSON.stringify({ reason: 'reset', cleared: 3 }))
    expect(useLogStore().entries.at(-1)!.msg).toBe('3 scheduled units cleared by reset')
    const n = useLogStore().entries.length
    await handleEvent('units', JSON.stringify({ reason: 'load', cleared: 0 }))
    await handleEvent('units', JSON.stringify({ reason: 'apply', cleared: 0 }))
    expect(useLogStore().entries).toHaveLength(n)

    stop()
    const before = calls.length
    await handleEvent('units', JSON.stringify({ reason: 'apply', cleared: 0 }))
    expect(calls).toHaveLength(before)
  })

  it('coalesces a burst of events into at most two fetches', async () => {
    let release!: () => void
    const gate = new Promise<void>((r) => (release = r))
    const { calls } = mockFetch({
      'GET /api/blast/units': async () => {
        await gate
        return []
      },
    })
    const units = useUnitsStore()
    units.watch()
    const evs = Array.from({ length: 5 }, () =>
      handleEvent('units', JSON.stringify({ reason: 'apply', cleared: 0 })),
    )
    release()
    await Promise.all(evs)
    expect(calls.filter((c) => c.path === '/api/blast/units')).toHaveLength(2)
  })

  it('clears units without fetching when disconnected', async () => {
    const { calls } = mockFetch({})
    const units = useUnitsStore()
    units.watch()
    units.units = [emuUnit()]
    useStatusStore().set(statusFixture({ connected: false, emulator: undefined, game: undefined }))
    await handleEvent('units', JSON.stringify({ reason: 'disconnect', cleared: 0 }))
    expect(units.units).toEqual([])
    expect(calls).toHaveLength(0)
  })

  it('refetches units when the game changes', async () => {
    const { calls } = mockFetch({ 'GET /api/blast/units': [], 'GET /api/domains': [] })
    useUnitsStore().watch()
    await handleEvent('status', JSON.stringify(statusFixture()))
    const g = statusFixture().game!
    await handleEvent(
      'status',
      JSON.stringify(statusFixture({ game: { ...g, romPath: '/r/b.nds' } })),
    )
    expect(calls.map((c) => c.path)).toContain('/api/blast/units')
  })
})
