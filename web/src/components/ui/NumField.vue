<script setup lang="ts">
import { ref, watch } from 'vue'

const props = withDefaults(
  defineProps<{ modelValue: number; min?: number; max?: number; testid: string; width?: string }>(),
  { min: 0, max: Number.MAX_SAFE_INTEGER, width: 'w-20' },
)
const emit = defineEmits<{ 'update:modelValue': [v: number] }>()

const draft = ref(String(props.modelValue))
const invalid = ref(false)
watch(
  () => props.modelValue,
  (v) => {
    draft.value = String(v)
    invalid.value = false
  },
)

function commit() {
  const t = draft.value.trim()
  const n = Number(t)
  if (!/^-?\d+$/.test(t) || n < props.min || n > props.max) {
    invalid.value = true
    return
  }
  invalid.value = false
  if (n !== props.modelValue) emit('update:modelValue', n)
}
</script>

<template>
  <input
    v-model="draft"
    class="input font-mono"
    :class="[width, { invalid }]"
    inputmode="numeric"
    :title="invalid ? `Integer ${min}..${max === Number.MAX_SAFE_INTEGER ? '' : max}` : undefined"
    :data-testid="testid"
    @change="commit"
    @keydown.enter="commit"
  />
</template>
