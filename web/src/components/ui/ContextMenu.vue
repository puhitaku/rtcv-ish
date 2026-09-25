<script setup lang="ts">
import { onBeforeUnmount, onMounted } from 'vue'
import { useDialogStore } from '@/stores/dialog'

const dlg = useDialogStore()

function onDown(e: MouseEvent) {
  if (!(e.target as HTMLElement | null)?.closest('[data-context-menu]')) dlg.closeMenu()
}
function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') dlg.closeMenu()
}
onMounted(() => {
  window.addEventListener('mousedown', onDown)
  window.addEventListener('keydown', onKey)
})
onBeforeUnmount(() => {
  window.removeEventListener('mousedown', onDown)
  window.removeEventListener('keydown', onKey)
})
</script>

<template>
  <ul
    v-if="dlg.menu"
    data-context-menu
    class="box fixed z-50 min-w-44 py-0.5 shadow"
    :style="{ left: `${dlg.menu.x}px`, top: `${dlg.menu.y}px` }"
    role="menu"
    data-testid="context-menu"
  >
    <li v-for="it in dlg.menu.items" :key="it.testid">
      <button
        class="w-full px-3 py-1 text-left hover:bg-inset disabled:opacity-40"
        role="menuitem"
        :disabled="it.disabled"
        :data-testid="it.testid"
        @click="
          () => {
            dlg.closeMenu()
            it.action()
          }
        "
      >
        {{ it.label }}
      </button>
    </li>
  </ul>
</template>
