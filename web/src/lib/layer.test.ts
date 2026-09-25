import { describe, expect, it } from 'vitest'
import type { Layer, Unit } from '@/api/types'
import * as L from './layer'
import { mulberry32 } from './prng'

function unit(over: Partial<Unit> = {}): Unit {
  return { ...L.newUnit('RAM', 0), ...over }
}

function layer(units: Unit[]): Layer {
  return { note: 'n', units }
}

describe('disable50', () => {
  it('disables exactly half of the unlocked units, deterministically per seed', () => {
    const l = layer([
      ...Array.from({ length: 10 }, (_, i) => unit({ address: i, enabled: i % 3 !== 0 })),
      unit({ address: 99, locked: true, enabled: false }),
    ])
    const a = L.disable50(l, mulberry32(1))
    const b = L.disable50(l, mulberry32(1))
    expect(a).toEqual(b)
    const unlocked = a.units.filter((u) => !u.locked)
    expect(unlocked.filter((u) => !u.enabled)).toHaveLength(5)
    // Locked units are untouched.
    expect(a.units[10]).toEqual(l.units[10])
    // The input is not mutated.
    expect(l.units[0]!.enabled).toBe(false)
  })

  it('differs between seeds', () => {
    const l = layer(Array.from({ length: 20 }, (_, i) => unit({ address: i })))
    const a = L.disable50(l, mulberry32(1)).units.map((u) => u.enabled)
    const b = L.disable50(l, mulberry32(2)).units.map((u) => u.enabled)
    expect(a).not.toEqual(b)
  })
})

describe('enable/disable helpers', () => {
  const l = layer([
    unit({ enabled: true }),
    unit({ enabled: false }),
    unit({ enabled: false, locked: true }),
  ])

  it('invertDisabled skips locked units', () => {
    expect(L.invertDisabled(l).units.map((u) => u.enabled)).toEqual([false, true, false])
  })

  it('removeDisabled keeps locked units', () => {
    expect(L.removeDisabled(l).units).toHaveLength(2)
  })

  it('enableAll / disableAll skip locked units', () => {
    expect(L.enableAll(l).units.map((u) => u.enabled)).toEqual([true, true, false])
    expect(
      L.disableAll(layer([unit(), unit({ locked: true })])).units.map((u) => u.enabled),
    ).toEqual([false, true])
  })

  it('duplicate appends copies of unlocked units only', () => {
    const d = L.duplicate(l, [0, 2])
    expect(d.units).toHaveLength(4)
    expect(d.units[3]).toEqual(l.units[0])
  })

  it('removeIndices', () => {
    expect(L.removeIndices(l, [0, 2]).units).toEqual([l.units[1]])
  })
})

describe('shift', () => {
  const l = layer([
    unit({ address: 5, sourceAddress: 3, value: 'ff', lifetime: 1, executeFrame: 2 }),
    unit({ address: 10, precision: 2, value: 'ff00' }),
  ])

  it('adds to address, clamping at 0', () => {
    expect(L.shift(l, 'address', 3).units.map((u) => u.address)).toEqual([8, 13])
    expect(L.shift(l, 'address', -7).units.map((u) => u.address)).toEqual([0, 3])
  })

  it('only touches selected indices', () => {
    expect(L.shift(l, 'address', 1, [1]).units.map((u) => u.address)).toEqual([5, 11])
  })

  it('shifts sourceAddress, lifetime and executeFrame', () => {
    const s = L.shift(l, 'sourceAddress', 2, [0]).units[0]!
    expect(s.sourceAddress).toBe(5)
    expect(L.shift(l, 'lifetime', -5, [0]).units[0]!.lifetime).toBe(0)
    expect(L.shift(l, 'executeFrame', 10, [0]).units[0]!.executeFrame).toBe(12)
  })

  it('adds to value as a little-endian number with wrap-around', () => {
    const s = L.shift(l, 'value', 1)
    expect(s.units[0]!.value).toBe('00') // 0xff + 1 wraps on 1 byte
    expect(s.units[1]!.value).toBe('0001') // 0x00ff + 1 = 0x0100
  })

  it('adds to tilt as a decimal', () => {
    expect(L.shift(l, 'tilt', -3, [0]).units[0]!.tilt).toBe('-3')
  })
})

describe('breakDown', () => {
  it('splits VALUE units into the bytes they write', () => {
    const u = unit({ address: 0x10, precision: 2, value: '3412', tilt: '1' })
    const b = L.breakDown(layer([u]))
    expect(b.units.map((x) => [x.address, x.value, x.precision, x.tilt])).toEqual([
      [0x10, '35', 1, '0'],
      [0x11, '12', 1, '0'],
    ])
  })

  it('writes big-endian bytes in memory order', () => {
    const u = unit({ precision: 2, value: '3412', bigEndian: true })
    expect(L.breakDown(layer([u])).units.map((x) => [x.value, x.bigEndian])).toEqual([
      ['12', false],
      ['34', false],
    ])
  })

  it('keeps the tilt of STORE units on the least significant byte', () => {
    const u = unit({
      source: 'store',
      value: '',
      precision: 4,
      sourceDomain: 'RAM',
      sourceAddress: 100,
      tilt: '7',
      bigEndian: true,
    })
    const b = L.breakDown(layer([u]))
    expect(b.units.map((x) => [x.sourceAddress, x.tilt])).toEqual([
      [100, '0'],
      [101, '0'],
      [102, '0'],
      [103, '7'],
    ])
  })

  it('leaves 1-byte and unselected units alone', () => {
    const l = layer([unit(), unit({ precision: 2, value: '0000' })])
    expect(L.breakDown(l, [0]).units).toHaveLength(2)
    expect(L.breakDown(l).units).toHaveLength(3)
  })
})

describe('sanitizeDuplicates', () => {
  it('keeps the last unlocked unit per domain+address and every locked unit', () => {
    const l = layer([
      unit({ address: 1, note: 'a' }),
      unit({ address: 1, note: 'locked', locked: true }),
      unit({ address: 2, note: 'b' }),
      unit({ address: 1, note: 'c' }),
      unit({ address: 1, domain: 'VRAM', note: 'd' }),
    ])
    expect(L.sanitizeDuplicates(l).units.map((u) => u.note)).toEqual(['locked', 'b', 'c', 'd'])
  })
})

describe('bake', () => {
  it('replaces enabled selected units with VALUE units holding memory', async () => {
    const l = layer([
      unit({
        source: 'store',
        value: '',
        sourceDomain: 'RAM',
        address: 4,
        precision: 2,
        note: 'x',
      }),
      unit({ address: 8, enabled: false }),
    ])
    const reads: [string, number, number][] = []
    const b = await L.bake(l, [], async (d, a, s) => {
      reads.push([d, a, s])
      return 'abcd'
    })
    expect(reads).toEqual([['RAM', 4, 2]])
    expect(b.units[0]).toMatchObject({ source: 'value', value: 'abcd', lifetime: 1, note: 'x' })
    expect(b.units[1]).toEqual(l.units[1])
  })
})

describe('applyToIndices', () => {
  it('resizes values when the precision changes', () => {
    const l = layer([unit({ value: 'ff' })])
    expect(L.applyToIndices(l, [0], { precision: 2 }).units[0]!.value).toBe('ff00')
    const store = layer([unit({ source: 'store', value: '' })])
    expect(L.applyToIndices(store, [0], { source: 'value' }).units[0]!.value).toBe('00')
  })
})

describe('parseLayerFile', () => {
  it('fills defaults and rejects non-layers', () => {
    const l = L.parseLayerFile('{"units":[{"domain":"RAM","address":3}]}')
    expect(l.units[0]).toMatchObject({ domain: 'RAM', address: 3, enabled: true, lifetime: 1 })
    expect(() => L.parseLayerFile('{}')).toThrow()
  })
})
