<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { Domain } from '@/api/types'
import { readWords } from '@/api/memory'
import { errorMessage } from '@/api/client'
import {
  STRIDES,
  autoStride,
  byteSize,
  bitmapSize,
  pixelAddress,
  pixelAtPoint,
  rgb565ToRgba,
  wordAt,
} from '@/lib/bitmap'
import { formatAddress, hex } from '@/lib/hex'
import { useMemoryStore } from '@/stores/memory'
import { useStatusStore } from '@/stores/status'

const props = defineProps<{ domain: Domain }>()

const mem = useMemoryStore()
const st = useStatusStore()

const name = computed(() => props.domain.name)
const collapsed = computed({
  get: () => mem.bitmaps.collapsed[name.value] ?? props.domain.hidden,
  set: (v: boolean) => (mem.bitmaps.collapsed[name.value] = v),
})
const auto = computed(() => autoStride(props.domain.size))
const stride = computed({
  get: () => mem.bitmaps.stride[name.value] ?? auto.value,
  set: (v: number) => {
    if (v === auto.value) delete mem.bitmaps.stride[name.value]
    else mem.bitmaps.stride[name.value] = v
  },
})
const strides = computed(() => [...new Set([...STRIDES, auto.value])].sort((a, b) => a - b))
const geom = computed(() => bitmapSize(props.domain.size, stride.value))

const canvas = ref<HTMLCanvasElement | null>(null)
/** Words of the last fetch; not reactive, the canvas shows them. */
let words: Uint8Array = new Uint8Array()
let wordsStride = 0
const loaded = ref(false)
const error = ref('')
let inflight: Promise<void> | null = null

function draw() {
  const c = canvas.value
  const ctx = c?.getContext('2d')
  if (!c || !ctx) return
  const { width, height } = geom.value
  const img = ctx.createImageData(width, height)
  rgb565ToRgba(words, img.data)
  ctx.putImageData(img, 0, 0)
}

async function fetchWords() {
  const s = stride.value
  const size = props.domain.size
  try {
    const w = await readWords(name.value, 0, size, s)
    if (s !== stride.value || size !== props.domain.size) return
    words = w
    wordsStride = s
    loaded.value = true
    error.value = ''
    draw()
  } catch (e) {
    error.value = errorMessage(e)
  }
}

/** Fetches and redraws unless collapsed; concurrent calls share one fetch. */
function refresh(): Promise<void> {
  if (collapsed.value || st.needRom || props.domain.size < 2) return Promise.resolve()
  inflight ??= fetchWords().finally(() => (inflight = null))
  return inflight
}
defineExpose({ refresh })

watch([collapsed, stride], async () => {
  loaded.value = false
  hover.value = null
  await nextTick()
  if (inflight) await inflight
  void refresh()
})
watch(
  () => props.domain,
  () => void refresh(),
)
onMounted(() => void refresh())

// ---- Hover and click ----

const hover = ref<{ index: number; x: number; y: number } | null>(null)
const tip = computed(() => {
  const h = hover.value
  if (!h) return null
  const v = wordsStride === stride.value ? wordAt(words, h.index) : null
  return {
    address: formatAddress(pixelAddress(h.index, stride.value), props.domain.size),
    value: v == null ? '----' : hex(v, 4),
  }
})

function pixelAt(e: MouseEvent): number | null {
  const c = canvas.value
  return c ? pixelAtPoint(e.clientX, e.clientY, c.getBoundingClientRect(), geom.value) : null
}

function onMove(e: MouseEvent) {
  const i = pixelAt(e)
  hover.value = i == null ? null : { index: i, x: e.clientX, y: e.clientY }
}

const flash = ref<number | null>(null)
let flashTimer: ReturnType<typeof setTimeout> | undefined
const flashStyle = computed(() => {
  if (flash.value == null) return undefined
  const { width, height } = geom.value
  const x = flash.value % width
  const y = Math.floor(flash.value / width)
  return {
    left: `${(x / width) * 100}%`,
    top: `${(y / height) * 100}%`,
    width: `${100 / width}%`,
    height: `${100 / height}%`,
  }
})

function onClick(e: MouseEvent) {
  const i = pixelAt(e)
  if (i == null) return
  mem.jump(name.value, pixelAddress(i, stride.value))
  flash.value = i
  clearTimeout(flashTimer)
  flashTimer = setTimeout(() => (flash.value = null), 1200)
}
onBeforeUnmount(() => clearTimeout(flashTimer))
</script>

<template>
  <div class="flex flex-col gap-1" :data-testid="`bitmap-${domain.name}`">
    <div class="flex flex-wrap items-center gap-2">
      <button
        class="flex items-center gap-1 font-mono"
        :aria-expanded="!collapsed"
        :data-testid="`bitmap-toggle-${domain.name}`"
        @click="collapsed = !collapsed"
      >
        <span class="w-3 text-dim">{{ collapsed ? '▸' : '▾' }}</span>
        <span>{{ domain.name }}</span>
      </button>
      <span class="text-dim" :title="`${hex(domain.size)}h bytes`">{{
        byteSize(domain.size)
      }}</span>
      <span v-if="domain.hidden" class="text-dim">(hidden)</span>
      <label class="ml-auto flex items-center gap-1">
        <span class="lbl">Stride</span>
        <select
          v-model.number="stride"
          class="input"
          :title="`Each pixel is one 16-bit word standing for ${2 * stride} bytes`"
          :data-testid="`bitmap-stride-${domain.name}`"
        >
          <option v-for="s in strides" :key="s" :value="s">
            {{ s }}{{ s === auto ? ' (auto)' : '' }}
          </option>
        </select>
      </label>
      <span class="font-mono text-dim">{{ geom.width }}×{{ geom.height }}</span>
      <span v-if="error" class="text-err">{{ error }}</span>
    </div>
    <div v-if="!collapsed" class="relative border border-line bg-inset">
      <canvas
        ref="canvas"
        :width="geom.width"
        :height="geom.height"
        class="block h-auto w-full cursor-crosshair [image-rendering:pixelated]"
        :data-testid="`bitmap-canvas-${domain.name}`"
        @mousemove="onMove"
        @mouseleave="hover = null"
        @click="onClick"
      />
      <div
        v-if="flashStyle"
        class="pointer-events-none absolute min-h-1.5 min-w-1.5 outline-2 outline-accent"
        :style="flashStyle"
        data-testid="bitmap-flash"
      />
      <div
        v-if="!loaded"
        class="pointer-events-none absolute inset-0 flex items-center justify-center text-dim"
      >
        {{ st.needRom || (error ? '' : 'loading…') }}
      </div>
    </div>
    <Teleport to="body">
      <div
        v-if="hover && tip"
        class="pointer-events-none fixed z-50 border border-line bg-panel px-1 font-mono text-[11px]"
        :style="{ left: `${hover.x + 14}px`, top: `${hover.y + 14}px` }"
        data-testid="bitmap-tip"
      >
        <span class="text-dim">{{ domain.name }}</span> {{ tip.address }}
        <span class="text-dim">=</span> {{ tip.value }}
      </div>
    </Teleport>
  </div>
</template>
