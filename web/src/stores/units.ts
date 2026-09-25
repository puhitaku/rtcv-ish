import { defineStore } from 'pinia'
import { ref } from 'vue'
import { call, client } from '@/api/client'
import type { EmuUnit, Layer } from '@/api/types'

/** Units scheduled in the emulator, and blast actions. */
export const useUnitsStore = defineStore('units', () => {
  const units = ref<EmuUnit[]>([])
  const lastBlast = ref<Layer | null>(null)

  async function fetch() {
    units.value = await call(client.GET('/blast/units'))
  }

  async function clear() {
    await call(client.DELETE('/blast/units'))
    units.value = []
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

  return { units, lastBlast, fetch, clear, blast, apply, toggle, reroll }
})
