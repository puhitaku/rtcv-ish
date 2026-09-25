<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { Settings } from '@/api/types'
import { hex, parseHex } from '@/lib/hex'
import { useSettingsPatch } from './useSettingsPatch'

/** GENERAL PARAMETERS "Address range": limits generation to [start, end) of each domain. */
const props = defineProps<{ range: Settings['addressRange'] }>()
const patch = useSettingsPatch()

const start = ref('')
const end = ref('')
watch(
  () => props.range,
  (r) => {
    start.value = hex(r.start)
    end.value = hex(r.end)
  },
  { immediate: true, deep: true },
)

const parsed = computed(() => ({ start: parseHex(start.value), end: parseHex(end.value) }))
const err = computed(() => {
  const { start: s, end: e } = parsed.value
  if (s == null) return 'start is not a hex number'
  if (e == null) return 'end is not a hex number'
  if (s >= e) return 'start must be below end'
  return ''
})
const hint = computed(() => {
  if (err.value) return err.value
  const n = parsed.value.end! - parsed.value.start!
  const size = `0x${hex(n)} bytes (${n.toLocaleString('en-US')})`
  return props.range.enabled
    ? `${size}, clipped to each domain`
    : `off: whole domains (range ${size})`
})

function commit() {
  if (err.value) return
  const s = parsed.value.start!
  const e = parsed.value.end!
  if (s !== props.range.start || e !== props.range.end)
    void patch({ addressRange: { start: s, end: e } })
}
</script>

<template>
  <div class="flex flex-col gap-1" data-testid="address-range">
    <label class="flex items-center gap-1">
      <input
        type="checkbox"
        :checked="range.enabled"
        data-testid="address-range-enabled"
        @change="patch({ addressRange: { enabled: ($event.target as HTMLInputElement).checked } })"
      />
      <span class="lbl">Address range</span>
    </label>
    <div class="flex items-center gap-1 font-mono">
      <span class="text-dim">0x</span>
      <input
        v-model="start"
        class="input w-24 font-mono"
        :class="{ invalid: !!err }"
        :disabled="!range.enabled"
        aria-label="Address range start (hex)"
        title="Start (hex, inclusive)"
        data-testid="address-range-start"
        @change="commit"
      />
      <span class="text-dim">– 0x</span>
      <input
        v-model="end"
        class="input w-24 font-mono"
        :class="{ invalid: !!err }"
        :disabled="!range.enabled"
        aria-label="Address range end (hex)"
        title="End (hex, exclusive)"
        data-testid="address-range-end"
        @change="commit"
      />
    </div>
    <span
      class="text-[11px]"
      :class="err ? 'text-err' : 'text-dim'"
      data-testid="address-range-hint"
      >{{ hint }}</span
    >
  </div>
</template>
