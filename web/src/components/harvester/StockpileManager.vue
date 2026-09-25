<script setup lang="ts">
import { computed, ref } from 'vue'
import BoxPanel from '@/components/ui/BoxPanel.vue'
import { useDialogStore, type MenuItem } from '@/stores/dialog'
import { act } from '@/stores/log'
import { EXPORT_URL, useStockpileStore } from '@/stores/stockpile'
import { useStatusStore } from '@/stores/status'
import { useUiStore } from '@/stores/ui'
import { clickSelect, useHarvester } from './useHarvester'

const sp = useStockpileStore()
const st = useStatusStore()
const ui = useUiStore()
const dlg = useDialogStore()
const gh = useHarvester()
const fileInput = ref<HTMLInputElement | null>(null)
const importMerge = ref(true)

const sel = computed(() => ui.stockpileSelection)
const single = computed(() => (sel.value.length === 1 ? sel.value[0]! : null))
const keys = computed(() => sp.keys.map((k) => k.key))

function select(key: string, ev: MouseEvent) {
  ui.ghSource = 'stockpile'
  ui.stockpileSelection = clickSelect(sel.value, keys.value, key, ev)
  const plain = !ev.ctrlKey && !ev.metaKey && !ev.shiftKey
  if (plain && ui.gh.loadOnSelect && !st.needRom) void gh.runStockpile(key)
}

function item(key: string) {
  return sp.keys.find((x) => x.key === key)
}

async function rename() {
  const key = single.value
  if (!key) return
  const name = await dlg.ask('Rename stockpile item', item(key)?.alias || key)
  if (name) await act(() => sp.update(key, { alias: name }))
}

async function editNote() {
  const key = single.value
  if (!key) return
  const note = await dlg.ask('Note', item(key)?.note ?? '')
  if (note !== null) await act(() => sp.update(key, { note }))
}

async function removeSelected() {
  for (const k of [...sel.value]) await act(() => sp.remove(k))
  ui.stockpileSelection = []
}

async function clearAll() {
  if (await dlg.confirm('Clear the stockpile?')) {
    await act(() => sp.clear(), 'stockpile cleared')
    ui.stockpileSelection = []
  }
}

async function loadPath() {
  const p = await dlg.ask('Load stockpile (.sks) from a path on the core host', sp.path)
  if (p) await act(() => sp.loadFrom(p), `stockpile loaded from ${p}`)
}

async function saveAs() {
  const p = await dlg.ask('Save stockpile (.sks) to a path on the core host', sp.path)
  if (p) await act(() => sp.saveTo(p), `stockpile saved to ${p}`)
}

async function save() {
  if (!sp.path) return saveAs()
  await act(() => sp.saveTo(sp.path), `stockpile saved to ${sp.path}`)
}

async function onFile(e: Event) {
  const input = e.target as HTMLInputElement
  const f = input.files?.[0]
  input.value = ''
  if (f) await act(() => sp.importFile(f, f.name, importMerge.value), `imported ${f.name}`)
}

function openEditor() {
  if (single.value) ui.openEditor({ kind: 'stockpile', key: single.value })
}

function menuItems(): MenuItem[] {
  return [
    {
      label: 'Open in Blast Editor',
      testid: 'stockpile-menu-edit',
      disabled: !single.value,
      action: openEditor,
    },
    { label: 'Rename', testid: 'stockpile-menu-rename', disabled: !single.value, action: rename },
    {
      label: 'Edit note',
      testid: 'stockpile-menu-note',
      disabled: !single.value,
      action: editNote,
    },
    {
      label: 'Remove',
      testid: 'stockpile-menu-remove',
      disabled: !sel.value.length,
      action: removeSelected,
    },
  ]
}

function onContext(key: string, ev: MouseEvent) {
  if (!sel.value.includes(key)) {
    ui.ghSource = 'stockpile'
    ui.stockpileSelection = [key]
  }
  dlg.openMenu(ev, menuItems())
}
</script>

<template>
  <BoxPanel title="Stockpile">
    <template #actions>
      <span class="truncate font-mono normal-case" :title="sp.path" data-testid="stockpile-path">
        {{ sp.path || 'unsaved' }} · {{ sp.keys.length }}
      </span>
    </template>
    <div class="flex flex-wrap gap-1">
      <button class="btn" data-testid="stockpile-load" @click="loadPath">Load</button>
      <button class="btn" data-testid="stockpile-save" @click="save">Save</button>
      <button class="btn" data-testid="stockpile-save-as" @click="saveAs">Save as</button>
      <button class="btn" data-testid="stockpile-import" @click="fileInput?.click()">
        Import .sks
      </button>
      <label class="flex items-center gap-1 text-dim" title="Append instead of replacing">
        <input v-model="importMerge" type="checkbox" data-testid="stockpile-import-merge" />
        merge
      </label>
      <input
        ref="fileInput"
        type="file"
        accept=".sks,application/zip"
        class="hidden"
        data-testid="stockpile-import-file"
        @change="onFile"
      />
      <a class="btn" :href="EXPORT_URL" download="stockpile.sks" data-testid="stockpile-export">
        Export .sks
      </a>
    </div>
    <div class="min-h-40 flex-1 overflow-auto border border-line bg-inset">
      <table class="w-full border-collapse text-left" data-testid="stockpile-table">
        <thead class="sticky top-0 bg-panel text-dim">
          <tr>
            <th class="px-1.5 font-normal">Item Name</th>
            <th class="px-1.5 font-normal">Game</th>
            <th class="px-1.5 font-normal">System</th>
            <th class="px-1.5 font-normal">Note</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="!sp.keys.length">
            <td colspan="4" class="p-1 text-dim">empty</td>
          </tr>
          <tr
            v-for="k in sp.keys"
            :key="k.key"
            tabindex="0"
            class="cursor-pointer hover:bg-panel"
            :class="{ 'row-sel': sel.includes(k.key) }"
            :aria-selected="sel.includes(k.key)"
            data-testid="stockpile-item"
            :data-key="k.key"
            @click="select(k.key, $event)"
            @keydown.enter="select(k.key, $event as unknown as MouseEvent)"
            @contextmenu="onContext(k.key, $event)"
          >
            <td class="truncate px-1.5">{{ k.alias || k.key }}</td>
            <td class="truncate px-1.5">{{ k.game.title }}</td>
            <td class="px-1.5 font-mono">{{ k.game.system }}</td>
            <td class="max-w-40 truncate px-1.5 text-dim" :title="k.note">{{ k.note }}</td>
          </tr>
        </tbody>
      </table>
    </div>
    <div class="flex flex-wrap gap-1">
      <button
        class="btn"
        :disabled="!sel.length"
        data-testid="stockpile-up"
        title="Move selected up"
        @click="act(() => sp.move(sel, -1))"
      >
        ▲
      </button>
      <button
        class="btn"
        :disabled="!sel.length"
        data-testid="stockpile-down"
        title="Move selected down"
        @click="act(() => sp.move(sel, 1))"
      >
        ▼
      </button>
      <button class="btn" :disabled="!single" data-testid="stockpile-edit" @click="openEditor">
        Blast Editor
      </button>
      <button class="btn" :disabled="!single" data-testid="stockpile-rename" @click="rename">
        Rename
      </button>
      <button class="btn" :disabled="!single" data-testid="stockpile-note" @click="editNote">
        Note
      </button>
      <button
        class="btn"
        :disabled="!sel.length"
        data-testid="stockpile-remove"
        @click="removeSelected"
      >
        Remove
      </button>
      <button
        class="btn"
        :disabled="!sp.keys.length"
        data-testid="stockpile-clear"
        @click="clearAll"
      >
        Clear
      </button>
    </div>
  </BoxPanel>
</template>
