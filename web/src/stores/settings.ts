import { defineStore } from 'pinia'
import { ref } from 'vue'
import { call, client } from '@/api/client'
import type { Settings, SettingsPatch } from '@/api/types'

function isObject(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v)
}

/** Deep merge like the server's PATCH: nested objects merge field by field. */
export function deepMerge<T>(base: T, patch: unknown): T {
  if (!isObject(base) || !isObject(patch)) return (patch === undefined ? base : patch) as T
  const out: Record<string, unknown> = { ...base }
  for (const [k, v] of Object.entries(patch)) {
    out[k] = isObject(v) && isObject(out[k]) ? deepMerge(out[k], v) : v
  }
  return out as T
}

export const useSettingsStore = defineStore('settings', () => {
  const settings = ref<Settings | null>(null)

  async function fetch() {
    settings.value = await call(client.GET('/settings'))
  }

  /** Optimistic PATCH: applies locally, then takes the server's result; reverts on error. */
  async function patch(p: SettingsPatch) {
    const before = settings.value
    if (before) settings.value = deepMerge(before, p)
    try {
      settings.value = await call(client.PATCH('/settings', { body: p }))
    } catch (e) {
      settings.value = before
      throw e
    }
  }

  return { settings, fetch, patch }
})
