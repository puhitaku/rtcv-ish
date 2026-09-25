<script setup lang="ts">
import { computed } from 'vue'
import BoxPanel from '@/components/ui/BoxPanel.vue'
import { useDialogStore, type MenuItem } from '@/stores/dialog'
import { act } from '@/stores/log'
import { useStashStore } from '@/stores/stash'
import { useStatusStore } from '@/stores/status'
import { useUiStore } from '@/stores/ui'
import { clickSelect, useHarvester } from './useHarvester'

const stash = useStashStore()
const st = useStatusStore()
const ui = useUiStore()
const dlg = useDialogStore()
const gh = useHarvester()

const sel = computed(() => ui.stashSelection)
const single = computed(() => (sel.value.length === 1 ? sel.value[0]! : null))
const keys = computed(() => stash.keys.map((k) => k.key))

function select(key: string, ev: MouseEvent) {
  ui.ghSource = 'stash'
  ui.stashSelection = clickSelect(sel.value, keys.value, key, ev)
  const plain = !ev.ctrlKey && !ev.metaKey && !ev.shiftKey
  if (plain && ui.gh.loadOnSelect && !st.needRom) void gh.runStash(key)
}

function step(dir: -1 | 1) {
  const n = keys.value.length
  if (!n) return
  const cur = single.value ? keys.value.indexOf(single.value) : dir > 0 ? -1 : n
  const key = keys.value[(((cur + dir) % n) + n) % n]!
  ui.ghSource = 'stash'
  ui.stashSelection = [key]
  if (ui.gh.loadOnSelect && !st.needRom) void gh.runStash(key)
}

function alias(key: string) {
  const k = stash.keys.find((x) => x.key === key)
  return k?.alias || key
}

async function toStockpile() {
  const key = single.value
  if (!key) return
  const name = await dlg.ask('Name for the stockpile item', alias(key))
  if (name === null) return
  const r = await act(() => stash.toStockpile(key, name || undefined), `sent to stockpile: ${name}`)
  if (r) {
    ui.stashSelection = []
    ui.ghSource = 'stockpile'
    ui.stockpileSelection = [r.key]
  }
}

async function rename() {
  const key = single.value
  if (!key) return
  const name = await dlg.ask('Rename stash item', alias(key))
  if (name) await act(() => stash.update(key, { alias: name }))
}

async function clearAll() {
  if (await dlg.confirm('Clear the stash history?')) {
    await act(() => stash.clear(), 'stash cleared')
    ui.stashSelection = []
  }
}

async function removeSelected() {
  for (const k of [...sel.value]) await act(() => stash.remove(k))
  ui.stashSelection = []
}

async function mergeSelected() {
  const k = await act(() => stash.merge([...sel.value]), 'merged')
  if (k) ui.stashSelection = [k.key]
}

function openEditor() {
  if (single.value) ui.openEditor({ kind: 'stash', key: single.value })
}

function menuItems(): MenuItem[] {
  return [
    {
      label: 'Open in Blast Editor',
      testid: 'stash-menu-edit',
      disabled: !single.value,
      action: openEditor,
    },
    { label: 'Rename', testid: 'stash-menu-rename', disabled: !single.value, action: rename },
    {
      label: 'Merge selected',
      testid: 'stash-menu-merge',
      disabled: sel.value.length < 2 || !!st.needRom,
      action: mergeSelected,
    },
    {
      label: 'Remove',
      testid: 'stash-menu-remove',
      disabled: !sel.value.length,
      action: removeSelected,
    },
  ]
}

function onContext(key: string, ev: MouseEvent) {
  if (!sel.value.includes(key)) {
    ui.ghSource = 'stash'
    ui.stashSelection = [key]
  }
  dlg.openMenu(ev, menuItems())
}
</script>

<template>
  <BoxPanel title="Stash History">
    <template #actions>
      <span class="font-mono normal-case" data-testid="stash-count">{{ stash.keys.length }}</span>
    </template>
    <ul
      class="min-h-40 flex-1 overflow-auto border border-line bg-inset"
      role="listbox"
      aria-multiselectable="true"
      data-testid="stash-list"
    >
      <li v-if="!stash.keys.length" class="p-1 text-dim">empty</li>
      <li
        v-for="k in stash.keys"
        :key="k.key"
        role="option"
        tabindex="0"
        :aria-selected="sel.includes(k.key)"
        class="flex cursor-pointer items-center gap-2 px-1.5 py-0.5 hover:bg-panel"
        :class="{ 'row-sel': sel.includes(k.key) }"
        data-testid="stash-item"
        :data-key="k.key"
        @click="select(k.key, $event)"
        @keydown.enter="select(k.key, $event as unknown as MouseEvent)"
        @contextmenu="onContext(k.key, $event)"
      >
        <span class="flex-1 truncate">{{ k.alias || k.key }}</span>
        <span class="font-mono text-[11px] text-dim">{{ k.unitCount }}u</span>
      </li>
    </ul>
    <div class="flex flex-wrap gap-1">
      <button class="btn" :disabled="!stash.keys.length" data-testid="stash-up" @click="step(-1)">
        ▲
      </button>
      <button class="btn" :disabled="!stash.keys.length" data-testid="stash-down" @click="step(1)">
        ▼
      </button>
      <button class="btn" :disabled="!single" data-testid="stash-to-stockpile" @click="toStockpile">
        To Stockpile
      </button>
      <button
        class="btn"
        :disabled="!stash.keys.length"
        data-testid="stash-clear"
        @click="clearAll"
      >
        Clear
      </button>
    </div>
    <div class="flex flex-wrap gap-1">
      <button class="btn" :disabled="!single" data-testid="stash-edit" @click="openEditor">
        Blast Editor
      </button>
      <button class="btn" :disabled="!single" data-testid="stash-rename" @click="rename">
        Rename
      </button>
      <button
        class="btn"
        :disabled="sel.length < 2 || !!st.needRom"
        :title="sel.length < 2 ? 'Ctrl-click to select 2+ items' : st.needRom"
        data-testid="stash-merge"
        @click="mergeSelected"
      >
        Merge
      </button>
      <button
        class="btn"
        :disabled="!sel.length"
        data-testid="stash-remove"
        @click="removeSelected"
      >
        Remove
      </button>
    </div>
  </BoxPanel>
</template>
