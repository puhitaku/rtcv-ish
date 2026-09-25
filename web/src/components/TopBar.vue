<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import ConnectPopover from './ConnectPopover.vue'
import { act } from '@/stores/log'
import { useSettingsStore } from '@/stores/settings'
import { useStatusStore } from '@/stores/status'
import { useUiStore } from '@/stores/ui'
import { useUnitsStore } from '@/stores/units'

const st = useStatusStore()
const settings = useSettingsStore()
const ui = useUiStore()
const units = useUnitsStore()

const open = ref(false)
const root = ref<HTMLElement | null>(null)

function onDown(e: MouseEvent) {
  if (open.value && root.value && !root.value.contains(e.target as Node)) open.value = false
}
onMounted(() => window.addEventListener('mousedown', onDown))
onBeforeUnmount(() => window.removeEventListener('mousedown', onDown))

const emuLabel = computed(() => {
  if (!st.connected) return 'disconnected'
  const e = st.status?.emulator
  return e ? `${e.name} ${e.version}` : 'connected'
})

const gameLabel = computed(() => {
  const g = st.game
  if (!st.connected || !g || g.state === 'noRom') return 'no ROM'
  return g.title || g.romPath.split(/[\\/]/).pop() || '(untitled)'
})

const autoCorrupt = computed(() => settings.settings?.autoCorrupt ?? false)
const protection = computed(() => settings.settings?.gameProtection.enabled ?? false)

const themeLabel = computed(() => ({ system: 'Auto', light: 'Light', dark: 'Dark' })[ui.theme])
</script>

<template>
  <header
    class="flex shrink-0 flex-wrap items-center gap-x-3 gap-y-1 border-b border-line bg-panel px-2 py-1"
  >
    <span class="font-mono font-bold text-accent">rtcv-ish</span>

    <div ref="root" class="relative">
      <button
        class="btn"
        :aria-expanded="open"
        data-testid="connection-status"
        @click="open = !open"
      >
        <span
          class="inline-block h-2 w-2 rounded-full"
          :class="st.connected ? 'bg-ok' : st.live ? 'bg-warn' : 'bg-err'"
        />
        <span data-testid="emulator-label">{{ emuLabel }}</span>
      </button>
      <ConnectPopover v-if="open" @close="open = false" />
    </div>

    <span class="flex items-center gap-1" :title="st.game?.romPath">
      <span class="lbl">game</span>
      <span data-testid="game-title">{{ gameLabel }}</span>
      <span v-if="st.game?.code" class="font-mono text-dim" data-testid="game-code">
        [{{ st.game.code }}]
      </span>
      <span v-if="st.game?.state === 'paused'" class="text-warn">paused</span>
    </span>

    <span class="flex items-center gap-1">
      <span class="lbl">frame</span>
      <span class="min-w-16 font-mono" data-testid="frame-counter">{{ st.frame }}</span>
    </span>

    <span class="flex-1" />

    <button
      class="btn btn-accent"
      :disabled="!!st.needRom"
      :title="st.needRom || 'Generate with the current settings and apply'"
      data-testid="manual-blast"
      @click="act(() => units.blast())"
    >
      Manual Blast
    </button>
    <button
      class="btn"
      :class="{ 'btn-on': autoCorrupt }"
      :disabled="!settings.settings"
      data-testid="auto-corrupt"
      :aria-pressed="autoCorrupt"
      @click="act(() => settings.patch({ autoCorrupt: !autoCorrupt }))"
    >
      Auto-Corrupt: {{ autoCorrupt ? 'ON' : 'OFF' }}
    </button>

    <span class="flex items-center gap-1 border-l border-line pl-3">
      <button
        class="btn"
        :class="{ 'btn-on': protection }"
        :disabled="!settings.settings"
        :aria-pressed="protection"
        data-testid="protection-toggle"
        @click="act(() => settings.patch({ gameProtection: { enabled: !protection } }))"
      >
        Game Protection: {{ protection ? 'ON' : 'OFF' }}
      </button>
      <button
        class="btn"
        :disabled="!!st.needRom || !st.status?.protectionBackups"
        :title="
          st.needRom ||
          (!st.status?.protectionBackups ? 'No backups yet' : 'Load the previous backup')
        "
        data-testid="protection-back"
        @click="act(() => st.protectionBack(), 'game protection: back')"
      >
        Back
        <span class="font-mono text-dim">{{ st.status?.protectionBackups ?? 0 }}</span>
      </button>
      <button
        class="btn"
        :disabled="!!st.needRom"
        :title="st.needRom || 'Take a backup now'"
        data-testid="protection-now"
        @click="act(() => st.protectionNow(), 'game protection: backup taken')"
      >
        Now
      </button>
    </span>

    <button
      class="btn"
      :title="`Theme: ${themeLabel} (click to change)`"
      data-testid="theme-toggle"
      @click="ui.cycleTheme()"
    >
      Theme: {{ themeLabel }}
    </button>
  </header>
</template>
