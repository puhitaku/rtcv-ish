import { defineStore } from 'pinia'
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { call, client, inflight } from '@/api/client'
import type { BundledEmulator, BusyStatus, GameStatus, Status } from '@/api/types'

/** Status with the optional resilience fields filled in. */
export type NormalizedStatus = Omit<Status, 'busy' | 'unresponsive'> & {
  unresponsive: boolean
  busy: BusyStatus | null
}

/** Treats missing `unresponsive` as false and missing `busy` as null. */
export function normalizeStatus(s: Status): NormalizedStatus {
  return { ...s, unresponsive: s.unresponsive ?? false, busy: s.busy ?? null }
}

/** Busy for longer than this offers Quit / kill. */
export const STUCK_MS = 5000

export const useStatusStore = defineStore('status', () => {
  const status = ref<NormalizedStatus | null>(null)
  /** Latest frame, from `frame` events (faster than status updates). */
  const frame = ref(0)
  /** The SSE stream is open. */
  const live = ref(false)
  const emulators = ref<BundledEmulator[]>([])

  /** Local clock (ms) ticked while busy, for the elapsed time. */
  const now = ref(Date.now())
  /** Local time the running operation started, estimated from `sinceMs`. */
  const busyStart = ref(0)

  const connected = computed(() => status.value?.connected ?? false)
  const game = computed(() => status.value?.game)
  const hasRom = computed(() => connected.value && !!game.value && game.value.state !== 'noRom')
  const unresponsive = computed(() => connected.value && (status.value?.unresponsive ?? false))
  const busy = computed(() => status.value?.busy ?? null)
  /** Elapsed milliseconds of the running operation, 0 when idle. */
  const busyMs = computed(() => (busy.value ? Math.max(0, now.value - busyStart.value) : 0))
  /** Unresponsive, or busy for more than STUCK_MS: offer Quit / kill. */
  const stuck = computed(() => unresponsive.value || busyMs.value > STUCK_MS)

  /** Why operations cannot start now (busy/unresponsive), or ''. */
  const needIdle = computed(() => {
    if (unresponsive.value) return 'Emulator is unresponsive; disconnect or quit it'
    if (busy.value) return `Emulator is busy: ${busy.value.operation}`
    return ''
  })
  /** Why emulator actions are disabled, or '' when they are available. */
  const needEmu = computed(() => (connected.value ? needIdle.value : 'Emulator not connected'))
  const needRom = computed(() => needEmu.value || (hasRom.value ? '' : 'No ROM loaded'))

  function set(s: Status) {
    const n = normalizeStatus(s)
    const t = Date.now()
    now.value = t
    if (n.busy) busyStart.value = t - n.busy.sinceMs
    status.value = n
    if (n.game) frame.value = n.game.frame
  }

  function tick() {
    now.value = Date.now()
  }

  function setGame(g: GameStatus) {
    if (status.value) status.value = { ...status.value, game: g }
    frame.value = g.frame
  }

  function setFrame(f: number) {
    frame.value = f
  }

  async function fetch() {
    set(await call(client.GET('/status')))
  }

  /** Fetches status without counting as an in-flight call. */
  async function poll() {
    set(await call(client.GET('/status'), false))
  }

  async function fetchEmulators() {
    emulators.value = await call(client.GET('/emulators'))
  }

  async function connect(address: string) {
    set(await call(client.POST('/emulator/connect', { body: { address } })))
  }

  async function launch(name: string, rom?: string) {
    set(await call(client.POST('/emulator/launch', { body: rom ? { name, rom } : { name } })))
  }

  async function disconnect() {
    set(await call(client.POST('/emulator/disconnect')))
  }

  async function loadRom(path: string) {
    setGame(await call(client.POST('/emulator/rom', { body: { path } })))
  }

  async function control(op: 'pause' | 'resume' | 'reset' | 'close-rom') {
    const g = await call(client.POST(`/emulator/${op}`))
    setGame(g)
  }

  async function step(frames: number) {
    setGame(await call(client.POST('/emulator/step', { body: { frames } })))
  }

  async function quit() {
    await call(client.POST('/emulator/quit'))
  }

  async function protectionNow() {
    await call(client.POST('/protection/backup'))
  }

  async function protectionBack() {
    await call(client.POST('/protection/back'))
  }

  async function protectionLast() {
    await call(client.POST('/protection/last'))
  }

  return {
    status,
    frame,
    live,
    emulators,
    connected,
    game,
    hasRom,
    unresponsive,
    busy,
    busyMs,
    stuck,
    needIdle,
    needEmu,
    needRom,
    now,
    set,
    tick,
    poll,
    setGame,
    setFrame,
    fetch,
    fetchEmulators,
    connect,
    launch,
    disconnect,
    loadRom,
    control,
    step,
    quit,
    protectionNow,
    protectionBack,
    protectionLast,
  }
})

/** Tick interval of the local elapsed-time clock while busy. */
export const BUSY_TICK_MS = 1000
/** Fallback poll interval while the core reports `busy`. */
export const BUSY_POLL_MS = 5000

/**
 * The core sends a `status` event when an operation starts or ends, so
 * `busy` normally arrives over SSE. While an API call is pending or the
 * core reports `busy`, tick the elapsed time once a second; while busy,
 * also poll /status every BUSY_POLL_MS as a fallback for a missed event.
 * Registered by App for its lifetime.
 */
export function useBusyPoll() {
  const st = useStatusStore()
  let timer: ReturnType<typeof setTimeout> | undefined
  let polling = false
  let stopped = false
  /** Milliseconds busy since the last poll (or since busy was seen). */
  let sincePoll = 0

  const active = () => inflight.value > 0 || !!st.busy

  function schedule() {
    if (!st.busy) sincePoll = 0
    if (stopped || timer || !active()) return
    timer = setTimeout(run, BUSY_TICK_MS)
  }

  async function run() {
    timer = undefined
    st.tick()
    if (st.busy) sincePoll += BUSY_TICK_MS
    if (st.busy && sincePoll >= BUSY_POLL_MS && !polling) {
      sincePoll = 0
      polling = true
      try {
        await st.poll()
      } catch {
        // The SSE stream reports a lost core; keep polling while busy.
      } finally {
        polling = false
      }
    }
    schedule()
  }

  const stop = watch([inflight, () => st.busy], schedule, { immediate: true })
  onBeforeUnmount(() => {
    stopped = true
    stop()
    clearTimeout(timer)
    timer = undefined
  })
}
