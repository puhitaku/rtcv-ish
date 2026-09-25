<script setup lang="ts">
import NumField from '@/components/ui/NumField.vue'
import ListSelect from '@/components/ui/ListSelect.vue'
import RangeEditor from './RangeEditor.vue'
import StepUnitsControls from './StepUnitsControls.vue'
import { useSettingsStore } from '@/stores/settings'
import { rangesPatch, useSettingsPatch } from './useSettingsPatch'
import { isSignedDecimal } from '@/lib/decimal'
import { ref, watch } from 'vue'

const settings = useSettingsStore()
const patch = useSettingsPatch()

const tilt = ref('0')
watch(
  () => settings.settings?.custom.tilt,
  (t) => (tilt.value = t ?? '0'),
  { immediate: true },
)
function commitTilt() {
  const t = tilt.value.trim()
  if (isSignedDecimal(t)) void patch({ custom: { tilt: BigInt(t).toString() } })
}

function sel(e: Event) {
  return (e.target as HTMLSelectElement).value
}
function chk(e: Event) {
  return (e.target as HTMLInputElement).checked
}
</script>

<template>
  <div v-if="settings.settings" class="flex flex-col gap-2" data-testid="custom-engine-form">
    <fieldset class="flex flex-col gap-1">
      <legend class="lbl">Unit Source</legend>
      <div class="flex gap-3">
        <label v-for="s in ['value', 'store'] as const" :key="s" class="flex items-center gap-1">
          <input
            type="radio"
            name="custom-source"
            :checked="settings.settings.custom.source === s"
            :data-testid="`custom-source-${s}`"
            @change="patch({ custom: { source: s } })"
          />
          {{ s === 'value' ? 'Value' : 'Store' }}
        </label>
      </div>
    </fieldset>

    <fieldset
      v-if="settings.settings.custom.source === 'value'"
      class="flex flex-col gap-1 border-t border-line pt-1"
    >
      <legend class="lbl">Value Settings</legend>
      <label class="flex items-center justify-between gap-2">
        <span>Value Source</span>
        <select
          class="input"
          :value="settings.settings.custom.valueSource"
          data-testid="custom-value-source"
          @change="
            patch({ custom: { valueSource: sel($event) as 'random' | 'range' | 'valueList' } })
          "
        >
          <option value="random">Random</option>
          <option value="range">Range</option>
          <option value="valueList">Value List</option>
        </select>
      </label>
      <RangeEditor
        v-if="settings.settings.custom.valueSource === 'range'"
        :ranges="settings.settings.custom.ranges"
        :precision="settings.settings.precision"
        testid="custom-range"
        @save="patch({ custom: { ranges: rangesPatch(settings.settings.precision, $event) } })"
      />
      <label
        v-if="settings.settings.custom.valueSource === 'valueList'"
        class="flex items-center justify-between gap-2"
      >
        <span>Value List</span>
        <ListSelect
          :model-value="settings.settings.custom.valueList"
          testid="custom-value-list"
          @update:model-value="patch({ custom: { valueList: $event } })"
        />
      </label>
    </fieldset>

    <fieldset v-else class="flex flex-col gap-1 border-t border-line pt-1">
      <legend class="lbl">Store Settings</legend>
      <label class="flex items-center justify-between gap-2">
        <span>Store Source</span>
        <select
          class="input"
          :value="settings.settings.custom.storeAddress"
          data-testid="custom-store-address"
          @change="patch({ custom: { storeAddress: sel($event) as 'same' | 'random' } })"
        >
          <option value="same">Same</option>
          <option value="random">Random</option>
        </select>
      </label>
      <label class="flex items-center justify-between gap-2">
        <span>Start Storing</span>
        <select
          class="input"
          :value="settings.settings.custom.storeTime"
          data-testid="custom-store-time"
          @change="patch({ custom: { storeTime: sel($event) as 'immediate' | 'preexecute' } })"
        >
          <option value="immediate">Immediate</option>
          <option value="preexecute">First Execute</option>
        </select>
      </label>
      <label class="flex items-center justify-between gap-2">
        <span>Store Type</span>
        <select
          class="input"
          :value="settings.settings.custom.storeType"
          data-testid="custom-store-type"
          @change="patch({ custom: { storeType: sel($event) as 'once' | 'continuous' } })"
        >
          <option value="once">Once</option>
          <option value="continuous">Continuous</option>
        </select>
      </label>
    </fieldset>

    <fieldset class="flex flex-col gap-1 border-t border-line pt-1">
      <legend class="lbl">Modifiers</legend>
      <label class="flex items-center justify-between gap-2">
        <span>Tilt</span>
        <input
          v-model="tilt"
          class="input w-32 font-mono"
          :class="{ invalid: !isSignedDecimal(tilt.trim()) }"
          data-testid="custom-tilt"
          @change="commitTilt"
        />
      </label>
    </fieldset>

    <fieldset class="flex flex-col gap-1 border-t border-line pt-1">
      <legend class="lbl">Step Settings</legend>
      <label class="flex items-center justify-between gap-2">
        <span>Delay</span>
        <NumField
          :model-value="settings.settings.custom.delay"
          testid="custom-delay"
          @update:model-value="patch({ custom: { delay: $event } })"
        />
      </label>
      <label class="flex items-center justify-between gap-2">
        <span>Lifetime (0 = ∞)</span>
        <NumField
          :model-value="settings.settings.custom.lifetime"
          testid="custom-lifetime"
          @update:model-value="patch({ custom: { lifetime: $event } })"
        />
      </label>
      <label class="flex items-center gap-2">
        <input
          type="checkbox"
          :checked="settings.settings.custom.loop"
          data-testid="custom-loop"
          @change="patch({ custom: { loop: chk($event) } })"
        />
        Loop generated units
      </label>
      <StepUnitsControls clear-label="Clear all active units" />
    </fieldset>

    <fieldset class="flex flex-col gap-1 border-t border-line pt-1">
      <legend class="lbl">Limiter</legend>
      <label class="flex items-center justify-between gap-2">
        <span>Limiter List</span>
        <ListSelect
          :model-value="settings.settings.custom.limiterList"
          testid="custom-limiter-list"
          @update:model-value="patch({ custom: { limiterList: $event } })"
        />
      </label>
      <label class="flex items-center justify-between gap-2">
        <span>Limiter Time</span>
        <select
          class="input"
          :value="settings.settings.custom.limiterTime"
          data-testid="custom-limiter-time"
          @change="patch({ custom: { limiterTime: sel($event) as 'none' | 'generate' } })"
        >
          <option value="none">None</option>
          <option value="generate">Generate</option>
        </select>
      </label>
      <label class="flex items-center gap-2">
        <input
          type="checkbox"
          :checked="settings.settings.custom.limiterInverted"
          data-testid="custom-limiter-inverted"
          @change="patch({ custom: { limiterInverted: chk($event) } })"
        />
        Inverted
      </label>
    </fieldset>
  </div>
</template>
