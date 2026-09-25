import { describe, expect, it } from 'vitest'
import {
  addressPixel,
  autoStride,
  bitmapSize,
  bitmapWidth,
  byteSize,
  pixelAddress,
  pixelAtPoint,
  pixelCount,
  rgb565,
  rgb565ToRgba,
  wordAt,
} from './bitmap'

const KiB = 1024
const MiB = 1024 * KiB

describe('rgb565', () => {
  it('expands channels to 8 bits', () => {
    expect(rgb565(0x0000)).toEqual([0, 0, 0])
    expect(rgb565(0xffff)).toEqual([255, 255, 255])
    expect(rgb565(0xf800)).toEqual([255, 0, 0])
    expect(rgb565(0x07e0)).toEqual([0, 255, 0])
    expect(rgb565(0x001f)).toEqual([0, 0, 255])
    // r=16 -> 132, g=32 -> 130, b=1 -> 8
    expect(rgb565((16 << 11) | (32 << 5) | 1)).toEqual([132, 130, 8])
  })

  it('converts little-endian words to RGBA and clears the rest', () => {
    const words = new Uint8Array([0x00, 0xf8, 0xe0, 0x07, 0x1f, 0x00])
    const out = new Uint8ClampedArray(4 * 4).fill(7)
    rgb565ToRgba(words, out)
    expect(Array.from(out)).toEqual([255, 0, 0, 255, 0, 255, 0, 255, 0, 0, 255, 255, 0, 0, 0, 0])
  })

  it('ignores words beyond the output', () => {
    const out = new Uint8ClampedArray(4)
    rgb565ToRgba(new Uint8Array([0xff, 0xff, 0x00, 0xf8]), out)
    expect(Array.from(out)).toEqual([255, 255, 255, 255])
  })
})

describe('bitmap geometry', () => {
  it('picks a stride that keeps a fetch within 4 MiB', () => {
    expect(autoStride(4 * MiB)).toBe(1)
    expect(autoStride(8 * MiB)).toBe(2)
    expect(autoStride(16 * MiB)).toBe(4)
    expect(autoStride(2 * KiB)).toBe(1)
    expect(autoStride(5 * MiB)).toBe(2)
  })

  it('chooses a landscape power-of-two width', () => {
    expect(bitmapSize(4 * MiB, 1)).toEqual({ width: 2048, height: 1024, pixels: 2 * MiB })
    expect(bitmapSize(8 * MiB, 1)).toMatchObject({ width: 4096, height: 1024 })
    expect(bitmapSize(64 * KiB, 1)).toMatchObject({ width: 256, height: 128 })
    expect(bitmapSize(2 * KiB, 1)).toMatchObject({ width: 64, height: 16 })
    expect(bitmapSize(16 * MiB, 1)).toMatchObject({ width: 4096, height: 2048 })
    expect(bitmapWidth(2)).toBe(1)
    expect(bitmapWidth(6)).toBe(4)
  })

  it('keeps the width when the stride changes', () => {
    expect(bitmapSize(8 * MiB, 2)).toMatchObject({ width: 4096, height: 512 })
    expect(bitmapSize(16 * MiB, 4)).toMatchObject({ width: 4096, height: 512 })
    expect(bitmapSize(2 * KiB, 16)).toEqual({ width: 64, height: 1, pixels: 64 })
    expect(bitmapSize(2 * KiB, 8)).toMatchObject({ width: 64, height: 2, pixels: 128 })
  })

  it('counts pixels like the words endpoint', () => {
    expect(pixelCount(10, 1)).toBe(5)
    expect(pixelCount(11, 1)).toBe(5)
    expect(pixelCount(10, 4)).toBe(2)
  })

  it('maps pixels to addresses and back', () => {
    expect(pixelAddress(0, 1)).toBe(0)
    expect(pixelAddress(5, 1)).toBe(10)
    expect(pixelAddress(5, 4)).toBe(40)
    expect(addressPixel(41, 4)).toBe(5)
    expect(addressPixel(39, 4)).toBe(4)
    expect(addressPixel(pixelAddress(1234, 2), 2)).toBe(1234)
  })

  it('maps a scaled point to a pixel', () => {
    const b = { width: 64, height: 16, pixels: 64 * 16 - 10 }
    const rect = { left: 100, top: 50, width: 640, height: 160 }
    expect(pixelAtPoint(100, 50, rect, b)).toBe(0)
    expect(pixelAtPoint(109.9, 59.9, rect, b)).toBe(0)
    expect(pixelAtPoint(110, 60, rect, b)).toBe(65)
    expect(pixelAtPoint(739, 60, rect, b)).toBe(127)
    expect(pixelAtPoint(99, 60, rect, b)).toBeNull()
    expect(pixelAtPoint(740, 60, rect, b)).toBeNull()
    // Last row past the final pixel.
    expect(pixelAtPoint(739, 209, rect, b)).toBeNull()
  })

  it('reads the word of a pixel', () => {
    const w = new Uint8Array([0x34, 0x12, 0xcd, 0xab])
    expect(wordAt(w, 0)).toBe(0x1234)
    expect(wordAt(w, 1)).toBe(0xabcd)
    expect(wordAt(w, 2)).toBeNull()
  })
})

describe('byteSize', () => {
  it('prints whole units', () => {
    expect(byteSize(4 * MiB)).toBe('4 MiB')
    expect(byteSize(64 * KiB)).toBe('64 KiB')
    expect(byteSize(2 * KiB)).toBe('2 KiB')
    expect(byteSize(1536)).toBe('1536 B')
  })
})
