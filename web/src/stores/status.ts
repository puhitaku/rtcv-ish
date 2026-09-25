import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { call, client } from '@/api/client'
import type { BundledEmulator, GameStatus, Status } from '@/api/types'

export const useStatusStore = defineStore('status', () => {
  const status = ref<Status | null>(null)
  /** Latest frame, from `frame` events (faster than status updates). */
  const frame = ref(0)
  /** The SSE stream is open. */
  const live = ref(false)
  const emulators = ref<BundledEmulator[]>([])

  const connected = computed(() => status.value?.connected ?? false)
  const game = computed(() => status.value?.game)
  const hasRom = computed(() => connected.value && !!game.value && game.value.state !== 'noRom')

  /** Why emulator actions are disabled, or '' when they are available. */
  const needEmu = computed(() => (connected.value ? '' : 'Emulator not connected'))
  const needRom = computed(() => needEmu.value || (hasRom.value ? '' : 'No ROM loaded'))

  function set(s: Status) {
    status.value = s
    if (s.game) frame.value = s.game.frame
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
    needEmu,
    needRom,
    set,
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
