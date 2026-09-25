<script setup lang="ts">
import NumField from '@/components/ui/NumField.vue'
import { act } from '@/stores/log'
import { useSettingsStore } from '@/stores/settings'
import { useStatusStore } from '@/stores/status'
import { useUnitsStore } from '@/stores/units'
import { useSettingsPatch } from './useSettingsPatch'

/** Max ∞ Units / Lock units / Clear units, shared by several engines. */
defineProps<{ clearLabel: string; lock?: boolean }>()
const settings = useSettingsStore()
const st = useStatusStore()
const units = useUnitsStore()
const patch = useSettingsPatch()
</script>

<template>
  <div v-if="settings.settings" class="flex flex-col gap-1">
    <label class="flex items-center justify-between gap-2">
      <span class="lbl">Max ∞ Units</span>
      <NumField
        :model-value="settings.settings.maxInfiniteUnits"
        :min="1"
        testid="max-infinite-units"
        @update:model-value="patch({ maxInfiniteUnits: $event })"
      />
    </label>
    <label v-if="lock" class="flex items-center gap-2">
      <input
        type="checkbox"
        :checked="settings.settings.lockUnits"
        data-testid="lock-units"
        @change="patch({ lockUnits: ($event.target as HTMLInputElement).checked })"
      />
      Lock step units
    </label>
    <button
      class="btn"
      :disabled="!!st.needEmu"
      :title="st.needEmu"
      data-testid="clear-units"
      @click="act(() => units.clear(), 'units cleared')"
    >
      {{ clearLabel }}
    </button>
  </div>
</template>
