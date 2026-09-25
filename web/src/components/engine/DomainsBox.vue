<script setup lang="ts">
import BoxPanel from '@/components/ui/BoxPanel.vue'
import { byteSize as size } from '@/lib/bitmap'
import { useDomainsStore } from '@/stores/domains'
import { act } from '@/stores/log'
import { useStatusStore } from '@/stores/status'

const domains = useDomainsStore()
const st = useStatusStore()
</script>

<template>
  <BoxPanel title="Memory Domains">
    <div class="flex flex-wrap gap-1">
      <button
        class="btn"
        :disabled="!!st.needRom"
        :title="st.needRom"
        data-testid="domains-auto"
        @click="act(() => domains.autoSelect())"
      >
        Auto-select
      </button>
      <button
        class="btn"
        :disabled="!!st.needRom"
        :title="st.needRom"
        data-testid="domains-all"
        @click="act(() => domains.selectAll())"
      >
        Select all
      </button>
      <button
        class="btn"
        :disabled="!!st.needRom"
        :title="st.needRom"
        data-testid="domains-none"
        @click="act(() => domains.unselectAll())"
      >
        Unselect all
      </button>
    </div>
    <ul
      class="min-h-24 flex-1 overflow-auto border border-line bg-inset"
      role="listbox"
      aria-multiselectable="true"
      data-testid="domain-list"
    >
      <li v-if="!domains.domains.length" class="p-1 text-dim">no domains (load a ROM)</li>
      <li
        v-for="d in domains.domains"
        :key="d.name"
        role="option"
        :aria-selected="d.selected"
        tabindex="0"
        class="flex cursor-pointer items-center gap-2 px-1.5 py-0.5 hover:bg-panel"
        :class="{ 'row-sel': d.selected, 'text-dim': d.hidden }"
        :data-testid="`domain-${d.name}`"
        @click="act(() => domains.toggle(d.name))"
        @keydown.space.prevent="act(() => domains.toggle(d.name))"
      >
        <input type="checkbox" :checked="d.selected" tabindex="-1" class="pointer-events-none" />
        <span class="flex-1 truncate font-mono">
          {{ d.name }}<span v-if="d.hidden" class="ml-1 text-[11px]">(hidden)</span>
        </span>
        <span class="font-mono text-[11px] text-dim">
          {{ size(d.size) }} · w{{ d.wordSize }}{{ d.bigEndian ? ' BE' : ''
          }}{{ d.writable ? '' : ' RO' }}
        </span>
      </li>
    </ul>
  </BoxPanel>
</template>
