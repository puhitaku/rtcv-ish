<script setup lang="ts">
import { nextTick, onMounted, ref } from 'vue'
import { call, client, errorMessage } from '@/api/client'
import type { DirEntry, DirListing } from '@/api/types'
import { loadString, save } from '@/lib/storage'

const emit = defineEmits<{ select: [path: string]; close: [] }>()

const KEY = 'rtcvish.browse.dir'

const listing = ref<DirListing | null>(null)
const pathInput = ref('')
const error = ref('')
const loading = ref(false)
const box = ref<HTMLElement | null>(null)

let seq = 0

async function go(path?: string, fallback = false) {
  const id = ++seq
  const typed = pathInput.value
  loading.value = true
  error.value = ''
  try {
    const l = await call(client.GET('/browse', { params: { query: path ? { path } : {} } }))
    if (id !== seq) return
    listing.value = l
    // Keep what the user typed while the listing loaded.
    if (pathInput.value === typed) pathInput.value = l.path
    save(KEY, l.path)
  } catch (e) {
    if (id !== seq) return
    if (fallback) return go()
    error.value = errorMessage(e)
  } finally {
    if (id === seq) loading.value = false
  }
}

function up() {
  if (listing.value?.parent) void go(listing.value.parent)
}

function open(e: DirEntry) {
  if (e.dir) void go(e.path)
  else emit('select', e.path)
}

function size(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1 << 20) return `${(n / 1024).toFixed(1)} KiB`
  if (n < 1 << 30) return `${(n / (1 << 20)).toFixed(1)} MiB`
  return `${(n / (1 << 30)).toFixed(1)} GiB`
}

onMounted(async () => {
  const last = loadString(KEY, '')
  await go(last || undefined, !!last)
  await nextTick()
  box.value?.focus()
})
</script>

<template>
  <div
    class="fixed inset-0 z-50 flex items-start justify-center bg-black/30 pt-16"
    @keydown.esc.stop="emit('close')"
    @mousedown.self="emit('close')"
  >
    <div
      ref="box"
      tabindex="-1"
      class="box flex max-h-[70vh] w-[34rem] max-w-[calc(100vw-2rem)] flex-col outline-none"
      role="dialog"
      aria-label="Choose a ROM"
      data-testid="file-picker"
    >
      <div class="box-title flex items-center">
        <span class="flex-1">Choose a ROM (core host)</span>
        <button
          type="button"
          class="btn px-1 py-0"
          aria-label="Close"
          data-testid="picker-close"
          @click="emit('close')"
        >
          ×
        </button>
      </div>
      <form class="flex gap-1 p-2" @submit.prevent="go(pathInput)">
        <input
          v-model="pathInput"
          class="input flex-1 font-mono"
          placeholder="/path/to/dir"
          data-testid="picker-path"
        />
        <button type="submit" class="btn" data-testid="picker-go">Go</button>
      </form>
      <div v-if="error" class="px-2 pb-2 text-err" data-testid="picker-error">{{ error }}</div>
      <ul
        class="min-h-24 flex-1 overflow-auto border-t border-line font-mono"
        :aria-busy="loading"
        data-testid="picker-list"
      >
        <li v-if="listing?.parent">
          <button
            type="button"
            class="flex w-full gap-2 px-2 py-0.5 text-left hover:bg-inset"
            data-testid="picker-parent"
            @click="up"
          >
            <span class="w-4 text-dim">↑</span><span>..</span>
          </button>
        </li>
        <li v-for="e in listing?.entries ?? []" :key="e.path">
          <button
            type="button"
            class="flex w-full gap-2 px-2 py-0.5 text-left hover:bg-inset"
            :title="e.path"
            :data-testid="e.dir ? 'picker-dir' : 'picker-file'"
            :data-name="e.name"
            @click="open(e)"
          >
            <span class="w-4 text-dim">{{ e.dir ? '▸' : '' }}</span>
            <span class="flex-1 truncate" :class="{ 'text-accent': !e.dir }">
              {{ e.name }}
            </span>
            <span v-if="!e.dir" class="text-dim">{{ size(e.size) }}</span>
          </button>
        </li>
        <li
          v-if="listing && !listing.entries.length"
          class="px-2 py-0.5 text-dim"
          data-testid="picker-empty"
        >
          no folders or ROMs here
        </li>
      </ul>
    </div>
  </div>
</template>
