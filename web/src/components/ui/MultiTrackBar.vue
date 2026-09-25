<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import NumField from './NumField.vue'
import { SLIDER_STEPS, sliderToValue, valueToSlider } from '@/lib/scale'

/** RTCV MultiTrackBar: a non-linear slider plus an uncapped number box. */
const props = withDefaults(
  defineProps<{ label: string; modelValue: number; min?: number; max?: number; testid: string }>(),
  { min: 1, max: 65535 },
)
const emit = defineEmits<{ 'update:modelValue': [v: number] }>()

const dragging = ref<number | null>(null)
const pos = computed(() => dragging.value ?? valueToSlider(props.modelValue, props.min, props.max))
const shown = computed(() =>
  dragging.value == null ? props.modelValue : sliderToValue(dragging.value, props.min, props.max),
)
watch(
  () => props.modelValue,
  () => (dragging.value = null),
)

function onInput(e: Event) {
  dragging.value = Number((e.target as HTMLInputElement).value)
}

function onChange(e: Event) {
  const v = sliderToValue(Number((e.target as HTMLInputElement).value), props.min, props.max)
  if (v === props.modelValue) dragging.value = null
  else emit('update:modelValue', v)
}
</script>

<template>
  <div class="flex flex-col gap-0.5">
    <div class="flex items-center justify-between">
      <span class="lbl">{{ label }}</span>
      <NumField
        :model-value="shown"
        :min="min"
        :testid="`${testid}-number`"
        @update:model-value="emit('update:modelValue', $event)"
      />
    </div>
    <input
      type="range"
      class="w-full accent-[var(--c-accent)]"
      :min="0"
      :max="SLIDER_STEPS"
      :value="pos"
      :aria-label="label"
      :data-testid="`${testid}-slider`"
      @input="onInput"
      @change="onChange"
    />
  </div>
</template>
