<script setup lang="ts">
defineProps<{ title: string }>()
</script>

<template>
  <section class="box flex min-w-0 flex-col">
    <header class="box-title flex items-center justify-between gap-2">
      <span>{{ title }}</span>
      <slot name="actions" />
    </header>
    <div class="flex min-h-0 flex-1 flex-col gap-2 p-2">
      <slot />
    </div>
  </section>
</template>
