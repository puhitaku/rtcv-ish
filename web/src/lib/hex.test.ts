import { describe, expect, it } from 'vitest'
import * as H from './hex'

describe('hex formatting', () => {
  it('formats and pads', () => {
    expect(H.hex(255, 4)).toBe('00FF')
    expect(H.formatAddress(0x10, 0x400000)).toBe('000010')
    expect(H.addressWidth(0)).toBe(4)
  })

  it('parses hex input', () => {
    expect(H.parseHex('0x1f')).toBe(31)
    expect(H.parseHex('$FF')).toBe(255)
    expect(H.parseHex(' 10 ')).toBe(16)
    expect(H.parseHex('zz')).toBeNull()
    expect(H.parseHex('')).toBeNull()
  })

  it('converts bytes', () => {
    expect(H.bytesToHex(new Uint8Array([1, 0xab]))).toBe('01ab')
    expect(Array.from(H.hexToBytes('abc'))).toEqual([0x0a, 0xbc])
    expect(H.isHexBytes('0aF0')).toBe(true)
    expect(H.isHexBytes('0aF')).toBe(false)
  })

  it('handles little-endian numbers', () => {
    expect(H.leHexToBigInt('3412')).toBe(0x1234n)
    expect(H.bigIntToLeHex(0x1234n, 2)).toBe('3412')
    expect(H.bigIntToLeHex(-1n, 2)).toBe('ffff')
    expect(H.bigIntToLeHex(0x10000n, 2)).toBe('0000')
  })

  it('shows unit values most significant byte first', () => {
    expect(H.valueDisplay('3412')).toBe('1234')
    expect(H.valueFromDisplay('1234', 2)).toBe('3412')
    expect(H.valueFromDisplay('1', 2)).toBe('0100')
    expect(H.valueFromDisplay('123456', 2)).toBeNull()
  })
})

describe('hex view model', () => {
  const data = new Uint8Array(Array.from({ length: 20 }, (_, i) => i + 0x40))

  it('builds 16-byte rows with ASCII', () => {
    const rows = H.hexRows(data, 0x100, 1, false)
    expect(rows).toHaveLength(2)
    expect(rows[0]!.address).toBe(0x100)
    expect(rows[0]!.cells).toHaveLength(16)
    expect(rows[0]!.cells[1]).toEqual({ offset: 1, size: 1, text: '41' })
    expect(rows[0]!.ascii).toBe('@ABCDEFGHIJKLMNO')
    expect(rows[1]!.cells).toHaveLength(4)
  })

  it('groups words by endianness', () => {
    expect(H.hexRows(data, 0, 2, false)[0]!.cells[0]!.text).toBe('4140')
    expect(H.hexRows(data, 0, 2, true)[0]!.cells[0]!.text).toBe('4041')
    expect(H.hexRows(data, 0, 4, false)[0]!.cells[1]).toEqual({
      offset: 4,
      size: 4,
      text: '47464544',
    })
  })

  it('shows non-printable bytes as dots', () => {
    expect(H.hexRows(new Uint8Array([0, 0x7f, 0x41]), 0, 1, false)[0]!.ascii).toBe('..A')
  })

  it('converts a typed word back to memory order', () => {
    expect(H.wordToMemoryHex('1234', 2, false)).toBe('3412')
    expect(H.wordToMemoryHex('1234', 2, true)).toBe('1234')
    expect(H.wordToMemoryHex('AB', 1, false)).toBe('ab')
  })
})
