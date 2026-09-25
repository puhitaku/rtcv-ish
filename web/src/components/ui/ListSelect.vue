<script setup lang="ts">
import { useListsStore } from '@/stores/lists'

defineProps<{ modelValue: string; testid: string }>()
const emit = defineEmits<{ 'update:modelValue': [v: string] }>()
const lists = useListsStore()
</script>

<template>
  <select
    class="input"
    :value="modelValue"
    :data-testid="testid"
    @change="emit('update:modelValue', ($event.target as HTMLSelectElement).value)"
  >
    <option value="">(none)</option>
    <option v-for="l in lists.lists" :key="l.name" :value="l.name">
      {{ l.name }} ({{ l.precision * 8 }}-bit, {{ l.entries }})
    </option>
    <option
      v-if="modelValue && !lists.lists.some((l) => l.name === modelValue)"
      :value="modelValue"
    >
      {{ modelValue }} (missing)
    </option>
  </select>
</template>
