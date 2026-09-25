<script setup lang="ts">
import { computed, ref } from 'vue'
import { useLogStore } from '@/stores/log'

const log = useLogStore()
const expanded = ref(false)
const shown = computed(() => log.entries.slice(expanded.value ? -40 : -3).reverse())

const color: Record<string, string> = {
  debug: 'text-dim',
  info: 'text-fg',
  warn: 'text-warn',
  error: 'text-err',
}

function time(d: Date) {
  return d.toTimeString().slice(0, 8)
}
</script>

<template>
  <footer
    class="shrink-0 border-t border-line bg-panel font-mono text-[11px]"
    data-testid="log-strip"
  >
    <div class="flex items-start gap-2 px-2 py-0.5">
      <button
        class="lbl shrink-0 hover:text-fg"
        data-testid="log-toggle"
        :title="expanded ? 'Collapse log' : 'Expand log'"
        @click="expanded = !expanded"
      >
        {{ expanded ? '▼' : '▲' }} log
      </button>
      <ul class="min-w-0 flex-1 overflow-auto" :class="expanded ? 'max-h-60' : 'max-h-12'">
        <li v-if="!shown.length" class="text-dim">no messages</li>
        <li
          v-for="e in shown"
          :key="e.id"
          class="truncate"
          :class="color[e.level]"
          data-testid="log-entry"
        >
          <span class="text-dim">{{ time(e.time) }}</span> {{ e.msg }}
        </li>
      </ul>
      <button class="lbl shrink-0 hover:text-fg" data-testid="log-clear" @click="log.clear()">
        clear
      </button>
    </div>
  </footer>
</template>
