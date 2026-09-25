/** Uppercase hex of a non-negative integer, zero-padded to width. */
export function hex(n: number | bigint, width = 0): string {
  return n.toString(16).toUpperCase().padStart(width, '0')
}

/** Hex digits needed to show every address of a domain of `size` bytes (at least 4). */
export function addressWidth(size: number): number {
  return Math.max(4, Math.max(size - 1, 0).toString(16).length)
}

export function formatAddress(address: number, domainSize = 0): string {
  return hex(address, addressWidth(domainSize))
}

/** Parses "1F", "0x1f" or "$1f". Returns null for invalid input. */
export function parseHex(s: string): number | null {
  const t = s.trim().replace(/^(0x|\$)/i, '')
  if (!/^[0-9a-fA-F]+$/.test(t)) return null
  const n = parseInt(t, 16)
  return Number.isSafeInteger(n) ? n : null
}

export function isHexBytes(s: string): boolean {
  return /^([0-9a-fA-F]{2})*$/.test(s)
}

export function bytesToHex(b: Uint8Array): string {
  let s = ''
  for (const x of b) s += x.toString(16).padStart(2, '0')
  return s
}

/** Decodes a hex byte string; an odd number of digits is left-padded with 0. */
export function hexToBytes(h: string): Uint8Array {
  const s = h.length % 2 ? '0' + h : h
  const out = new Uint8Array(s.length / 2)
  for (let i = 0; i < out.length; i++) out[i] = parseInt(s.slice(i * 2, i * 2 + 2), 16)
  return out
}

/** Little-endian hex bytes to an unsigned integer. */
export function leHexToBigInt(h: string): bigint {
  const b = hexToBytes(h)
  let v = 0n
  for (let i = b.length - 1; i >= 0; i--) v = (v << 8n) | BigInt(b[i]!)
  return v
}

/** Unsigned integer to `n` little-endian bytes as hex, wrapping around. */
export function bigIntToLeHex(v: bigint, n: number): string {
  const mod = 1n << BigInt(8 * n)
  let x = ((v % mod) + mod) % mod
  const b = new Uint8Array(n)
  for (let i = 0; i < n; i++) {
    b[i] = Number(x & 0xffn)
    x >>= 8n
  }
  return bytesToHex(b)
}

/** Unit value (little-endian hex) as a big-endian display string, e.g. "0102" -> "0201". */
export function valueDisplay(leHex: string): string {
  return bytesToHex(hexToBytes(leHex).reverse()).toUpperCase()
}

/** Big-endian display string back to little-endian hex of `n` bytes. */
export function valueFromDisplay(s: string, n: number): string | null {
  const t = s.trim().replace(/^0x/i, '')
  if (!/^[0-9a-fA-F]*$/.test(t) || t.length > n * 2) return null
  return bytesToHex(hexToBytes(t.padStart(n * 2, '0')).reverse())
}

export type Group = 1 | 2 | 4

export interface HexCell {
  /** Offset of the group's first byte from the view base. */
  offset: number
  size: number
  /** Word value as shown (most significant byte first). */
  text: string
}

export interface HexRow {
  address: number
  cells: HexCell[]
  ascii: string
}

/**
 * Hex view model: rows of `perRow` bytes, grouped into words. Words are shown
 * most significant byte first, so little-endian domains show reversed bytes.
 */
export function hexRows(
  data: Uint8Array,
  base: number,
  group: Group,
  bigEndian: boolean,
  perRow = 16,
): HexRow[] {
  const rows: HexRow[] = []
  for (let r = 0; r < data.length; r += perRow) {
    const cells: HexCell[] = []
    let ascii = ''
    for (let c = r; c < Math.min(r + perRow, data.length); c += group) {
      const bytes = Array.from(data.subarray(c, Math.min(c + group, data.length)))
      if (!bigEndian) bytes.reverse()
      cells.push({ offset: c, size: bytes.length, text: bytes.map((x) => hex(x, 2)).join('') })
    }
    for (let c = r; c < Math.min(r + perRow, data.length); c++) {
      const x = data[c]!
      ascii += x >= 0x20 && x < 0x7f ? String.fromCharCode(x) : '.'
    }
    rows.push({ address: base + r, cells, ascii })
  }
  return rows
}

/** Word typed in the hex view (MSB first) to bytes in memory order, as hex. */
export function wordToMemoryHex(text: string, size: number, bigEndian: boolean): string {
  const b = hexToBytes(text.padStart(size * 2, '0').slice(-size * 2))
  if (!bigEndian) b.reverse()
  return bytesToHex(b)
}
