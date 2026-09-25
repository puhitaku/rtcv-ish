<script setup lang="ts">
import { useUiStore, type Panel } from '@/stores/ui'

const ui = useUiStore()
const items: { id: Panel; label: string }[] = [
  { id: 'engine', label: 'Engine' },
  { id: 'harvester', label: 'Harvester' },
  { id: 'editor', label: 'Blast Editor' },
  { id: 'memory', label: 'Memory' },
  { id: 'settings', label: 'Settings' },
]
</script>

<template>
  <nav class="flex w-32 shrink-0 flex-col border-r border-line bg-panel" aria-label="Panels">
    <button
      v-for="it in items"
      :key="it.id"
      class="border-l-2 px-3 py-1.5 text-left hover:bg-inset"
      :class="ui.panel === it.id ? 'border-accent bg-inset text-accent' : 'border-transparent'"
      :data-testid="`nav-${it.id}`"
      :aria-current="ui.panel === it.id ? 'page' : undefined"
      @click="ui.panel = it.id"
    >
      {{ it.label }}
    </button>
  </nav>
</template>
