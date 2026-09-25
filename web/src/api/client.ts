import createClient from 'openapi-fetch'
import type { paths } from './schema'

/** Error raised for non-2xx responses and network failures. */
export class ApiError extends Error {
  readonly code: string
  readonly status: number

  constructor(message: string, code: string, status: number) {
    super(message)
    this.name = 'ApiError'
    this.code = code
    this.status = status
  }
}

function origin(): string {
  return typeof location !== 'undefined' ? location.origin : 'http://localhost'
}

export const client = createClient<paths>({
  baseUrl: `${origin()}/api`,
  // Resolved at call time so tests can stub the global fetch.
  fetch: (req: Request) => globalThis.fetch(req),
})

function toApiError(body: unknown, status: number): ApiError {
  if (body && typeof body === 'object' && 'error' in body) {
    const b = body as { error?: unknown; code?: unknown }
    return new ApiError(String(b.error), typeof b.code === 'string' ? b.code : 'UNKNOWN', status)
  }
  const text = typeof body === 'string' && body ? body : `HTTP ${status}`
  return new ApiError(text, 'UNKNOWN', status)
}

type Result<T> = { data?: T; error?: unknown; response: Response }

/** Awaits an openapi-fetch call and returns its data, or throws ApiError. */
export async function call<T>(p: Promise<Result<T>>): Promise<T> {
  let r: Result<T>
  try {
    r = await p
  } catch (e) {
    throw new ApiError(e instanceof Error ? e.message : String(e), 'NETWORK', 0)
  }
  if (!r.response.ok || r.error !== undefined) throw toApiError(r.error, r.response.status)
  return r.data as T
}

/** Plain fetch for endpoints openapi-fetch does not model well (multipart, binary). */
export async function rawFetch(path: string, init?: RequestInit): Promise<Response> {
  let res: Response
  try {
    res = await globalThis.fetch(`${origin()}/api${path}`, init)
  } catch (e) {
    throw new ApiError(e instanceof Error ? e.message : String(e), 'NETWORK', 0)
  }
  if (!res.ok) {
    let body: unknown
    const text = await res.text()
    try {
      body = JSON.parse(text)
    } catch {
      body = text
    }
    throw toApiError(body, res.status)
  }
  return res
}

const HINTS: Record<string, string> = {
  NOT_IMPLEMENTED: 'not implemented yet',
  NETWORK: 'cannot reach the rtcv-ish core',
}

/** A one-line user message for any error. */
export function errorMessage(e: unknown): string {
  if (e instanceof ApiError) {
    const hint = HINTS[e.code]
    if (hint && !e.message.toLowerCase().includes(hint)) return `${e.message} (${hint})`
    return e.message
  }
  if (e instanceof Error) return e.message
  return String(e)
}
