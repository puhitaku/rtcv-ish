<script setup lang="ts">
import { fieldBases, toggleBase, type NumField } from '@/lib/numBase'

/** Switches a numeric field between hex and decimal display. */
const props = defineProps<{ field: NumField }>()
</script>

<template>
  <button
    type="button"
    class="base-toggle font-mono"
    :title="`Shown in ${fieldBases[props.field] === 'hex' ? 'hexadecimal' : 'decimal'}; click to switch`"
    :aria-label="`${props.field}: ${fieldBases[props.field]}, switch base`"
    :data-testid="`be-base-${props.field}`"
    @click="toggleBase(props.field)"
  >
    {{ fieldBases[props.field] }}
  </button>
</template>

<style scoped>
.base-toggle {
  width: 2.4rem;
  flex: none;
  padding: 0 0.2rem;
  border: 1px solid var(--c-line);
  background: var(--c-panel);
  color: var(--c-dim);
  border-radius: 2px;
  font-size: 10px;
  cursor: pointer;
}
.base-toggle:hover {
  border-color: var(--c-accent);
  color: var(--c-fg);
}
.base-toggle:focus-visible {
  outline: 1px solid var(--c-accent);
}
</style>
