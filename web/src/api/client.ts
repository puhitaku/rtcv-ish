import createClient from 'openapi-fetch'
import { ref } from 'vue'
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

/** Number of API calls in flight (tracked calls only). */
export const inflight = ref(0)

/**
 * Awaits an openapi-fetch call and returns its data, or throws ApiError.
 * Untracked calls (status polling) do not count in `inflight`.
 */
export async function call<T>(p: Promise<Result<T>>, track = true): Promise<T> {
  let r: Result<T>
  if (track) inflight.value++
  try {
    r = await p
  } catch (e) {
    throw new ApiError(e instanceof Error ? e.message : String(e), 'NETWORK', 0)
  } finally {
    if (track) inflight.value--
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

/** Codes that mean the emulator (or the core's operation gate) is stuck. */
export const STUCK_CODES: ReadonlySet<string> = new Set([
  'BUSY',
  'EMULATOR_TIMEOUT',
  'EMULATOR_UNRESPONSIVE',
])

function busyMessage(msg: string): string {
  // The core says "another operation is running: <name>".
  const op = /running: (\S+)\s*$/.exec(msg)?.[1]
  if (op) return `Emulator is busy: ${op}`
  return msg ? `Emulator is busy: ${msg}` : 'Emulator is busy'
}

/** A one-line user message for any error. */
export function errorMessage(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.code === 'BUSY') return busyMessage(e.message)
    if (e.code === 'EMULATOR_TIMEOUT') return 'Emulator did not answer in time'
    if (e.code === 'EMULATOR_UNRESPONSIVE') return 'Emulator is unresponsive; disconnect or quit it'
    const hint = HINTS[e.code]
    if (hint && !e.message.toLowerCase().includes(hint)) return `${e.message} (${hint})`
    return e.message
  }
  if (e instanceof Error) return e.message
  return String(e)
}
