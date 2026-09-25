import { reactive } from 'vue'
import { loadJSON, save } from '@/lib/storage'

export type Base = 'hex' | 'dec'

/** Numeric unit fields whose display base the user can switch. */
export type NumField =
  'address' | 'sourceAddress' | 'precision' | 'tilt' | 'executeFrame' | 'lifetime' | 'loopTiming'

export const DEFAULT_BASES: Record<NumField, Base> = {
  address: 'hex',
  sourceAddress: 'hex',
  precision: 'dec',
  tilt: 'dec',
  executeFrame: 'dec',
  lifetime: 'dec',
  loopTiming: 'dec',
}

export const BASES_KEY = 'rtcvish.fieldBases'

/** Formats an integer in `base`: uppercase hex digits without a prefix, sign kept. */
export function formatNum(v: number | bigint | string, base: Base, width = 0): string {
  let n: bigint
  try {
    n = BigInt(v)
  } catch {
    return String(v)
  }
  const neg = n < 0n
  const abs = neg ? -n : n
  const digits =
    base === 'hex' ? abs.toString(16).toUpperCase().padStart(width, '0') : abs.toString()
  return (neg ? '-' : '') + digits
}

/**
 * Parses an integer typed in `base`. Hex accepts an optional `0x` prefix in
 * either case; decimal input with a `0x` prefix is read as hex too. Returns
 * null for invalid input or a negative number when `signed` is false.
 */
export function parseNum(s: string, base: Base, signed = false): bigint | null {
  let t = s.trim()
  let neg = false
  if (t.startsWith('-') || t.startsWith('+')) {
    neg = t[0] === '-'
    t = t.slice(1)
  }
  let b = base
  if (/^0x/i.test(t)) {
    b = 'hex'
    t = t.slice(2)
  }
  const ok = b === 'hex' ? /^[0-9a-f]+$/i.test(t) : /^\d+$/.test(t)
  if (!ok) return null
  const n = BigInt(b === 'hex' ? '0x' + t : t)
  if (neg && !signed && n !== 0n) return null
  return neg ? -n : n
}

/** Like parseNum, as a safe non-negative integer. */
export function parseSafe(s: string, base: Base): number | null {
  const n = parseNum(s, base)
  if (n == null || n > BigInt(Number.MAX_SAFE_INTEGER)) return null
  return Number(n)
}

export function loadBases(): Record<NumField, Base> {
  const raw = loadJSON<Record<string, unknown>>(BASES_KEY, { ...DEFAULT_BASES })
  const out = { ...DEFAULT_BASES }
  for (const k of Object.keys(out) as NumField[]) {
    if (raw[k] === 'hex' || raw[k] === 'dec') out[k] = raw[k]
  }
  return out
}

/** Per-field display base shared by the property editor and the table, persisted in localStorage. */
export const fieldBases = reactive(loadBases())

export function setBase(f: NumField, b: Base) {
  fieldBases[f] = b
  save(BASES_KEY, { ...fieldBases })
}

export function toggleBase(f: NumField) {
  setBase(f, fieldBases[f] === 'hex' ? 'dec' : 'hex')
}
