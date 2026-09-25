<script setup lang="ts">
import { onBeforeUnmount, onMounted } from 'vue'
import TopBar from './components/TopBar.vue'
import SideBar from './components/SideBar.vue'
import LogStrip from './components/LogStrip.vue'
import ToastList from './components/ui/ToastList.vue'
import PromptDialog from './components/ui/PromptDialog.vue'
import ContextMenu from './components/ui/ContextMenu.vue'
import EnginePanel from './components/panels/EnginePanel.vue'
import HarvesterPanel from './components/panels/HarvesterPanel.vue'
import BlastEditorPanel from './components/panels/BlastEditorPanel.vue'
import MemoryPanel from './components/panels/MemoryPanel.vue'
import SettingsPanel from './components/panels/SettingsPanel.vue'
import { refetchAll, startEvents } from './stores/events'
import { useUiStore } from './stores/ui'

const ui = useUiStore()
let stop: (() => void) | undefined

onMounted(() => {
  void refetchAll()
  stop = startEvents()
})
onBeforeUnmount(() => stop?.())
</script>

<template>
  <div class="flex h-full flex-col">
    <TopBar />
    <div class="flex min-h-0 flex-1">
      <SideBar />
      <main class="min-w-0 flex-1 overflow-auto p-2" data-testid="panel">
        <EnginePanel v-if="ui.panel === 'engine'" />
        <HarvesterPanel v-else-if="ui.panel === 'harvester'" />
        <BlastEditorPanel v-else-if="ui.panel === 'editor'" />
        <MemoryPanel v-else-if="ui.panel === 'memory'" />
        <SettingsPanel v-else-if="ui.panel === 'settings'" />
      </main>
    </div>
    <LogStrip />
    <ToastList />
    <PromptDialog />
    <ContextMenu />
  </div>
</template>
