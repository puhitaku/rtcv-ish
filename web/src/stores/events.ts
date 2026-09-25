import type { BlastEvent, FrameEvent, LogEvent, Status, UnitsEvent } from '@/api/types'
import { useDomainsStore } from './domains'
import { useListsStore } from './lists'
import { useLogStore } from './log'
import { useSavestatesStore } from './savestates'
import { useSettingsStore } from './settings'
import { useStashStore } from './stash'
import { useStatusStore } from './status'
import { useStockpileStore } from './stockpile'
import { useUnitsStore } from './units'

export const EVENT_TYPES = [
  'status',
  'frame',
  'blast',
  'stash',
  'stockpile',
  'savestates',
  'settings',
  'domains',
  'lists',
  'units',
  'log',
] as const

type Refetch = () => Promise<unknown>

/** Runs a refetch; failures go to the log strip only (no toast spam). */
function refetch(what: string, f: Refetch): Promise<void> {
  return f().then(
    () => undefined,
    (e: unknown) => {
      useLogStore().add('warn', `refresh ${what} failed: ${e instanceof Error ? e.message : e}`)
    },
  )
}

/**
 * Refetches the scheduled units while a view shows them. Without a
 * connection there are none.
 */
function refetchUnits(): Promise<void> {
  const units = useUnitsStore()
  if (!useStatusStore().connected) {
    units.reset()
    return Promise.resolve()
  }
  if (!units.watchers) return Promise.resolve()
  return refetch('units', units.refresh)
}

/** "N scheduled units cleared by savestate load", or '' when nothing was cleared. */
export function unitsClearedMessage(e: UnitsEvent): string {
  if (e.cleared <= 0) return ''
  const what = e.reason === 'load' ? 'savestate load' : e.reason === 'reset' ? 'reset' : ''
  if (!what) return ''
  return `${e.cleared} scheduled unit${e.cleared === 1 ? '' : 's'} cleared by ${what}`
}

/** Refetches every resource (initial load and after SSE reconnects). */
export function refetchAll(): Promise<void[]> {
  return Promise.all([
    refetchUnits(),
    refetch('status', useStatusStore().fetch),
    refetch('settings', useSettingsStore().fetch),
    refetch('domains', useDomainsStore().fetch),
    refetch('stash', useStashStore().fetch),
    refetch('stockpile', useStockpileStore().fetch),
    refetch('savestates', useSavestatesStore().fetch),
    refetch('lists', useListsStore().fetch),
  ])
}

let lastGameKey = ''

/** Applies one SSE event to the stores. */
export async function handleEvent(type: string, raw: string): Promise<void> {
  let data: unknown = {}
  try {
    data = raw ? JSON.parse(raw) : {}
  } catch {
    return
  }
  switch (type) {
    case 'status': {
      const s = data as Status
      useStatusStore().set(s)
      // A new ROM or connection changes the domain list.
      const gk = `${s.connected}|${s.game?.romPath ?? ''}|${s.game?.state === 'noRom'}`
      const changed = lastGameKey && gk !== lastGameKey
      lastGameKey = gk
      if (changed) await Promise.all([refetch('domains', useDomainsStore().fetch), refetchUnits()])
      return
    }
    case 'frame':
      useStatusStore().setFrame((data as FrameEvent).frame)
      return
    case 'blast': {
      const b = data as BlastEvent
      useLogStore().add(
        'info',
        `blast: ${b.count} units (${b.engine}, ${b.elapsedMs.toFixed(1)} ms)`,
      )
      return
    }
    case 'units': {
      const msg = unitsClearedMessage(data as UnitsEvent)
      if (msg) useLogStore().add('info', msg)
      return refetchUnits()
    }
    case 'log': {
      const l = data as LogEvent
      useLogStore().add(l.level, l.msg)
      return
    }
    case 'stash':
      return refetch(type, useStashStore().fetch)
    case 'stockpile':
      return refetch(type, useStockpileStore().fetch)
    case 'savestates':
      return refetch(type, useSavestatesStore().fetch)
    case 'settings':
      return refetch(type, useSettingsStore().fetch)
    case 'domains':
      return refetch(type, useDomainsStore().fetch)
    case 'lists':
      return refetch(type, useListsStore().fetch)
  }
}

export interface EventSourceLike {
  onopen: ((ev: Event) => unknown) | null
  onerror: ((ev: Event) => unknown) | null
  addEventListener(type: string, listener: (ev: MessageEvent) => unknown): void
  close(): void
}

export type EventSourceFactory = (url: string) => EventSourceLike

/**
 * Opens the SSE stream. EventSource reconnects by itself; every (re)open
 * refetches all resources. Returns a stop function.
 */
export function startEvents(
  factory: EventSourceFactory = (url) => new EventSource(url),
  url = '/api/events',
): () => void {
  const status = useStatusStore()
  const es = factory(url)
  es.onopen = () => {
    status.live = true
    void refetchAll()
  }
  es.onerror = () => {
    status.live = false
  }
  for (const t of EVENT_TYPES) {
    es.addEventListener(t, (ev: MessageEvent) => void handleEvent(t, String(ev.data ?? '')))
  }
  return () => {
    es.close()
    status.live = false
  }
}
