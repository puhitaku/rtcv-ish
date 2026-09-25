import { defineStore } from 'pinia'
import { ref } from 'vue'
import { ApiError, errorMessage, STUCK_CODES } from '@/api/client'
import type { LogLevel } from '@/api/types'
import { useStatusStore } from './status'

export interface LogEntry {
  id: number
  time: Date
  level: LogLevel
  msg: string
}

const MAX_ENTRIES = 200
const TOAST_MS = 4000

let nextId = 1

export const useLogStore = defineStore('log', () => {
  const entries = ref<LogEntry[]>([])
  const toasts = ref<LogEntry[]>([])

  function add(level: LogLevel, msg: string): LogEntry {
    const e: LogEntry = { id: nextId++, time: new Date(), level, msg }
    entries.value.push(e)
    if (entries.value.length > MAX_ENTRIES)
      entries.value.splice(0, entries.value.length - MAX_ENTRIES)
    return e
  }

  function toast(level: LogLevel, msg: string) {
    const e: LogEntry = { id: nextId++, time: new Date(), level, msg }
    toasts.value.push(e)
    setTimeout(() => dismiss(e.id), TOAST_MS)
  }

  function dismiss(id: number) {
    toasts.value = toasts.value.filter((t) => t.id !== id)
  }

  /** Logs an API error to the strip and shows it as a toast. */
  function error(e: unknown, context?: string) {
    const msg = context ? `${context}: ${errorMessage(e)}` : errorMessage(e)
    add('error', msg)
    toast('error', msg)
    // Show what the emulator is stuck on in the top bar.
    if (e instanceof ApiError && STUCK_CODES.has(e.code))
      void useStatusStore()
        .poll()
        .catch(() => {})
  }

  function clear() {
    entries.value = []
  }

  return { entries, toasts, add, toast, dismiss, error, clear }
})

/**
 * Runs a UI action. Errors go to the log strip and a toast; the result is
 * undefined on failure. `ok` is logged on success.
 */
export async function act<T>(fn: () => Promise<T>, ok?: string): Promise<T | undefined> {
  const log = useLogStore()
  try {
    const r = await fn()
    if (ok) log.add('info', ok)
    return r
  } catch (e) {
    log.error(e)
    return undefined
  }
}
