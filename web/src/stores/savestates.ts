import { defineStore } from 'pinia'
import { ref } from 'vue'
import { call, client } from '@/api/client'
import type { SavestateSlot } from '@/api/types'

export const SLOT_COUNT = 50

export const useSavestatesStore = defineStore('savestates', () => {
  const slots = ref<SavestateSlot[]>(
    Array.from({ length: SLOT_COUNT }, (_, i) => ({ slot: i + 1, label: '' })),
  )

  function replace(s: SavestateSlot) {
    slots.value = slots.value.map((x) => (x.slot === s.slot ? s : x))
  }

  function get(slot: number): SavestateSlot | undefined {
    return slots.value.find((s) => s.slot === slot)
  }

  async function fetch() {
    const list = await call(client.GET('/savestates'))
    if (list.length) slots.value = list
  }

  async function save(slot: number) {
    replace(await call(client.POST('/savestates/{slot}', { params: { path: { slot } } })))
  }

  async function load(slot: number) {
    await call(client.POST('/savestates/{slot}/load', { params: { path: { slot } } }))
  }

  async function setLabel(slot: number, label: string) {
    replace(
      await call(
        client.PATCH('/savestates/{slot}', { params: { path: { slot } }, body: { label } }),
      ),
    )
  }

  async function remove(slot: number) {
    await call(client.DELETE('/savestates/{slot}', { params: { path: { slot } } }))
    replace({ slot, label: '' })
  }

  return { slots, get, fetch, save, load, setLabel, remove }
})
