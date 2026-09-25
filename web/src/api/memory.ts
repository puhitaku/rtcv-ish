import { call, client, rawFetch } from './client'

/** Reads `size` bytes; returns hex in memory order. */
export async function readMemory(domain: string, address: number, size: number): Promise<string> {
  const r = await call(
    client.GET('/memory/{domain}', {
      params: { path: { domain }, query: { address, size } },
    }),
  )
  return r.data
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
