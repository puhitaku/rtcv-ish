import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { call, client } from '@/api/client'
import type { Domain } from '@/api/types'

export const useDomainsStore = defineStore('domains', () => {
  const domains = ref<Domain[]>([])
  const selected = computed(() => domains.value.filter((d) => d.selected).map((d) => d.name))

  async function fetch() {
    domains.value = await call(client.GET('/domains'))
  }

  async function setSelected(names: string[]) {
    domains.value = await call(client.PUT('/domains/selected', { body: { names } }))
  }

  async function toggle(name: string) {
    const cur = new Set(selected.value)
    if (cur.has(name)) cur.delete(name)
    else cur.add(name)
    await setSelected(domains.value.filter((d) => cur.has(d.name)).map((d) => d.name))
  }

  async function autoSelect() {
    domains.value = await call(client.POST('/domains/auto-select'))
  }

  function byName(name: string): Domain | undefined {
    return domains.value.find((d) => d.name === name)
  }

  return {
    domains,
    selected,
    fetch,
    setSelected,
    toggle,
    autoSelect,
    selectAll: () => setSelected(domains.value.map((d) => d.name)),
    unselectAll: () => setSelected([]),
    byName,
  }
})
