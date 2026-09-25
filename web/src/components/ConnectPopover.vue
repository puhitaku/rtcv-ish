<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { act } from '@/stores/log'
import { useStatusStore } from '@/stores/status'
import { loadJSON, save } from '@/lib/storage'

const emit = defineEmits<{ close: [] }>()
const st = useStatusStore()

const KEY = 'rtcvish.connect'
const saved = loadJSON(KEY, { address: '127.0.0.1:42069', rom: '' })
const address = ref(saved.address)
const rom = ref(saved.rom)
const busy = ref(false)

function remember() {
  save(KEY, { address: address.value, rom: rom.value })
}

async function run(fn: () => Promise<unknown>, ok: string) {
  busy.value = true
  remember()
  await act(fn, ok)
  busy.value = false
}

onMounted(() => void act(() => st.fetchEmulators()))
</script>

<template>
  <div
    class="box absolute top-full left-0 z-40 mt-1 flex w-[26rem] flex-col gap-3 p-3 shadow-lg"
    data-testid="connect-popover"
    @keydown.esc="emit('close')"
  >
    <section class="flex flex-col gap-1">
      <div class="lbl">Emulator API address</div>
      <form
        class="flex gap-1"
        @submit.prevent="run(() => st.connect(address), `connected to ${address}`)"
      >
        <input
          v-model="address"
          class="input flex-1 font-mono"
          placeholder="127.0.0.1:42069"
          data-testid="connect-address"
        />
        <button
          class="btn btn-accent"
          type="submit"
          :disabled="busy || st.connected || !address"
          :title="st.connected ? 'Already connected' : ''"
          data-testid="connect-button"
        >
          Connect
        </button>
        <button
          class="btn"
          type="button"
          :disabled="busy || !st.connected"
          data-testid="disconnect-button"
          @click="run(() => st.disconnect(), 'disconnected')"
        >
          Disconnect
        </button>
      </form>
    </section>

    <section class="flex flex-col gap-1">
      <div class="lbl">Bundled emulators</div>
      <div v-if="!st.emulators.length" class="text-dim" data-testid="emulator-list-empty">
        none bundled
      </div>
      <ul class="flex flex-col gap-1" data-testid="emulator-list">
        <li v-for="e in st.emulators" :key="e.name" class="flex items-center gap-2">
          <span class="font-semibold">{{ e.name }}</span>
          <span class="flex-1 truncate font-mono text-dim" :title="e.path">{{ e.path }}</span>
          <button
            class="btn"
            :disabled="busy || st.connected || !e.present"
            :title="!e.present ? 'Executable not found' : st.connected ? 'Already connected' : ''"
            :data-testid="`launch-${e.name}`"
            @click="run(() => st.launch(e.name, rom || undefined), `launched ${e.name}`)"
          >
            Launch
          </button>
        </li>
      </ul>
    </section>

    <section class="flex flex-col gap-1">
      <div class="lbl">ROM path (on the emulator host; also used by Launch)</div>
      <form class="flex gap-1" @submit.prevent="run(() => st.loadRom(rom), `loaded ${rom}`)">
        <input
          v-model="rom"
          class="input flex-1 font-mono"
          placeholder="/path/to/game.nds"
          data-testid="rom-path"
        />
        <button
          class="btn btn-accent"
          type="submit"
          :disabled="busy || !st.connected || !rom"
          :title="st.needEmu"
          data-testid="rom-load"
        >
          Load ROM
        </button>
      </form>
    </section>

    <section class="flex flex-wrap gap-1">
      <button
        v-if="st.game?.state === 'paused'"
        class="btn"
        :disabled="!!st.needRom"
        :title="st.needRom"
        data-testid="emu-resume"
        @click="run(() => st.control('resume'), 'resumed')"
      >
        Resume
      </button>
      <button
        v-else
        class="btn"
        :disabled="!!st.needRom"
        :title="st.needRom"
        data-testid="emu-pause"
        @click="run(() => st.control('pause'), 'paused')"
      >
        Pause
      </button>
      <button
        class="btn"
        :disabled="!!st.needRom"
        :title="st.needRom"
        data-testid="emu-step"
        @click="run(() => st.step(1), 'stepped 1 frame')"
      >
        Step
      </button>
      <button
        class="btn"
        :disabled="!!st.needRom"
        :title="st.needRom"
        data-testid="emu-reset"
        @click="run(() => st.control('reset'), 'reset')"
      >
        Reset
      </button>
      <button
        class="btn"
        :disabled="!!st.needRom"
        :title="st.needRom"
        data-testid="emu-close-rom"
        @click="run(() => st.control('close-rom'), 'ROM closed')"
      >
        Close ROM
      </button>
      <button
        class="btn"
        :disabled="!!st.needEmu"
        :title="st.needEmu"
        data-testid="emu-quit"
        @click="run(() => st.quit(), 'quit sent')"
      >
        Quit emulator
      </button>
    </section>
  </div>
</template>
