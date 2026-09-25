import { defineStore } from 'pinia'
import { ref } from 'vue'
import { call, client } from '@/api/client'
import type { Layer, StashKey } from '@/api/types'

const key = (k: string) => ({ params: { path: { key: k } } })

export const useStashStore = defineStore('stash', () => {
  const keys = ref<StashKey[]>([])

  async function fetch() {
    keys.value = await call(client.GET('/stash'))
  }

  function added(k: StashKey): StashKey {
    const rest = { ...k }
    delete rest.layer
    if (!keys.value.some((x) => x.key === k.key)) keys.value = [...keys.value, rest]
    return k
  }

  async function corrupt(slot: number, loadBefore: boolean) {
    return added(await call(client.POST('/stash/corrupt', { body: { slot, loadBefore } })))
  }

  async function inject(k: string, slot: number) {
    return added(await call(client.POST('/stash/inject', { body: { key: k, slot } })))
  }

  async function merge(ks: string[]) {
    return added(await call(client.POST('/stash/merge', { body: { keys: ks } })))
  }

  async function run(k: string) {
    await call(client.POST('/stash/{key}/run', key(k)))
  }

  async function original(k: string) {
    await call(client.POST('/stash/{key}/original', key(k)))
  }

  async function reroll(k: string) {
    return added(await call(client.POST('/stash/{key}/reroll', key(k))))
  }

  async function update(k: string, body: { alias?: string; note?: string }) {
    const r = await call(client.PATCH('/stash/{key}', { ...key(k), body }))
    keys.value = keys.value.map((x) => (x.key === k ? r : x))
  }

  async function remove(k: string) {
    await call(client.DELETE('/stash/{key}', key(k)))
    keys.value = keys.value.filter((x) => x.key !== k)
  }

  async function clear() {
    await call(client.DELETE('/stash'))
    keys.value = []
  }

  async function toStockpile(k: string, alias?: string) {
    const r = await call(
      client.POST('/stash/{key}/to-stockpile', { ...key(k), body: alias ? { alias } : {} }),
    )
    keys.value = keys.value.filter((x) => x.key !== k)
    return r
  }

  async function getLayer(k: string): Promise<Layer> {
    return call(client.GET('/stash/{key}/layer', key(k)))
  }

  async function putLayer(k: string, layer: Layer): Promise<Layer> {
    return call(client.PUT('/stash/{key}/layer', { ...key(k), body: layer }))
  }

  return {
    keys,
    fetch,
    corrupt,
    inject,
    merge,
    run,
    original,
    reroll,
    update,
    remove,
    clear,
    toStockpile,
    getLayer,
    putLayer,
  }
})
