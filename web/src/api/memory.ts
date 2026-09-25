import { call, client, rawFetch } from './client'

/** Largest range GET /memory/{domain} returns in one request. */
export const MEMORY_READ_MAX = 0x10000

/** Reads `size` bytes, in requests of at most MEMORY_READ_MAX; returns hex in memory order. */
export async function readMemory(domain: string, address: number, size: number): Promise<string> {
  const parts: string[] = []
  for (let off = 0; off < size; off += MEMORY_READ_MAX) {
    const r = await call(
      client.GET('/memory/{domain}', {
        params: {
          path: { domain },
          query: { address: address + off, size: Math.min(MEMORY_READ_MAX, size - off) },
        },
      }),
    )
    parts.push(r.data)
  }
  return parts.join('')
}

export async function writeMemory(domain: string, address: number, data: string): Promise<void> {
  await call(
    client.PUT('/memory/{domain}', { params: { path: { domain } }, body: { address, data } }),
  )
}

/** Every `stride`-th little-endian 16-bit word of the range, as raw bytes. */
export async function readWords(
  domain: string,
  address: number,
  size: number,
  stride: number,
): Promise<Uint8Array> {
  const q = new URLSearchParams({
    address: String(address),
    size: String(size),
    stride: String(stride),
  })
  const r = await rawFetch(`/memory/${encodeURIComponent(domain)}/words?${q}`)
  return new Uint8Array(await r.arrayBuffer())
}
