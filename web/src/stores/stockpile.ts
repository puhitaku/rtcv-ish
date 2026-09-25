import { defineStore } from 'pinia'
import { ref } from 'vue'
import { call, client, rawFetch } from '@/api/client'
import type { Layer, StashKey } from '@/api/types'

const key = (k: string) => ({ params: { path: { key: k } } })

export const EXPORT_URL = '/api/stockpile/export'

export const useStockpileStore = defineStore('stockpile', () => {
  const keys = ref<StashKey[]>([])
  /** Host path of the last Load/Save as, for plain Save. */
  const path = ref('')

  async function fetch() {
    keys.value = await call(client.GET('/stockpile'))
  }

  async function run(k: string) {
    await call(client.POST('/stockpile/{key}/run', key(k)))
  }

  async function update(k: string, body: { alias?: string; note?: string }) {
    const r = await call(client.PATCH('/stockpile/{key}', { ...key(k), body }))
    keys.value = keys.value.map((x) => (x.key === k ? r : x))
  }

  async function remove(k: string) {
    await call(client.DELETE('/stockpile/{key}', key(k)))
    keys.value = keys.value.filter((x) => x.key !== k)
  }

  async function clear() {
    await call(client.DELETE('/stockpile'))
    keys.value = []
  }

  async function reorder(order: string[]) {
    keys.value = await call(client.POST('/stockpile/reorder', { body: { keys: order } }))
  }

  /** Moves the selected keys one step up (-1) or down (+1), keeping their relative order. */
  async function move(selected: string[], dir: -1 | 1) {
    const order = keys.value.map((k) => k.key)
    const sel = new Set(selected)
    const idx = order.map((_, i) => i)
    if (dir > 0) idx.reverse()
    for (const i of idx) {
      const j = i + dir
      if (!sel.has(order[i]!) || j < 0 || j >= order.length || sel.has(order[j]!)) continue
      ;[order[i], order[j]] = [order[j]!, order[i]!]
    }
    await reorder(order)
  }

  async function saveTo(p: string) {
    await call(client.POST('/stockpile/save', { body: { path: p } }))
    path.value = p
  }

  async function loadFrom(p: string) {
    keys.value = await call(client.POST('/stockpile/load', { body: { path: p } }))
    path.value = p
  }

  async function importFile(file: Blob, name: string, merge: boolean) {
    const fd = new FormData()
    fd.append('file', file, name)
    const res = await rawFetch(`/stockpile/import?merge=${merge}`, { method: 'POST', body: fd })
    keys.value = (await res.json()) as StashKey[]
  }

  async function getLayer(k: string): Promise<Layer> {
    return call(client.GET('/stockpile/{key}/layer', key(k)))
  }

  async function putLayer(k: string, layer: Layer): Promise<Layer> {
    return call(client.PUT('/stockpile/{key}/layer', { ...key(k), body: layer }))
  }

  return {
    keys,
    path,
    fetch,
    run,
    update,
    remove,
    clear,
    reorder,
    move,
    saveTo,
    loadFrom,
    importFile,
    getLayer,
    putLayer,
  }
})
