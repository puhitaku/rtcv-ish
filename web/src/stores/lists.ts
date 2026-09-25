import { defineStore } from 'pinia'
import { ref } from 'vue'
import { call, client, rawFetch } from '@/api/client'
import type { ListInfo } from '@/api/types'

export const useListsStore = defineStore('lists', () => {
  const lists = ref<ListInfo[]>([])

  async function fetch() {
    lists.value = await call(client.GET('/lists'))
  }

  async function upload(file: Blob, filename: string, name?: string) {
    const fd = new FormData()
    fd.append('file', file, filename)
    if (name) fd.append('name', name)
    await rawFetch('/lists', { method: 'POST', body: fd })
    await fetch()
  }

  async function remove(name: string) {
    await call(client.DELETE('/lists/{name}', { params: { path: { name } } }))
    lists.value = lists.value.filter((l) => l.name !== name)
  }

  return { lists, fetch, upload, remove }
})
