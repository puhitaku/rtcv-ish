<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import BoxPanel from '@/components/ui/BoxPanel.vue'
import { formatAddress, hex, hexRows, parseHex, wordToMemoryHex, type Group } from '@/lib/hex'
import { useDomainsStore } from '@/stores/domains'
import { act } from '@/stores/log'
import { useMemoryStore } from '@/stores/memory'
import { useStatusStore } from '@/stores/status'
import { useUiStore } from '@/stores/ui'

const VIEW = 256
const mem = useMemoryStore()
const domains = useDomainsStore()
const st = useStatusStore()
const ui = useUiStore()

const group = ref<Group>(1)
const addrText = ref(hex(mem.address))
const cursor = ref<number | null>(null)
const typed = ref('')
const auto = ref(false)
const shot = ref(0)
const shotError = ref(false)

const domain = computed(() => domains.byName(mem.domain))
const size = computed(() => {
  const d = domain.value
  return d ? Math.max(0, Math.min(VIEW, d.size - mem.address)) : 0
})
const rows = computed(() =>
  hexRows(mem.data, mem.address, group.value, domain.value?.bigEndian ?? false),
)
const cursorCell = computed(() => {
  if (cursor.value == null) return null
  const off = cursor.value - (cursor.value % group.value)
  return { offset: off, size: Math.min(group.value, mem.data.length - off) }
})

async function refresh() {
  if (!mem.domain || st.needRom || !size.value) return
  await act(() => mem.read(size.value))
}

function go() {
  const a = parseHex(addrText.value)
  if (a == null) return
  const max = Math.max(0, (domain.value?.size ?? 0) - 1)
  mem.address = Math.min(a - (a % 16), max - (max % 16))
  addrText.value = hex(mem.address)
  cursor.value = null
  void refresh()
}

function page(dir: -1 | 1) {
  const next = mem.address + dir * VIEW
  if (next < 0 || next >= (domain.value?.size ?? 0)) return
  mem.address = next
  addrText.value = hex(next)
  cursor.value = null
  void refresh()
}

watch(
  () => mem.domain,
  () => {
    mem.address = 0
    addrText.value = '0'
    mem.data = new Uint8Array()
    cursor.value = null
    void refresh()
  },
)

watch(
  () => domains.domains,
  (ds) => {
    if (!ds.some((d) => d.name === mem.domain)) mem.domain = ds[0]?.name ?? ''
  },
  { immediate: true },
)

function consumeTarget() {
  const t = ui.memoryTarget
  if (!t) return
  ui.memoryTarget = null
  mem.domain = t.domain
  addrText.value = hex(t.address)
  // Let the domain watcher reset first.
  setTimeout(() => {
    go()
    cursor.value = t.address - mem.address
  })
}
watch(() => ui.memoryTarget, consumeTarget)

function select(offset: number) {
  cursor.value = offset
  typed.value = ''
}

async function onKey(e: KeyboardEvent) {
  if (cursor.value == null) return
  const cell = cursorCell.value!
  const move = (d: number) => {
    const n = cell.offset + d
    if (n >= 0 && n < mem.data.length) select(n)
    e.preventDefault()
  }
  if (e.key === 'ArrowRight') return move(group.value)
  if (e.key === 'ArrowLeft') return move(-group.value)
  if (e.key === 'ArrowDown') return move(16)
  if (e.key === 'ArrowUp') return move(-16)
  if (e.key === 'Escape') {
    typed.value = ''
    return
  }
  if (!/^[0-9a-fA-F]$/.test(e.key)) return
  e.preventDefault()
  typed.value += e.key.toUpperCase()
  if (typed.value.length < cell.size * 2) return
  const data = wordToMemoryHex(typed.value, cell.size, domain.value?.bigEndian ?? false)
  typed.value = ''
  const addr = mem.address + cell.offset
  await act(() => mem.write(addr, data))
  await refresh()
  move(cell.size)
}

const cursorFrozen = computed(() =>
  cursorCell.value ? mem.frozenAt(mem.domain, mem.address + cursorCell.value.offset) : undefined,
)

async function toggleFreeze() {
  const c = cursorCell.value
  if (!c) return
  const addr = mem.address + c.offset
  if (cursorFrozen.value) await act(() => mem.unfreeze(addr), `unfrozen ${hex(addr)}`)
  else await act(() => mem.freeze(addr, c.size), `frozen ${hex(addr)} (${c.size} bytes)`)
}

function isFrozen(offset: number) {
  return !!mem.frozenAt(mem.domain, mem.address + offset)
}

let timer: ReturnType<typeof setInterval> | undefined
watch(auto, (on) => {
  clearInterval(timer)
  if (on) timer = setInterval(() => void refresh(), 1000)
})
onMounted(() => {
  consumeTarget()
  void refresh()
})
onBeforeUnmount(() => clearInterval(timer))

const shotUrl = computed(() => `/api/emulator/screenshot?t=${shot.value}`)
function refreshShot() {
  shotError.value = false
  shot.value = Date.now()
}
</script>

<template>
  <div class="flex flex-wrap items-start gap-2" data-testid="memory-panel">
    <BoxPanel title="Memory" class="min-w-[40rem] flex-1">
      <div class="flex flex-wrap items-center gap-2">
        <select v-model="mem.domain" class="input" data-testid="mem-domain">
          <option v-if="!domains.domains.length" value="">(no domains)</option>
          <option v-for="d in domains.domains" :key="d.name" :value="d.name">{{ d.name }}</option>
        </select>
        <form class="flex items-center gap-1" @submit.prevent="go">
          <span class="lbl">Address</span>
          <input v-model="addrText" class="input w-24 font-mono" data-testid="mem-address" />
          <button class="btn" type="submit" data-testid="mem-go">Go</button>
        </form>
        <button class="btn" data-testid="mem-prev" @click="page(-1)">◀</button>
        <button class="btn" data-testid="mem-next" @click="page(1)">▶</button>
        <label class="flex items-center gap-1">
          <span class="lbl">Group</span>
          <select v-model.number="group" class="input" data-testid="mem-group">
            <option :value="1">1</option>
            <option :value="2">2</option>
            <option :value="4">4</option>
          </select>
        </label>
        <button
          class="btn"
          :disabled="!!st.needRom"
          :title="st.needRom"
          data-testid="mem-refresh"
          @click="refresh"
        >
          Refresh
        </button>
        <label class="flex items-center gap-1">
          <input v-model="auto" type="checkbox" data-testid="mem-auto" />
          auto (1 Hz)
        </label>
        <button
          class="btn"
          :class="{ 'btn-on': !!cursorFrozen }"
          :disabled="!cursorCell || !!st.needRom"
          :title="
            st.needRom ||
            (!cursorCell
              ? 'Select a cell'
              : cursorFrozen
                ? 'Unfreeze (clears scheduled units and re-applies other freezes)'
                : 'Freeze the value at the cursor')
          "
          data-testid="mem-freeze"
          @click="toggleFreeze"
        >
          {{ cursorFrozen ? 'Unfreeze' : 'Freeze' }}
        </button>
      </div>
      <div
        class="overflow-auto border border-line bg-inset p-1 font-mono outline-none focus:border-accent"
        tabindex="0"
        data-testid="hex-view"
        @keydown="onKey"
      >
        <div v-if="!rows.length" class="text-dim">
          {{ st.needRom || (mem.domain ? 'press Refresh' : 'no domain') }}
        </div>
        <div v-for="r in rows" :key="r.address" class="flex gap-3 whitespace-pre">
          <span class="text-dim">{{ formatAddress(r.address, domain?.size ?? 0) }}</span>
          <span class="flex gap-1">
            <span
              v-for="c in r.cells"
              :key="c.offset"
              class="cursor-pointer px-0.5"
              :class="{
                'bg-accent text-accent-fg': cursorCell?.offset === c.offset,
                'text-warn': isFrozen(c.offset) && cursorCell?.offset !== c.offset,
              }"
              :data-testid="`hex-cell-${c.offset}`"
              @click="select(c.offset)"
              >{{
                cursorCell?.offset === c.offset && typed ? typed.padEnd(c.size * 2, '_') : c.text
              }}</span
            >
          </span>
          <span class="text-dim">{{ r.ascii }}</span>
        </div>
      </div>
      <div class="text-[11px] text-dim">
        Click a cell and type hex digits to write. Arrows move. Frozen cells are highlighted.
      </div>
    </BoxPanel>

    <BoxPanel title="Screen" class="w-80">
      <button
        class="btn"
        :disabled="!!st.needRom"
        :title="st.needRom"
        data-testid="screenshot-refresh"
        @click="refreshShot"
      >
        Refresh screenshot
      </button>
      <img
        v-if="shot && !shotError"
        :src="shotUrl"
        alt="Emulator screenshot"
        class="w-full border border-line [image-rendering:pixelated]"
        data-testid="screenshot"
        @error="shotError = true"
      />
      <div v-else-if="shotError" class="text-err">screenshot unavailable</div>
    </BoxPanel>
  </div>
</template>
