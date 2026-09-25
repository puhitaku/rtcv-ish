<script setup lang="ts">
import { computed } from 'vue'
import EngineConfig from './engine/EngineConfig.vue'
import { useDomainsStore } from '@/stores/domains'
import { useSettingsStore } from '@/stores/settings'
import { useUiStore } from '@/stores/ui'

const ui = useUiStore()
const settings = useSettingsStore()
const domains = useDomainsStore()

/** One-line summary shown in the header while collapsed. */
const summary = computed(() => {
  const s = settings.settings
  if (!s) return ''
  const sel = domains.selected.length
  return `${s.engine} · intensity ${s.intensity} · ${s.precision * 8}-bit · ${sel} domain${sel === 1 ? '' : 's'}`
})
</script>

<template>
  <section class="flex shrink-0 flex-col border-b border-line" data-testid="engine-section">
    <button
      class="flex items-center gap-2 bg-panel px-2 py-0.5 text-left hover:bg-inset"
      :aria-expanded="ui.engineOpen"
      aria-controls="engine-section-body"
      :title="ui.engineOpen ? 'Collapse the engine settings' : 'Expand the engine settings'"
      data-testid="engine-toggle"
      @click="ui.engineOpen = !ui.engineOpen"
    >
      <span class="inline-block w-3 font-mono text-dim">{{ ui.engineOpen ? '▾' : '▸' }}</span>
      <span class="lbl">Engine</span>
      <span
        v-if="!ui.engineOpen"
        class="truncate font-mono text-xs text-dim"
        data-testid="engine-summary"
      >
        {{ summary }}
      </span>
    </button>
    <div
      v-show="ui.engineOpen"
      id="engine-section-body"
      class="max-h-[60vh] overflow-auto p-2"
      data-testid="engine-section-body"
    >
      <EngineConfig />
    </div>
  </section>
</template>
