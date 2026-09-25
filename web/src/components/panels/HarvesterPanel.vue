<script setup lang="ts">
import BlastTools from '@/components/harvester/BlastTools.vue'
import SavestateManager from '@/components/harvester/SavestateManager.vue'
import StashHistory from '@/components/harvester/StashHistory.vue'
import StockpileManager from '@/components/harvester/StockpileManager.vue'
</script>

<template>
  <div
    class="grid grid-cols-1 gap-2 md:grid-cols-2 xl:grid-cols-[14rem_16rem_minmax(14rem,1fr)_minmax(22rem,1.6fr)]"
    data-testid="harvester-panel"
  >
    <BlastTools />
    <SavestateManager />
    <StashHistory />
    <StockpileManager />
  </div>
</template>
