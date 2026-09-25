<script setup lang="ts">
import { computed } from 'vue'
import type { FreezeMode } from '@/api/types'
import { effectiveFreezeMode, FREEZE_MODES } from '@/lib/freeze'
import { useSettingsStore } from '@/stores/settings'
import { useStatusStore } from '@/stores/status'
import { useSettingsPatch } from './useSettingsPatch'

/** The `freezeMode` setting: how infinite units are enforced. */
defineProps<{ testid: string }>()
const settings = useSettingsStore()
const st = useStatusStore()
const patch = useSettingsPatch()

const caps = computed(() => st.status?.emulator?.capabilities)
const mode = computed(() => settings.settings?.freezeMode ?? 'hard')
const effective = computed(() => effectiveFreezeMode(mode.value, caps.value))
const label = (m: FreezeMode) => FREEZE_MODES.find((x) => x.value === m)?.label ?? m

function supported(m: FreezeMode) {
  return effectiveFreezeMode(m, caps.value) === m
}

const TITLE =
  'How units with infinite lifetime hold their value: written once per frame (RTCV), ' +
  'also at every scanline, or hard (game writes to the bytes are blocked). ' +
  'Hard applies to value units; store units use per scanline at most.'
</script>

<template>
  <div v-if="settings.settings" class="flex flex-col gap-0.5">
    <label class="flex items-center justify-between gap-2" :title="TITLE">
      <span>Infinite units</span>
      <select
        class="input"
        :value="mode"
        :data-testid="testid"
        @change="patch({ freezeMode: ($event.target as HTMLSelectElement).value as FreezeMode })"
      >
        <option v-for="m in FREEZE_MODES" :key="m.value" :value="m.value">
          {{ m.label }}{{ supported(m.value) ? '' : ' (unsupported)' }}
        </option>
      </select>
    </label>
    <div v-if="effective !== mode" class="text-[11px] text-dim" :data-testid="`${testid}-fallback`">
      The emulator lacks {{ label(mode) }}; using {{ label(effective) }}.
    </div>
  </div>
</template>
