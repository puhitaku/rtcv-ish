import { vi } from 'vitest'

export interface Call {
  method: string
  path: string
  body: unknown
}

type Handler = (call: Call) => unknown

/**
 * Stubs global fetch. Routes are "METHOD /api/path" (query ignored). A
 * handler returns the JSON body, or a Response for full control.
 */
export function mockFetch(routes: Record<string, Handler | object>) {
  const calls: Call[] = []
  const fn = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const req = input instanceof Request ? input : new Request(String(input), init)
    const url = new URL(req.url)
    const text = req.method === 'GET' || req.method === 'HEAD' ? '' : await req.text()
    let body: unknown = undefined
    try {
      body = text ? JSON.parse(text) : undefined
    } catch {
      body = text
    }
    const call = { method: req.method, path: url.pathname, body }
    calls.push(call)
    const h = routes[`${req.method} ${url.pathname}`]
    if (h === undefined) {
      return new Response(JSON.stringify({ error: 'no route', code: 'NOT_FOUND' }), {
        status: 404,
        headers: { 'Content-Type': 'application/json' },
      })
    }
    const r = await (typeof h === 'function' ? (h as Handler)(call) : h)
    if (r instanceof Response) return r
    if (r === undefined) return new Response(null, { status: 204 })
    return new Response(JSON.stringify(r), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })
  })
  vi.stubGlobal('fetch', fn)
  return { fn, calls }
}

export function errorResponse(status: number, code: string, error: string): Response {
  return new Response(JSON.stringify({ error, code }), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}
