<script setup lang="ts">
import { computed, ref } from 'vue'
import BoxPanel from '@/components/ui/BoxPanel.vue'
import { act } from '@/stores/log'
import { SLOT_COUNT, useSavestatesStore } from '@/stores/savestates'
import { useStatusStore } from '@/stores/status'
import { useUiStore } from '@/stores/ui'
import type { SavestateSlot } from '@/api/types'

const PER_PAGE = 10
const store = useSavestatesStore()
const st = useStatusStore()
const ui = useUiStore()

const page = ref(0)
const pages = SLOT_COUNT / PER_PAGE
const mode = ref<'save' | 'load'>('load')
const visible = computed(() =>
  store.slots.slice(page.value * PER_PAGE, (page.value + 1) * PER_PAGE),
)

async function click(s: SavestateSlot) {
  ui.selectedSlot = s.slot
  if (mode.value === 'save') {
    await act(() => store.save(s.slot), `saved slot ${s.slot}`)
  } else if (s.key) {
    await act(() => store.load(s.slot), `loaded slot ${s.slot}`)
  }
}

function reason(s: SavestateSlot) {
  if (mode.value === 'save') return st.needRom
  return s.key ? st.needRom : ''
}

function label(s: SavestateSlot, e: Event) {
  const v = (e.target as HTMLInputElement).value
  if (v !== s.label) void act(() => store.setLabel(s.slot, v))
}
</script>

<template>
  <BoxPanel title="Savestates">
    <template #actions>
      <button
        class="btn px-1.5 py-0 normal-case"
        :class="mode === 'save' ? 'btn-accent' : 'btn-on'"
        :data-testid="`savestate-mode`"
        :title="mode === 'save' ? 'Clicking a slot saves into it' : 'Clicking a slot loads it'"
        @click="mode = mode === 'save' ? 'load' : 'save'"
      >
        {{ mode === 'save' ? 'SAVE' : 'LOAD' }}
      </button>
    </template>
    <ul class="flex flex-col gap-0.5" data-testid="savestate-list">
      <li
        v-for="s in visible"
        :key="s.slot"
        class="flex items-center gap-1"
        :data-testid="`slot-${s.slot}`"
        :data-filled="s.key ? 'true' : 'false'"
      >
        <button
          class="btn w-9 font-mono"
          :class="{
            'btn-accent': ui.selectedSlot === s.slot,
            'btn-on': !!s.key && ui.selectedSlot !== s.slot,
          }"
          :disabled="!!reason(s)"
          :title="reason(s) || (s.key ? `${s.game?.title ?? ''} ${s.key}` : 'empty')"
          :data-testid="`slot-${s.slot}-button`"
          @click="click(s)"
        >
          {{ s.slot }}
        </button>
        <input
          class="input flex-1"
          :value="s.label"
          :placeholder="s.key ? (s.game?.title ?? 'state') : ''"
          :data-testid="`slot-${s.slot}-label`"
          @change="label(s, $event)"
          @keydown.enter="($event.target as HTMLInputElement).blur()"
        />
        <button
          class="btn px-1.5"
          :disabled="!s.key && !s.label"
          title="Delete entry"
          :data-testid="`slot-${s.slot}-delete`"
          @click="act(() => store.remove(s.slot))"
        >
          ×
        </button>
      </li>
    </ul>
    <div class="flex items-center justify-between">
      <button class="btn" :disabled="page === 0" data-testid="slot-page-prev" @click="page--">
        ◀
      </button>
      <span class="font-mono text-dim" data-testid="slot-page">{{ page + 1 }} / {{ pages }}</span>
      <button
        class="btn"
        :disabled="page === pages - 1"
        data-testid="slot-page-next"
        @click="page++"
      >
        ▶
      </button>
    </div>
  </BoxPanel>
</template>
