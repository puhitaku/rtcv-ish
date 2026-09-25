// Client-side Blast Editor operations. Semantics follow RTCV and
// internal/corrupt/layer.go. All functions return new objects.
import type { Layer, Unit } from '@/api/types'
import { bigIntToLeHex, hexToBytes, bytesToHex, leHexToBigInt } from './hex'
import { perm, type Rand } from './prng'

export function newUnit(domain = '', address = 0): Unit {
  return {
    enabled: true,
    locked: false,
    bigEndian: false,
    domain,
    address,
    precision: 1,
    source: 'value',
    value: '00',
    sourceDomain: '',
    sourceAddress: 0,
    storeTime: 'immediate',
    storeType: 'once',
    tilt: '0',
    executeFrame: 0,
    lifetime: 1,
    loop: false,
    loopTiming: null,
    limiterTime: 'none',
    limiterList: '',
    invertLimiter: false,
    generatedUsingValueList: false,
    note: '',
  }
}

export function emptyLayer(): Layer {
  return { note: '', units: [] }
}

export function cloneUnit(u: Unit): Unit {
  return { ...u }
}

export function cloneLayer(l: Layer): Layer {
  return { note: l.note, units: l.units.map(cloneUnit) }
}

function mapUnlocked(l: Layer, f: (u: Unit) => Unit): Layer {
  return { note: l.note, units: l.units.map((u) => (u.locked ? cloneUnit(u) : f(cloneUnit(u)))) }
}

/** Enables every unlocked unit, then disables a random half of them. */
export function disable50(l: Layer, rand: Rand): Layer {
  const out = mapUnlocked(l, (u) => ({ ...u, enabled: true }))
  const unlocked = out.units.filter((u) => !u.locked)
  for (const i of perm(unlocked.length, rand).slice(0, Math.floor(unlocked.length / 2))) {
    unlocked[i]!.enabled = false
  }
  return out
}

export function invertDisabled(l: Layer): Layer {
  return mapUnlocked(l, (u) => ({ ...u, enabled: !u.enabled }))
}

/** Removes unlocked disabled units. */
export function removeDisabled(l: Layer): Layer {
  return { note: l.note, units: l.units.filter((u) => u.locked || u.enabled).map(cloneUnit) }
}

export function enableAll(l: Layer): Layer {
  return mapUnlocked(l, (u) => ({ ...u, enabled: true }))
}

export function disableAll(l: Layer): Layer {
  return mapUnlocked(l, (u) => ({ ...u, enabled: false }))
}

export function removeIndices(l: Layer, indices: Iterable<number>): Layer {
  const drop = new Set(indices)
  return { note: l.note, units: l.units.filter((_, i) => !drop.has(i)).map(cloneUnit) }
}

/** Appends copies of the unlocked units at the given indices. */
export function duplicate(l: Layer, indices: number[]): Layer {
  const out = cloneLayer(l)
  for (const i of indices) {
    const u = l.units[i]
    if (u && !u.locked) out.units.push(cloneUnit(u))
  }
  return out
}

export function addUnit(l: Layer, u: Unit): Layer {
  const out = cloneLayer(l)
  out.units.push(u)
  return out
}

export type ShiftField =
  'address' | 'sourceAddress' | 'value' | 'lifetime' | 'executeFrame' | 'loopTiming' | 'tilt'

export const SHIFT_FIELDS: ShiftField[] = [
  'address',
  'sourceAddress',
  'value',
  'lifetime',
  'executeFrame',
  'loopTiming',
  'tilt',
]

const MAX_INT32 = 2 ** 31 - 1

function clampInt(v: number): number {
  return Math.min(Math.max(v, 0), MAX_INT32)
}

/**
 * Adds `amount` to a field of the units at `indices` (all units when empty).
 * Integer fields clamp at 0; `value` wraps around as a little-endian number.
 */
export function shift(l: Layer, field: ShiftField, amount: number, indices: number[] = []): Layer {
  const out = cloneLayer(l)
  const targets = indices.length ? indices : out.units.map((_, i) => i)
  for (const i of targets) {
    const u = out.units[i]
    if (!u) continue
    switch (field) {
      case 'address':
        u.address = Math.max(u.address + amount, 0)
        break
      case 'sourceAddress':
        u.sourceAddress = Math.max(u.sourceAddress + amount, 0)
        break
      case 'executeFrame':
        u.executeFrame = clampInt(u.executeFrame + amount)
        break
      case 'lifetime':
        u.lifetime = clampInt(u.lifetime + amount)
        break
      case 'loopTiming':
        u.loopTiming = clampInt((u.loopTiming ?? -1) + amount)
        break
      case 'tilt':
        u.tilt = (BigInt(u.tilt || '0') + BigInt(amount)).toString()
        break
      case 'value': {
        const n = Math.max(hexToBytes(u.value).length, 1)
        u.value = bigIntToLeHex(leHexToBigInt(u.value) + BigInt(amount), n)
        break
      }
    }
  }
  return out
}

/** Value bytes (little-endian) resized to `n` bytes like RTCV's SetPrecision. */
export function resizeValue(leHex: string, n: number): string {
  const b = hexToBytes(leHex)
  // Little-endian: the least significant bytes come first.
  const out = new Uint8Array(n)
  out.set(b.subarray(0, Math.min(n, b.length)))
  return bytesToHex(out)
}

/** Bytes a VALUE unit writes, in memory order: value + tilt, reversed if big-endian. */
export function writtenValue(u: Unit): Uint8Array {
  const v = leHexToBigInt(u.value) + BigInt(u.tilt || '0')
  const b = hexToBytes(bigIntToLeHex(v, u.precision))
  if (u.bigEndian) b.reverse()
  return b
}

/**
 * Splits units into 1-byte units. VALUE units get the bytes they would
 * write; STORE units keep the tilt on their least significant byte only.
 * Only the units at `indices` are split (all units when empty).
 */
export function breakDown(l: Layer, indices: number[] = []): Layer {
  const sel = new Set(indices.length ? indices : l.units.map((_, i) => i))
  const units: Unit[] = []
  l.units.forEach((u, idx) => {
    if (!sel.has(idx) || u.precision <= 1) {
      units.push(cloneUnit(u))
      return
    }
    const written = u.source === 'value' ? writtenValue(u) : null
    const lsb = u.bigEndian ? u.precision - 1 : 0
    for (let i = 0; i < u.precision; i++) {
      const s = cloneUnit(u)
      s.precision = 1
      s.address = u.address + i
      if (written) {
        s.value = bytesToHex(written.subarray(i, i + 1))
        s.bigEndian = false
        s.tilt = '0'
      } else {
        s.value = ''
        s.sourceAddress = u.sourceAddress + i
        if (i !== lsb) s.tilt = '0'
      }
      units.push(s)
    }
  })
  return { note: l.note, units }
}

/** Keeps only the last unlocked unit per (domain, address); locked units always stay. */
export function sanitizeDuplicates(l: Layer): Layer {
  const seen = new Set<string>()
  const keep: boolean[] = new Array(l.units.length).fill(false)
  for (let i = l.units.length - 1; i >= 0; i--) {
    const u = l.units[i]!
    const k = `${u.domain}\u0000${u.address}`
    keep[i] = u.locked || !seen.has(k)
    if (!u.locked) seen.add(k)
  }
  return { note: l.note, units: l.units.filter((_, i) => keep[i]).map(cloneUnit) }
}

/** Reads `size` bytes of memory; returns hex in memory order. */
export type MemoryReader = (domain: string, address: number, size: number) => Promise<string>

/**
 * Replaces the enabled units at `indices` (all when empty) with VALUE units
 * (lifetime 1) holding the current memory at their address, like RTCV's
 * "Bake to VALUE". Disabled units are left unchanged.
 */
export async function bake(l: Layer, indices: number[], read: MemoryReader): Promise<Layer> {
  const sel = new Set(indices.length ? indices : l.units.map((_, i) => i))
  const units: Unit[] = []
  for (const [i, u] of l.units.entries()) {
    if (!sel.has(i) || !u.enabled) {
      units.push(cloneUnit(u))
      continue
    }
    const mem = await read(u.domain, u.address, u.precision)
    units.push({
      ...newUnit(u.domain, u.address),
      precision: u.precision,
      value: mem,
      locked: u.locked,
      note: u.note,
    })
  }
  return { note: l.note, units }
}

export function applyToIndices(l: Layer, indices: number[], patch: Partial<Unit>): Layer {
  const out = cloneLayer(l)
  for (const i of indices) {
    const u = out.units[i]
    if (!u) continue
    Object.assign(u, patch)
    if (u.source === 'value' && u.value.length !== u.precision * 2) {
      u.value = resizeValue(u.value, u.precision)
    }
  }
  return out
}

/** Parses a `.bl` file (the Layer JSON object). Throws on invalid input. */
export function parseLayerFile(text: string): Layer {
  const obj = JSON.parse(text) as Partial<Layer>
  if (!obj || !Array.isArray(obj.units)) throw new Error('not a blast layer: missing "units"')
  return {
    note: typeof obj.note === 'string' ? obj.note : '',
    units: obj.units.map((u) => ({ ...newUnit(), ...u })),
  }
}
