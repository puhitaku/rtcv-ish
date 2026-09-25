import { beforeEach, describe, expect, it, vi } from 'vitest'
import { BASES_KEY, DEFAULT_BASES, formatNum, loadBases, parseNum, parseSafe } from './numBase'

describe('formatNum', () => {
  it('formats in hex and decimal', () => {
    expect(formatNum(255, 'hex')).toBe('FF')
    expect(formatNum(255, 'dec')).toBe('255')
    expect(formatNum(0x1a, 'hex', 8)).toBe('0000001A')
    expect(formatNum('-18446744073709551615', 'hex')).toBe('-FFFFFFFFFFFFFFFF')
    expect(formatNum(-5n, 'dec')).toBe('-5')
    expect(formatNum('junk', 'hex')).toBe('junk')
  })
})

describe('parseNum', () => {
  it('parses hex with optional prefix in any case', () => {
    expect(parseNum('ff', 'hex')).toBe(255n)
    expect(parseNum('0xFF', 'hex')).toBe(255n)
    expect(parseNum(' 0XaB ', 'hex')).toBe(0xabn)
    expect(parseNum('10', 'hex')).toBe(16n)
  })
  it('parses decimal, and 0x-prefixed input as hex', () => {
    expect(parseNum('10', 'dec')).toBe(10n)
    expect(parseNum('0x10', 'dec')).toBe(16n)
    expect(parseNum('1f', 'dec')).toBeNull()
  })
  it('handles signs', () => {
    expect(parseNum('-0x10', 'hex', true)).toBe(-16n)
    expect(parseNum('-3', 'dec', true)).toBe(-3n)
    expect(parseNum('-3', 'dec')).toBeNull()
    expect(parseNum('-0', 'dec')).toBe(0n)
    expect(parseNum('+7', 'dec')).toBe(7n)
  })
  it('rejects junk', () => {
    for (const s of ['', '0x', 'g', '1.5', '--1', '0x-1'])
      expect(parseNum(s, 'hex', true)).toBeNull()
  })
  it('round-trips big values', () => {
    const v = '-340282366920938463463374607431768211455'
    for (const b of ['hex', 'dec'] as const)
      expect(parseNum(formatNum(v, b), b, true)).toBe(BigInt(v))
  })
  it('parseSafe limits to safe integers', () => {
    expect(parseSafe('1FFFFFFFFFFFFF', 'hex')).toBe(Number.MAX_SAFE_INTEGER)
    expect(parseSafe('20000000000000', 'hex')).toBeNull()
  })
})

describe('field base persistence', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.resetModules()
  })

  it('defaults to hex addresses and decimal counts', () => {
    expect(loadBases()).toEqual(DEFAULT_BASES)
  })

  it('remembers the choice per field', async () => {
    const m = await import('./numBase')
    m.toggleBase('lifetime')
    m.setBase('address', 'dec')
    expect(JSON.parse(localStorage.getItem(BASES_KEY)!)).toMatchObject({
      lifetime: 'hex',
      address: 'dec',
      tilt: 'dec',
    })
    vi.resetModules()
    const again = await import('./numBase')
    expect(again.fieldBases.lifetime).toBe('hex')
    expect(again.fieldBases.address).toBe('dec')
    expect(again.fieldBases.sourceAddress).toBe('hex')
  })

  it('ignores invalid stored values', () => {
    localStorage.setItem(BASES_KEY, JSON.stringify({ tilt: 'oct', precision: 'hex', bogus: 'hex' }))
    expect(loadBases()).toEqual({ ...DEFAULT_BASES, precision: 'hex' })
  })
})
