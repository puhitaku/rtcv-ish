import { defineStore } from 'pinia'
import { ref, watch } from 'vue'
import { loadJSON, loadString, save } from '@/lib/storage'

export type Panel = 'engine' | 'harvester' | 'editor' | 'memory' | 'settings'
export type Theme = 'system' | 'light' | 'dark'
export type GhMode = 'corrupt' | 'inject' | 'original'

export interface Behaviours {
  autoLoadState: boolean
  loadOnSelect: boolean
  stashResults: boolean
}

export type EditorTarget =
  { kind: 'stash'; key: string } | { kind: 'stockpile'; key: string } | { kind: 'new' }

const THEME_KEY = 'rtcvish.theme'
const GH_KEY = 'rtcvish.gh'

export function applyTheme(t: Theme) {
  if (typeof document === 'undefined') return
  const root = document.documentElement
  if (t === 'system') root.removeAttribute('data-theme')
  else root.setAttribute('data-theme', t)
}

export const useUiStore = defineStore('ui', () => {
  const panel = ref<Panel>('engine')

  const theme = ref<Theme>(loadString(THEME_KEY, 'system') as Theme)
  watch(
    theme,
    (t) => {
      save(THEME_KEY, t)
      applyTheme(t)
    },
    { immediate: true },
  )

  function cycleTheme() {
    const order: Theme[] = ['system', 'light', 'dark']
    theme.value = order[(order.indexOf(theme.value) + 1) % order.length]!
  }

  const gh = ref(
    loadJSON<{ mode: GhMode } & Behaviours>(GH_KEY, {
      mode: 'corrupt',
      autoLoadState: true,
      loadOnSelect: true,
      stashResults: true,
    }),
  )
  watch(gh, (v) => save(GH_KEY, v), { deep: true })

  /** Glitch Harvester selection. */
  const selectedSlot = ref<number | null>(null)
  const stashSelection = ref<string[]>([])
  const stockpileSelection = ref<string[]>([])
  /** The list the user selected from last; Inject/Original/Reroll use its selection. */
  const ghSource = ref<'stash' | 'stockpile'>('stash')

  const editorTarget = ref<EditorTarget | null>(null)

  function openEditor(t: EditorTarget) {
    editorTarget.value = t
    panel.value = 'editor'
  }

  /** Memory panel jump target (from the Blast Editor). */
  const memoryTarget = ref<{ domain: string; address: number } | null>(null)

  function openMemory(domain: string, address: number) {
    memoryTarget.value = { domain, address }
    panel.value = 'memory'
  }

  return {
    panel,
    theme,
    cycleTheme,
    gh,
    selectedSlot,
    stashSelection,
    stockpileSelection,
    ghSource,
    editorTarget,
    openEditor,
    memoryTarget,
    openMemory,
  }
})
