import { defineStore } from 'pinia'
import { ref } from 'vue'
import { call, client } from '@/api/client'
import type { EmuUnit, Layer } from '@/api/types'

/** Units scheduled in the emulator, and blast actions. */
export const useUnitsStore = defineStore('units', () => {
  /** The server's scheduled units as of the last fetch. */
  const units = ref<EmuUnit[]>([])
  const lastBlast = ref<Layer | null>(null)
  /** Views showing `units`; `units` events refetch only while one is open. */
  const watchers = ref(0)

  async function fetch() {
    units.value = await call(client.GET('/blast/units'), false)
  }

  let running: Promise<void> | null = null
  let again = false

  /**
   * Refetches the units. Calls made while a fetch runs are coalesced into
   * one more fetch, so a burst of `units` events costs at most two.
   */
  function refresh(): Promise<void> {
    if (running) {
      again = true
      return running
    }
    running = (async () => {
      try {
        do {
          again = false
          await fetch()
        } while (again)
      } finally {
        running = null
      }
    })()
    return running
  }

  /** Registers a view that shows units; returns the unregister function. */
  function watch(): () => void {
    watchers.value++
    let done = false
    return () => {
      if (done) return
      done = true
      watchers.value--
    }
  }

  function reset() {
    units.value = []
  }

  async function clear() {
    await call(client.DELETE('/blast/units'))
    units.value = []
  }

  /** Removes one scheduled unit. */
  async function remove(id: number) {
    await call(client.DELETE('/blast/units/{id}', { params: { path: { id } } }))
    units.value = units.value.filter((u) => u.id !== id)
  }

  async function blast() {
    lastBlast.value = await call(client.POST('/blast'))
    return lastBlast.value
  }

  async function apply(layer: Layer, backup: boolean) {
    await call(client.POST('/blast/apply', { body: { layer, backup } }))
  }

  async function toggle(on: boolean) {
    await call(client.POST('/blast/toggle', { body: { on } }))
  }

  async function reroll(layer: Layer) {
    return call(client.POST('/blast/reroll', { body: { layer } }))
  }

  return {
    units,
    lastBlast,
    watchers,
    fetch,
    refresh,
    watch,
    reset,
    clear,
    remove,
    blast,
    apply,
    toggle,
    reroll,
  }
})
