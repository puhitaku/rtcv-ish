<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { PrecisionRanges } from '@/api/types'
import { maxForPrecision, normalizeDecimal, validateRange } from '@/lib/decimal'
import { hex } from '@/lib/hex'

/** Min/max for the current precision, as unsigned decimal strings. */
const props = defineProps<{ ranges: PrecisionRanges; precision: number; testid: string }>()
const emit = defineEmits<{ save: [v: { min: string; max: string }] }>()

type P = '1' | '2' | '4' | '8'
const cur = computed(() => props.ranges[String(props.precision) as P])
const min = ref('')
const max = ref('')
watch(
  cur,
  (r) => {
    min.value = r?.min ?? '0'
    max.value = r?.max ?? '0'
  },
  { immediate: true },
)
const err = computed(() => validateRange(min.value.trim(), max.value.trim(), props.precision))

function commit() {
  if (err.value) return
  const v = { min: normalizeDecimal(min.value.trim()), max: normalizeDecimal(max.value.trim()) }
  if (v.min !== cur.value?.min || v.max !== cur.value?.max) emit('save', v)
}

function hexOf(s: string) {
  try {
    return '0x' + hex(BigInt(s.trim()))
  } catch {
    return ''
  }
}
</script>

<template>
  <div class="grid grid-cols-[6rem_1fr] items-center gap-1">
    <span class="lbl">Minimum ({{ precision * 8 }}-bit)</span>
    <input
      v-model="min"
      class="input font-mono"
      :class="{ invalid: !!err }"
      :title="hexOf(min)"
      :data-testid="`${testid}-min`"
      @change="commit"
    />
    <span class="lbl">Maximum</span>
    <input
      v-model="max"
      class="input font-mono"
      :class="{ invalid: !!err }"
      :title="hexOf(max)"
      :data-testid="`${testid}-max`"
      @change="commit"
    />
    <span />
    <span class="text-[11px]" :class="err ? 'text-err' : 'text-dim'">
      {{ err || `0 .. ${maxForPrecision(precision)}` }}
    </span>
  </div>
</template>
