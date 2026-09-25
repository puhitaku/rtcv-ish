/**
 * Memory bitmap model: a domain drawn as RGB565 pixels, one little-endian
 * 16-bit word per pixel. With a stride s only every s-th word is fetched,
 * so pixel i stands for the 2*s bytes at 2*s*i.
 */

/** Domain size for labels: "4 MiB", "2 KiB", or bytes when not a whole unit. */
export function byteSize(n: number): string {
  if (n >= 1 << 20 && n % (1 << 20) === 0) return `${n >> 20} MiB`
  if (n >= 1 << 10 && n % (1 << 10) === 0) return `${n >> 10} KiB`
  return `${n} B`
}

/** Largest number of bytes one bitmap fetches per refresh at the auto stride. */
export const MAX_FETCH = 4 << 20
export const MAX_WIDTH = 4096
export const STRIDES = [1, 2, 4, 8, 16] as const

/** Smallest power-of-two stride that keeps a whole-domain fetch within `max` bytes. */
export function autoStride(size: number, max = MAX_FETCH): number {
  let s = 1
  while (size / s > max && s < 1 << 16) s *= 2
  return s
}

/** Pixels of a whole domain at a stride; matches the words endpoint's word count. */
export function pixelCount(size: number, stride: number): number {
  return Math.ceil(Math.floor(size / 2) / stride)
}

/**
 * Native width of a domain's bitmap: a power of two chosen from the
 * stride-1 pixel count so the image is landscape (4:1 or 2:1), capped at
 * MAX_WIDTH. The width does not depend on the stride, so changing the
 * stride only changes the height.
 */
export function bitmapWidth(size: number): number {
  const px = pixelCount(size, 1)
  if (px <= 1) return 1
  const n = Math.ceil(Math.log2(px))
  return Math.min(MAX_WIDTH, 2 ** Math.floor((n + 2) / 2), 2 ** n)
}

export interface BitmapSize {
  width: number
  height: number
  pixels: number
}

export function bitmapSize(size: number, stride: number): BitmapSize {
  const width = bitmapWidth(size)
  const pixels = pixelCount(size, stride)
  return { width, height: Math.max(1, Math.ceil(pixels / width)), pixels }
}

/** Domain-relative address of the first byte a pixel stands for. */
export function pixelAddress(index: number, stride: number): number {
  return index * 2 * stride
}

/** Pixel that covers a domain-relative address. */
export function addressPixel(address: number, stride: number): number {
  return Math.floor(address / (2 * stride))
}

/**
 * Pixel index under a point in client coordinates, for an image of
 * `width`x`height` native pixels scaled into `rect`. Null outside the image
 * or past the last pixel.
 */
export function pixelAtPoint(
  clientX: number,
  clientY: number,
  rect: { left: number; top: number; width: number; height: number },
  b: BitmapSize,
): number | null {
  if (rect.width <= 0 || rect.height <= 0) return null
  const x = Math.floor(((clientX - rect.left) / rect.width) * b.width)
  const y = Math.floor(((clientY - rect.top) / rect.height) * b.height)
  if (x < 0 || y < 0 || x >= b.width || y >= b.height) return null
  const i = y * b.width + x
  return i < b.pixels ? i : null
}

/** The little-endian 16-bit word of pixel `index` in fetched `words`. */
export function wordAt(words: Uint8Array, index: number): number | null {
  const o = index * 2
  if (o + 1 >= words.length) return null
  return words[o]! | (words[o + 1]! << 8)
}

/** 8-bit channels of an RGB565 word (bits 15..11 R, 10..5 G, 4..0 B). */
export function rgb565(v: number): [number, number, number] {
  const r = (v >> 11) & 0x1f
  const g = (v >> 5) & 0x3f
  const b = v & 0x1f
  return [(r << 3) | (r >> 2), (g << 2) | (g >> 4), (b << 3) | (b >> 2)]
}

let lut: Uint32Array | undefined

/** RGB565 -> RGBA lookup table in the platform's byte order for Uint32 writes. */
function rgbaLut(): Uint32Array {
  if (lut) return lut
  const bytes = new Uint8ClampedArray(65536 * 4)
  for (let v = 0; v < 65536; v++) {
    const [r, g, b] = rgb565(v)
    bytes[v * 4] = r
    bytes[v * 4 + 1] = g
    bytes[v * 4 + 2] = b
    bytes[v * 4 + 3] = 255
  }
  lut = new Uint32Array(bytes.buffer)
  return lut
}

/**
 * Converts little-endian RGB565 words into RGBA pixels. `out` has 4 bytes
 * per pixel; pixels without a word are left transparent.
 */
export function rgb565ToRgba(words: Uint8Array, out: Uint8ClampedArray): void {
  const table = rgbaLut()
  const dst = new Uint32Array(out.buffer, out.byteOffset, out.length >> 2)
  const n = Math.min(words.length >> 1, dst.length)
  for (let i = 0; i < n; i++) dst[i] = table[words[i * 2]! | (words[i * 2 + 1]! << 8)]!
  dst.fill(0, n)
}
