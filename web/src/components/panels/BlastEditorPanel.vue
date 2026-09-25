<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import UnitProperties from '@/components/editor/UnitProperties.vue'
import { COLUMNS, DEFAULT_COLUMNS, type ColumnId } from '@/components/editor/columns'
import { SHIFT_FIELDS, parseLayerFile, type ShiftField } from '@/lib/layer'
import { loadJSON, save as saveLocal } from '@/lib/storage'
import { useDialogStore } from '@/stores/dialog'
import { useDomainsStore } from '@/stores/domains'
import { useEditorStore } from '@/stores/editor'
import { act, useLogStore } from '@/stores/log'
import { useStashStore } from '@/stores/stash'
import { useStatusStore } from '@/stores/status'
import { useStockpileStore } from '@/stores/stockpile'
import { useUiStore } from '@/stores/ui'
import { useUnitsStore } from '@/stores/units'

const ed = useEditorStore()
const ui = useUiStore()
const st = useStatusStore()
const stash = useStashStore()
const stockpile = useStockpileStore()
const units = useUnitsStore()
const domains = useDomainsStore()
const dlg = useDialogStore()
const log = useLogStore()

const MAX_ROWS = 2000

// Open the requested target when it changes.
watch(
  () => ui.editorTarget,
  (t) => {
    if (!t) {
      if (!ed.target) ed.target = { kind: 'new' }
      return
    }
    const cur = ed.target
    const same =
      cur && cur.kind === t.kind && (t.kind === 'new' || ('key' in cur && cur.key === t.key))
    if (!same) void act(() => ed.open(t))
  },
  { immediate: true },
)

const COLS_KEY = 'rtcvish.editorColumns'
const visibleIds = ref<ColumnId[]>(loadJSON(COLS_KEY, { ids: DEFAULT_COLUMNS }).ids)
watch(visibleIds, (v) => saveLocal(COLS_KEY, { ids: v }), { deep: true })
const columns = computed(() => COLUMNS.filter((c) => visibleIds.value.includes(c.id)))
const chooser = ref(false)

function toggleColumn(id: ColumnId) {
  visibleIds.value = visibleIds.value.includes(id)
    ? visibleIds.value.filter((c) => c !== id)
    : COLUMNS.map((c) => c.id).filter((c) => c === id || visibleIds.value.includes(c))
}

const filterCol = ref<ColumnId | 'any'>('any')
const filterText = ref('')

/** Indices of units passing the filter. */
const rows = computed(() => {
  const q = filterText.value.trim().toLowerCase()
  const out: number[] = []
  ed.layer.units.forEach((u, i) => {
    if (q) {
      const cols =
        filterCol.value === 'any' ? COLUMNS : COLUMNS.filter((c) => c.id === filterCol.value)
      if (!cols.some((c) => c.text(u).toLowerCase().includes(q))) return
    }
    out.push(i)
  })
  return out
})
const shownRows = computed(() => rows.value.slice(0, MAX_ROWS))
const selected = computed(() => new Set(ed.selection))

let anchor: number | null = null
function clickRow(i: number, ev: MouseEvent) {
  if (ev.shiftKey && anchor != null) {
    const a = rows.value.indexOf(anchor)
    const b = rows.value.indexOf(i)
    if (a >= 0 && b >= 0) {
      ed.selection = rows.value.slice(Math.min(a, b), Math.max(a, b) + 1)
      return
    }
  }
  anchor = i
  if (ev.ctrlKey || ev.metaKey) {
    ed.selection = selected.value.has(i)
      ? ed.selection.filter((x) => x !== i)
      : [...ed.selection, i].sort((a, b) => a - b)
  } else {
    ed.selection = [i]
  }
}

function selectAllRows() {
  ed.selection = [...rows.value]
}

function toggleFlag(i: number, k: 'enabled' | 'locked') {
  const next = ed.layer.units.map((u, j) => (j === i ? { ...u, [k]: !u[k] } : u))
  ed.replace({ ...ed.layer, units: next })
}

const shiftField = ref<ShiftField>('address')
const shiftAmount = ref(1)

const t = computed(() => ed.target)
const keyed = computed(() => (t.value && t.value.kind !== 'new' ? t.value : null))
const title = computed(() => {
  const x = t.value
  if (!x) return 'no layer'
  if (x.kind === 'new') return 'new layer'
  const list = x.kind === 'stash' ? stash.keys : stockpile.keys
  const k = list.find((k) => k.key === x.key)
  return `${x.kind}: ${k?.alias || x.key}`
})

async function saveBack() {
  await act(() => ed.save(), 'layer saved')
}

async function loadAndCorrupt() {
  const k = keyed.value
  if (!k) return
  await act(async () => {
    if (ed.dirty) await ed.save()
    if (k.kind === 'stash') await stash.run(k.key)
    else await stockpile.run(k.key)
  }, 'loaded and corrupted')
}

async function apply() {
  await act(() => units.apply(ed.layer, true), `applied ${ed.layer.units.length} units`)
}

async function sendToStash() {
  const k = keyed.value
  if (k?.kind !== 'stash') return
  await act(() => ed.save(), 'layer saved to stash')
}

async function toStockpile() {
  const k = keyed.value
  if (k?.kind !== 'stash') return
  const cur = stash.keys.find((x) => x.key === k.key)
  const name = await dlg.ask('Name for the stockpile item', cur?.alias || k.key)
  if (name === null) return
  const r = await act(async () => {
    if (ed.dirty) await ed.save()
    return stash.toStockpile(k.key, name || undefined)
  }, `sent to stockpile: ${name}`)
  if (r) {
    ed.target = { kind: 'stockpile', key: r.key }
    ui.editorTarget = ed.target
  }
}

const fileInput = ref<HTMLInputElement | null>(null)
async function onFile(e: Event) {
  const input = e.target as HTMLInputElement
  const f = input.files?.[0]
  input.value = ''
  if (!f) return
  try {
    ed.replace(parseLayerFile(await f.text()), false)
    log.add('info', `loaded ${f.name} (${ed.layer.units.length} units)`)
  } catch (err) {
    log.error(err, `load ${f.name}`)
  }
}

function saveBl() {
  const blob = new Blob([JSON.stringify(ed.layer, null, 2)], { type: 'application/json' })
  const a = document.createElement('a')
  a.href = URL.createObjectURL(blob)
  a.download = `${title.value.replace(/[^\w.-]+/g, '_')}.bl`
  a.click()
  setTimeout(() => URL.revokeObjectURL(a.href), 1000)
}

function newLayer() {
  ui.openEditor({ kind: 'new' })
}

function openHex() {
  const u = ed.selectedUnits[0]
  if (u) ui.openMemory(u.domain, u.address)
}

const needKey = computed(() => (keyed.value ? '' : 'Only for a stash or stockpile item'))
const needStash = computed(() => (keyed.value?.kind === 'stash' ? '' : 'Only for a stash item'))

function onKey(e: KeyboardEvent) {
  if ((e.ctrlKey || e.metaKey) && e.key === 'a') {
    e.preventDefault()
    selectAllRows()
  } else if (e.key === 'Delete') {
    ed.removeSelected()
  }
}
</script>

<template>
  <div class="flex h-full min-h-0 flex-col gap-2" data-testid="editor-panel">
    <div class="flex flex-wrap items-center gap-2">
      <span class="font-semibold" data-testid="be-title">{{ title }}</span>
      <span v-if="ed.dirty" class="text-warn" data-testid="be-dirty">modified</span>
      <span class="font-mono text-dim" data-testid="be-size">
        Layer size: {{ ed.layer.units.length }}
      </span>
      <span class="flex-1" />
      <button class="btn" data-testid="be-new" @click="newLayer">New</button>
      <button class="btn" data-testid="be-load-bl" @click="fileInput?.click()">Load .bl</button>
      <input
        ref="fileInput"
        type="file"
        accept=".bl,application/json"
        class="hidden"
        data-testid="be-load-bl-file"
        @change="onFile"
      />
      <button class="btn" data-testid="be-save-bl" @click="saveBl">Save .bl</button>
      <button
        class="btn"
        :disabled="!keyed"
        :title="needKey"
        data-testid="be-revert"
        @click="keyed && act(() => ed.open(keyed!))"
      >
        Revert
      </button>
      <button
        class="btn btn-accent"
        :disabled="!keyed || !ed.dirty"
        :title="needKey || (!ed.dirty ? 'No changes' : 'Write the layer back')"
        data-testid="be-save"
        @click="saveBack"
      >
        Save
      </button>
    </div>

    <div class="flex flex-wrap items-center gap-1">
      <button class="btn" data-testid="be-disable50" @click="ed.disable50()">Disable 50%</button>
      <button class="btn" data-testid="be-invert" @click="ed.invertDisabled()">
        Invert Disabled
      </button>
      <button class="btn" data-testid="be-remove-disabled" @click="ed.removeDisabled()">
        Remove Disabled
      </button>
      <button class="btn" data-testid="be-enable-all" @click="ed.enableAll()">Enable all</button>
      <button class="btn" data-testid="be-disable-all" @click="ed.disableAll()">Disable all</button>
      <button
        class="btn"
        :disabled="!ed.selection.length"
        data-testid="be-remove-selected"
        @click="ed.removeSelected()"
      >
        Remove selected
      </button>
      <button
        class="btn"
        :disabled="!ed.selection.length"
        data-testid="be-duplicate"
        @click="ed.duplicate()"
      >
        Duplicate
      </button>
      <button
        class="btn"
        data-testid="be-add"
        @click="ed.addRow(domains.selected[0] ?? domains.domains[0]?.name ?? '')"
      >
        Add row
      </button>
      <span class="ml-2 flex items-center gap-1">
        <span class="lbl">Shift</span>
        <select v-model="shiftField" class="input" data-testid="be-shift-field">
          <option v-for="f in SHIFT_FIELDS" :key="f" :value="f">{{ f }}</option>
        </select>
        <input
          v-model.number="shiftAmount"
          type="number"
          min="0"
          class="input w-16 font-mono"
          data-testid="be-shift-amount"
        />
        <button
          class="btn"
          title="Add to selected (all when none selected)"
          data-testid="be-shift-up"
          @click="ed.shift(shiftField, Math.abs(shiftAmount || 0))"
        >
          ▲
        </button>
        <button
          class="btn"
          title="Subtract from selected (all when none selected)"
          data-testid="be-shift-down"
          @click="ed.shift(shiftField, -Math.abs(shiftAmount || 0))"
        >
          ▼
        </button>
      </span>
    </div>

    <div class="flex flex-wrap items-center gap-1">
      <button
        class="btn btn-accent"
        :disabled="!keyed || !!st.needRom"
        :title="needKey || st.needRom"
        data-testid="be-load-corrupt"
        @click="loadAndCorrupt"
      >
        Load + Corrupt
      </button>
      <button
        class="btn btn-accent"
        :disabled="!!st.needRom"
        :title="st.needRom"
        data-testid="be-apply"
        @click="apply"
      >
        Apply Corruption
      </button>
      <button
        class="btn"
        :disabled="!!needStash"
        :title="needStash"
        data-testid="be-send-stash"
        @click="sendToStash"
      >
        Send to Stash
      </button>
      <button
        class="btn"
        :disabled="!!needStash"
        :title="needStash"
        data-testid="be-to-stockpile"
        @click="toStockpile"
      >
        To Stockpile
      </button>
      <button
        class="btn"
        :disabled="!!st.needRom"
        :title="st.needRom || 'Selected units (all when none selected)'"
        data-testid="be-bake"
        @click="act(() => ed.bake(), 'baked to VALUE')"
      >
        Bake to VALUE
      </button>
      <button class="btn" data-testid="be-breakdown" @click="ed.breakDown()">Break down</button>
      <button class="btn" data-testid="be-sanitize" @click="ed.sanitize()">
        Sanitize duplicates
      </button>
      <button
        class="btn"
        :disabled="ed.selection.length !== 1"
        data-testid="be-open-hex"
        @click="openHex"
      >
        Open in Memory
      </button>
    </div>

    <div class="flex flex-wrap items-center gap-1">
      <span class="lbl">Filter</span>
      <select v-model="filterCol" class="input" data-testid="be-filter-column">
        <option value="any">any column</option>
        <option v-for="c in COLUMNS" :key="c.id" :value="c.id">{{ c.label }}</option>
      </select>
      <input
        v-model="filterText"
        class="input w-48"
        placeholder="text"
        data-testid="be-filter-text"
      />
      <span class="font-mono text-dim" data-testid="be-filter-count">
        {{ rows.length }} shown · {{ ed.selection.length }} selected
      </span>
      <span class="flex-1" />
      <div class="relative">
        <button class="btn" data-testid="be-columns" @click="chooser = !chooser">Columns</button>
        <div
          v-if="chooser"
          class="box absolute top-full right-0 z-30 mt-1 grid w-80 grid-cols-2 gap-x-2 p-2 shadow"
          data-testid="be-columns-popup"
        >
          <label v-for="c in COLUMNS" :key="c.id" class="flex items-center gap-1">
            <input
              type="checkbox"
              :checked="visibleIds.includes(c.id)"
              :data-testid="`be-col-${c.id}`"
              @change="toggleColumn(c.id)"
            />
            {{ c.label }}
          </label>
        </div>
      </div>
    </div>

    <div class="flex min-h-0 flex-1 gap-2">
      <div
        class="min-h-48 min-w-0 flex-1 overflow-auto border border-line bg-inset"
        tabindex="0"
        @keydown="onKey"
      >
        <table class="w-full border-collapse text-left whitespace-nowrap" data-testid="be-table">
          <thead class="sticky top-0 bg-panel text-dim">
            <tr>
              <th class="px-1 font-normal">#</th>
              <th v-for="c in columns" :key="c.id" class="px-1.5 font-normal">{{ c.label }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="!ed.layer.units.length">
              <td :colspan="columns.length + 1" class="p-1 text-dim">no units</td>
            </tr>
            <tr
              v-for="i in shownRows"
              :key="i"
              class="cursor-pointer hover:bg-panel"
              :class="{
                'row-sel': selected.has(i),
                'text-dim line-through': !ed.layer.units[i]!.enabled,
              }"
              data-testid="be-row"
              :data-index="i"
              :data-enabled="ed.layer.units[i]!.enabled"
              @click="clickRow(i, $event)"
            >
              <td class="px-1 font-mono text-dim">{{ i }}</td>
              <td v-for="c in columns" :key="c.id" class="px-1.5" :class="{ 'font-mono': c.mono }">
                <input
                  v-if="c.id === 'enabled' || c.id === 'locked'"
                  type="checkbox"
                  :checked="ed.layer.units[i]![c.id]"
                  :data-testid="`be-row-${c.id}`"
                  @click.stop="toggleFlag(i, c.id)"
                />
                <template v-else>{{ c.text(ed.layer.units[i]!) }}</template>
              </td>
            </tr>
            <tr v-if="rows.length > MAX_ROWS">
              <td :colspan="columns.length + 1" class="p-1 text-dim">
                {{ rows.length - MAX_ROWS }} more rows (use the filter)
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <UnitProperties />
    </div>
  </div>
</template>
