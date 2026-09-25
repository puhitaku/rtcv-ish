import { call, client } from './client'

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
