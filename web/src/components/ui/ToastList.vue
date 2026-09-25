<script setup lang="ts">
import { useLogStore } from '@/stores/log'

const log = useLogStore()
</script>

<template>
  <div class="fixed right-3 bottom-10 z-50 flex w-80 flex-col gap-1" aria-live="polite">
    <button
      v-for="t in log.toasts"
      :key="t.id"
      class="box border-l-4 px-2 py-1.5 text-left shadow"
      :class="t.level === 'error' ? 'border-l-err' : 'border-l-accent'"
      data-testid="toast"
      @click="log.dismiss(t.id)"
    >
      {{ t.msg }}
    </button>
  </div>
</template>
