<script setup lang="ts">
import NumField from '@/components/ui/NumField.vue'
import ListSelect from '@/components/ui/ListSelect.vue'
import RangeEditor from './RangeEditor.vue'
import StepUnitsControls from './StepUnitsControls.vue'
import CustomEngineForm from './CustomEngineForm.vue'
import type { Settings } from '@/api/types'
import { act } from '@/stores/log'
import { useStatusStore } from '@/stores/status'
import { useUnitsStore } from '@/stores/units'
import { rangesPatch, useSettingsPatch } from './useSettingsPatch'

/** The engine-specific parameter block. */
defineProps<{ s: Settings }>()
const patch = useSettingsPatch()
const st = useStatusStore()
const units = useUnitsStore()

function sel(e: Event) {
  return (e.target as HTMLSelectElement).value
}
function chk(e: Event) {
  return (e.target as HTMLInputElement).checked
}
</script>

<template>
  <div class="flex flex-col gap-2" :data-testid="`engine-params-${s.engine}`">
    <template v-if="s.engine === 'nightmare'">
      <label class="flex items-center justify-between gap-2">
        <span>Blast type</span>
        <select
          class="input"
          :value="s.nightmare.algo"
          data-testid="nightmare-algo"
          @change="patch({ nightmare: { algo: sel($event) as 'random' | 'randomTilt' | 'tilt' } })"
        >
          <option value="random">Random</option>
          <option value="randomTilt">Random Tilt</option>
          <option value="tilt">Tilt</option>
        </select>
      </label>
      <RangeEditor
        :ranges="s.nightmare.ranges"
        :precision="s.precision"
        testid="nightmare-range"
        @save="patch({ nightmare: { ranges: rangesPatch(s.precision, $event) } })"
      />
    </template>

    <template v-else-if="s.engine === 'hellgenie'">
      <RangeEditor
        :ranges="s.hellgenie.ranges"
        :precision="s.precision"
        testid="hellgenie-range"
        @save="patch({ hellgenie: { ranges: rangesPatch(s.precision, $event) } })"
      />
      <StepUnitsControls clear-label="Clear all cheats" />
    </template>

    <template v-else-if="s.engine === 'distortion'">
      <label class="flex items-center justify-between gap-2">
        <span>Distortion delay</span>
        <NumField
          :model-value="s.distortion.delay"
          :min="1"
          testid="distortion-delay"
          @update:model-value="patch({ distortion: { delay: $event } })"
        />
      </label>
      <button
        class="btn"
        :disabled="!!st.needEmu"
        :title="st.needEmu"
        data-testid="distortion-resync"
        @click="act(() => units.clear(), 'distortion engine resynced')"
      >
        Resync Distortion Engine
      </button>
    </template>

    <template v-else-if="s.engine === 'freeze'">
      <StepUnitsControls clear-label="Clear all freezes" />
    </template>

    <template v-else-if="s.engine === 'pipe'">
      <StepUnitsControls clear-label="Clear pipes" lock />
    </template>

    <template v-else-if="s.engine === 'vector'">
      <label class="flex items-center justify-between gap-2">
        <span>Limiter list</span>
        <ListSelect
          :model-value="s.vector.limiterList"
          testid="vector-limiter-list"
          @update:model-value="patch({ vector: { limiterList: $event } })"
        />
      </label>
      <label class="flex items-center justify-between gap-2">
        <span>Value list</span>
        <ListSelect
          :model-value="s.vector.valueList"
          testid="vector-value-list"
          @update:model-value="patch({ vector: { valueList: $event } })"
        />
      </label>
      <label class="flex items-center gap-2">
        <input
          type="checkbox"
          :checked="s.vector.unlockPrecision"
          data-testid="vector-unlock-precision"
          @change="patch({ vector: { unlockPrecision: chk($event) } })"
        />
        Unlock precision
      </label>
    </template>

    <template v-else-if="s.engine === 'cluster'">
      <label class="flex items-center justify-between gap-2">
        <span>Limiter list</span>
        <ListSelect
          :model-value="s.cluster.limiterList"
          testid="cluster-limiter-list"
          @update:model-value="patch({ cluster: { limiterList: $event } })"
        />
      </label>
      <label class="flex items-center justify-between gap-2">
        <span>Chunk size</span>
        <NumField
          :model-value="s.cluster.chunkSize"
          :min="1"
          testid="cluster-chunk-size"
          @update:model-value="patch({ cluster: { chunkSize: $event } })"
        />
      </label>
      <label class="flex items-center justify-between gap-2">
        <span>Method</span>
        <select
          class="input"
          :value="s.cluster.method"
          data-testid="cluster-method"
          @change="
            patch({
              cluster: {
                method: sel($event) as
                  'random' | 'reverse' | 'rotateForwards' | 'rotateBackwards' | 'overwrite',
              },
            })
          "
        >
          <option value="random">Random</option>
          <option value="reverse">Reverse</option>
          <option value="rotateForwards">Rotate Forwards</option>
          <option value="rotateBackwards">Rotate Backwards</option>
          <option value="overwrite">Overwrite</option>
        </select>
      </label>
      <label class="flex items-center justify-between gap-2">
        <span>Rotate amount</span>
        <NumField
          :model-value="s.cluster.modifier"
          testid="cluster-modifier"
          @update:model-value="patch({ cluster: { modifier: $event } })"
        />
      </label>
      <label class="flex items-center justify-between gap-2">
        <span>Direction</span>
        <select
          class="input"
          :value="s.cluster.direction"
          data-testid="cluster-direction"
          @change="patch({ cluster: { direction: sel($event) as 'forwards' | 'backwards' } })"
        >
          <option value="forwards">Forwards</option>
          <option value="backwards">Backwards</option>
        </select>
      </label>
      <label class="flex items-center gap-2">
        <input
          type="checkbox"
          :checked="s.cluster.splitUnits"
          data-testid="cluster-split-units"
          @change="patch({ cluster: { splitUnits: chk($event) } })"
        />
        Split blast units
      </label>
      <label class="flex items-center gap-2">
        <input
          type="checkbox"
          :checked="s.cluster.filterAll"
          data-testid="cluster-filter-all"
          @change="patch({ cluster: { filterAll: chk($event) } })"
        />
        Filter all
      </label>
    </template>

    <CustomEngineForm v-else-if="s.engine === 'custom'" />
  </div>
</template>
