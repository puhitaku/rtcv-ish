<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import ConnectPopover from './ConnectPopover.vue'
import { useGlobalActions, withKeyHint as hint } from '@/lib/shortcuts'
import { useSettingsStore } from '@/stores/settings'
import { useStatusStore } from '@/stores/status'
import { useUiStore } from '@/stores/ui'

const st = useStatusStore()
const settings = useSettingsStore()
const ui = useUiStore()
const actions = useGlobalActions()

const open = ref(false)
const root = ref<HTMLElement | null>(null)

function onDown(e: MouseEvent) {
  if (open.value && root.value && !root.value.contains(e.target as Node)) open.value = false
}
onMounted(() => window.addEventListener('mousedown', onDown))
onBeforeUnmount(() => window.removeEventListener('mousedown', onDown))

const emuLabel = computed(() => {
  if (!st.connected) return 'disconnected'
  if (st.unresponsive) return 'unresponsive'
  const e = st.status?.emulator
  return e ? `${e.name} ${e.version}` : 'connected'
})

const dotClass = computed(() => {
  if (st.unresponsive) return 'bg-warn'
  if (st.connected) return 'bg-ok'
  return st.live ? 'bg-warn' : 'bg-err'
})

const connTitle = computed(() =>
  st.unresponsive
    ? 'The emulator stopped answering API calls. Operations fail until it responds again; ' +
      'open this menu to disconnect or quit it.'
    : 'Connect, launch or control the emulator',
)

/** Only operations running for at least this long show the busy indicator. */
const BUSY_SHOW_MS = 1000

const showBusy = computed(() => !!st.busy && st.busyMs >= BUSY_SHOW_MS)

const busyLabel = computed(() =>
  st.busy ? `${st.busy.operation} ${Math.floor(st.busyMs / 1000)}s` : '',
)

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
        :class="{ 'border-warn text-warn': st.unresponsive }"
        :aria-expanded="open"
        :title="connTitle"
        data-testid="connection-status"
        @click="open = !open"
      >
        <span class="inline-block h-2 w-2 rounded-full" :class="dotClass" />
        <span class="inline-block min-w-[14ch] text-left" data-testid="emulator-label">
          {{ emuLabel }}
        </span>
      </button>
      <ConnectPopover v-if="open" @close="open = false" />
    </div>

    <!-- Fixed-width slot so the indicator never shifts the title or frame counter. -->
    <span class="w-[18ch] shrink-0 truncate font-mono text-xs text-dim" data-testid="busy-slot">
      <span
        v-if="showBusy && st.busy"
        :title="`The core is running ${st.busy.operation}; new operations wait or fail with BUSY`"
        data-testid="busy-indicator"
      >
        {{ busyLabel }}
      </span>
    </span>

    <span class="flex items-center gap-1" :title="st.game?.romPath">
      <span class="lbl">game</span>
      <span data-testid="game-title">{{ gameLabel }}</span>
      <span v-if="st.game?.code" class="font-mono text-dim" data-testid="game-code">
        [{{ st.game.code }}]
      </span>
      <span
        class="text-warn"
        :class="{ invisible: st.game?.state !== 'paused' }"
        :aria-hidden="st.game?.state !== 'paused'"
      >
        paused
      </span>
    </span>

    <span class="flex items-center gap-1">
      <span class="lbl">frame</span>
      <span class="inline-block min-w-[9ch] font-mono tabular-nums" data-testid="frame-counter">{{
        st.frame
      }}</span>
    </span>

    <span class="flex-1" />

    <button
      class="btn btn-accent"
      :disabled="!!actions.blast.disabled.value"
      :title="
        actions.blast.disabled.value ||
        hint('blast', 'Generate with the current settings and apply')
      "
      data-testid="manual-blast"
      @click="actions.blast.run()"
    >
      Manual Blast
    </button>
    <button
      class="btn"
      :class="{ 'btn-on': autoCorrupt }"
      :disabled="!!actions.autoCorrupt.disabled.value"
      :title="actions.autoCorrupt.disabled.value || hint('autoCorrupt', 'Toggle Auto-Corrupt')"
      data-testid="auto-corrupt"
      :aria-pressed="autoCorrupt"
      @click="actions.autoCorrupt.run()"
    >
      Auto-Corrupt: {{ autoCorrupt ? 'ON' : 'OFF' }}
    </button>

    <span class="flex items-center gap-1 border-l border-line pl-3">
      <button
        class="btn"
        :class="{ 'btn-on': protection }"
        :disabled="!!actions.protection.disabled.value"
        :title="actions.protection.disabled.value || hint('protection', 'Toggle Game Protection')"
        :aria-pressed="protection"
        data-testid="protection-toggle"
        @click="actions.protection.run()"
      >
        Game Protection: {{ protection ? 'ON' : 'OFF' }}
      </button>
      <button
        class="btn"
        :disabled="!!actions.protectionBack.disabled.value"
        :title="
          actions.protectionBack.disabled.value ||
          hint('protectionBack', 'Load the previous backup')
        "
        data-testid="protection-back"
        @click="actions.protectionBack.run()"
      >
        Back
        <span class="font-mono text-dim">{{ st.status?.protectionBackups ?? 0 }}</span>
      </button>
      <button
        class="btn"
        :disabled="!!actions.protectionLast.disabled.value"
        :title="
          actions.protectionLast.disabled.value ||
          hint('protectionLast', 'Load the most recent backup and keep it')
        "
        data-testid="protection-last"
        @click="actions.protectionLast.run()"
      >
        Last
      </button>
      <button
        class="btn"
        :disabled="!!actions.protectionNow.disabled.value"
        :title="actions.protectionNow.disabled.value || hint('protectionNow', 'Take a backup now')"
        data-testid="protection-now"
        @click="actions.protectionNow.run()"
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
