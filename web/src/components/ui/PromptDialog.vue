<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import { useDialogStore } from '@/stores/dialog'

const dlg = useDialogStore()
const input = ref<HTMLInputElement | null>(null)
const okBtn = ref<HTMLButtonElement | null>(null)

watch(
  () => dlg.prompt,
  async (p) => {
    if (!p) return
    await nextTick()
    if (p.confirm) okBtn.value?.focus()
    else input.value?.select()
  },
)

function ok() {
  if (!dlg.prompt) return
  dlg.close(dlg.prompt.value)
}
</script>

<template>
  <div
    v-if="dlg.prompt"
    class="fixed inset-0 z-50 flex items-start justify-center bg-black/30 pt-24"
    @keydown.esc="dlg.close(null)"
    @mousedown.self="dlg.close(null)"
  >
    <form class="box w-96 p-3" role="dialog" data-testid="prompt" @submit.prevent="ok">
      <div class="mb-2" data-testid="prompt-title">{{ dlg.prompt.title }}</div>
      <input
        v-if="!dlg.prompt.confirm"
        ref="input"
        v-model="dlg.prompt.value"
        class="input mb-2 w-full font-mono"
        data-testid="prompt-input"
      />
      <div class="flex justify-end gap-2">
        <button type="button" class="btn" data-testid="prompt-cancel" @click="dlg.close(null)">
          Cancel
        </button>
        <button ref="okBtn" type="submit" class="btn btn-accent" data-testid="prompt-ok">OK</button>
      </div>
    </form>
  </div>
</template>
