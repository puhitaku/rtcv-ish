<script setup lang="ts">
import { computed, ref } from 'vue'
import BoxPanel from '@/components/ui/BoxPanel.vue'
import { act } from '@/stores/log'
import { useStatusStore } from '@/stores/status'
import { useUiStore, type GhMode } from '@/stores/ui'
import { useUnitsStore } from '@/stores/units'
import { useHarvester } from './useHarvester'

const ui = useUiStore()
const st = useStatusStore()
const units = useUnitsStore()
const gh = useHarvester()
const menu = ref(false)

const bl = computed(() => st.status?.blastLayer ?? { available: false, on: false })
const modes: { id: GhMode; label: string }[] = [
  { id: 'corrupt', label: 'Corrupt' },
  { id: 'inject', label: 'Inject' },
  { id: 'original', label: 'Original' },
]
</script>

<template>
  <BoxPanel title="Blast Tools">
    <template #actions>
      <div class="relative normal-case">
        <button
          class="btn px-1.5 py-0"
          :aria-expanded="menu"
          data-testid="gh-menu"
          title="Glitch Harvester mode and behaviours"
          @click="menu = !menu"
        >
          ⚙
        </button>
        <div
          v-if="menu"
          class="box absolute top-full right-0 z-30 mt-1 flex w-52 flex-col gap-1 p-2 font-normal tracking-normal text-fg shadow"
          data-testid="gh-menu-popup"
        >
          <div class="lbl">Mode</div>
          <label v-for="m in modes" :key="m.id" class="flex items-center gap-2">
            <input
              v-model="ui.gh.mode"
              type="radio"
              name="gh-mode"
              :value="m.id"
              :data-testid="`gh-mode-${m.id}`"
            />
            {{ m.label }}
          </label>
          <div class="lbl mt-1">Behaviours</div>
          <label class="flex items-center gap-2">
            <input v-model="ui.gh.autoLoadState" type="checkbox" data-testid="gh-auto-load" />
            Auto-load state
          </label>
          <label class="flex items-center gap-2">
            <input v-model="ui.gh.loadOnSelect" type="checkbox" data-testid="gh-load-on-select" />
            Load on select
          </label>
          <label class="flex items-center gap-2">
            <input v-model="ui.gh.stashResults" type="checkbox" data-testid="gh-stash-results" />
            Stash results
          </label>
        </div>
      </div>
    </template>
    <button
      class="btn btn-accent py-3 text-base font-semibold"
      :disabled="!!gh.mainReason.value"
      :title="gh.mainReason.value"
      data-testid="gh-main"
      @click="gh.mainAction()"
    >
      {{ gh.mainLabel.value }}
    </button>
    <div class="grid grid-cols-2 gap-1">
      <button
        class="btn"
        :disabled="!!gh.rerollReason.value"
        :title="gh.rerollReason.value"
        data-testid="gh-reroll"
        @click="gh.rerollSelected()"
      >
        Reroll Selected
      </button>
      <button
        class="btn"
        :class="{ 'btn-on': bl.on }"
        :disabled="!!st.needRom || !bl.available"
        :title="st.needRom || (!bl.available ? 'No layer applied with backup yet' : '')"
        :aria-pressed="bl.on"
        data-testid="gh-blastlayer"
        @click="act(() => units.toggle(!bl.on))"
      >
        BlastLayer: {{ bl.on ? 'ON' : 'OFF' }}
      </button>
    </div>
    <div class="text-[11px] text-dim" data-testid="gh-mode-label">
      mode: {{ ui.gh.mode }}{{ ui.gh.autoLoadState ? ' · auto-load' : ''
      }}{{ ui.gh.loadOnSelect ? ' · load on select' : ''
      }}{{ ui.gh.stashResults ? ' · stash' : '' }}
    </div>
  </BoxPanel>
</template>
