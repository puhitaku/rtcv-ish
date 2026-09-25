<script setup lang="ts">
import BoxPanel from '@/components/ui/BoxPanel.vue'
import MultiTrackBar from '@/components/ui/MultiTrackBar.vue'
import NumField from '@/components/ui/NumField.vue'
import EngineParams from '@/components/engine/EngineParams.vue'
import DomainsBox from '@/components/engine/DomainsBox.vue'
import AddressRangeEditor from '@/components/engine/AddressRangeEditor.vue'
import { useSettingsPatch } from '@/components/engine/useSettingsPatch'
import { useSettingsStore } from '@/stores/settings'
import type { Engine, Precision, Settings } from '@/api/types'

const store = useSettingsStore()
const patch = useSettingsPatch()

const engines: { id: Engine; label: string }[] = [
  { id: 'nightmare', label: 'Nightmare Engine' },
  { id: 'hellgenie', label: 'Hellgenie Engine' },
  { id: 'distortion', label: 'Distortion Engine' },
  { id: 'freeze', label: 'Freeze Engine' },
  { id: 'pipe', label: 'Pipe Engine' },
  { id: 'vector', label: 'Vector Engine' },
  { id: 'cluster', label: 'Cluster Engine' },
  { id: 'custom', label: 'Custom Engine' },
]
const radii: { id: Settings['radius']; label: string }[] = [
  { id: 'spread', label: 'SPREAD' },
  { id: 'chunk', label: 'CHUNK' },
  { id: 'burst', label: 'BURST' },
  { id: 'even', label: 'EVEN' },
  { id: 'proportional', label: 'PROPORTIONAL' },
  { id: 'normalized', label: 'NORMALIZED' },
]
const precisions: Precision[] = [1, 2, 4, 8]

function sel(e: Event) {
  return (e.target as HTMLSelectElement).value
}

function setPrecision(p: Precision) {
  const s = store.settings
  void patch(s && s.alignment >= p ? { precision: p, alignment: 0 } : { precision: p })
}
</script>

<template>
  <div
    v-if="store.settings"
    class="grid grid-cols-1 gap-2 lg:grid-cols-[minmax(14rem,1fr)_minmax(18rem,1.3fr)_minmax(16rem,1fr)]"
    data-testid="engine-panel"
  >
    <BoxPanel title="General Parameters">
      <MultiTrackBar
        label="Intensity"
        :model-value="store.settings.intensity"
        testid="intensity"
        @update:model-value="patch({ intensity: $event })"
      />
      <MultiTrackBar
        label="Error Delay"
        :model-value="store.settings.errorDelay"
        testid="error-delay"
        @update:model-value="patch({ errorDelay: $event })"
      />
      <label class="flex items-center justify-between gap-2">
        <span class="lbl">Blast Radius</span>
        <select
          class="input"
          :value="store.settings.radius"
          data-testid="blast-radius"
          @change="patch({ radius: sel($event) as Settings['radius'] })"
        >
          <option v-for="r in radii" :key="r.id" :value="r.id">{{ r.label }}</option>
        </select>
      </label>
      <div class="border-t border-line pt-2">
        <AddressRangeEditor :range="store.settings.addressRange" />
      </div>
    </BoxPanel>

    <BoxPanel title="Corruption Engine">
      <select
        class="input"
        :value="store.settings.engine"
        data-testid="engine-select"
        @change="patch({ engine: sel($event) as Engine })"
      >
        <option v-for="e in engines" :key="e.id" :value="e.id">{{ e.label }}</option>
      </select>
      <div class="flex flex-wrap items-center gap-3">
        <label class="flex items-center gap-1">
          <span class="lbl">Precision</span>
          <select
            class="input"
            :value="store.settings.precision"
            data-testid="precision-select"
            @change="setPrecision(Number(sel($event)) as Precision)"
          >
            <option v-for="p in precisions" :key="p" :value="p">{{ p * 8 }}-bit</option>
          </select>
        </label>
        <label class="flex items-center gap-1">
          <span class="lbl">Alignment</span>
          <NumField
            :model-value="store.settings.alignment"
            :min="0"
            :max="store.settings.precision - 1"
            width="w-12"
            testid="alignment"
            @update:model-value="patch({ alignment: $event })"
          />
        </label>
      </div>
      <div class="border-t border-line pt-2">
        <EngineParams :s="store.settings" />
      </div>
    </BoxPanel>

    <DomainsBox />
  </div>
  <div v-else class="text-dim" data-testid="engine-panel-loading">Loading settings…</div>
</template>
