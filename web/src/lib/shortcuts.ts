import { computed, onBeforeUnmount, onMounted, type ComputedRef } from 'vue'
import { act } from '@/stores/log'
import { useDialogStore } from '@/stores/dialog'
import { useSettingsStore } from '@/stores/settings'
import { useStatusStore } from '@/stores/status'
import { useUiStore, type Panel } from '@/stores/ui'
import { useUnitsStore } from '@/stores/units'
import { loadJSON, save } from '@/lib/storage'

export type GlobalActionId =
  | 'blast'
  | 'autoCorrupt'
  | 'protection'
  | 'protectionBack'
  | 'protectionLast'
  | 'protectionNow'
  | 'launch'
  | 'loadRom'
  | 'pause'
  | 'reset'

/** Actions that live in one tab; the tab's component registers them while mounted. */
export type TabActionId = 'corrupt' | 'freeze' | 'refresh'

export type ActionId = GlobalActionId | TabActionId

export interface Shortcut {
  key: string
  action: ActionId
  label: string
  /** Fires only while this tab is active. */
  tab?: Panel
}

export const TAB_LABELS: Record<Panel, string> = {
  harvester: 'Harvester',
  editor: 'Blast Editor',
  memory: 'Memory',
  settings: 'Settings',
}

/** Keyboard shortcuts: plain single keys, no modifiers. */
export const SHORTCUTS: readonly Shortcut[] = [
  { key: 'm', action: 'blast', label: 'Manual Blast' },
  { key: 'a', action: 'autoCorrupt', label: 'Toggle Auto-Corrupt' },
  { key: 'p', action: 'protection', label: 'Toggle Game Protection' },
  { key: 'b', action: 'protectionBack', label: 'Game Protection: Back' },
  { key: 'l', action: 'protectionLast', label: 'Game Protection: Last' },
  { key: 'n', action: 'protectionNow', label: 'Game Protection: Now' },
  { key: 'c', action: 'corrupt', label: 'Corrupt (Blast Tools main button)', tab: 'harvester' },
  { key: 'f', action: 'freeze', label: 'Freeze / unfreeze at the hex cursor', tab: 'memory' },
  { key: 'r', action: 'refresh', label: 'Refresh the hex view and bitmaps', tab: 'memory' },
]

/** The shortcut key bound to an action, if any. */
export function shortcutKey(id: ActionId): string | undefined {
  return SHORTCUTS.find((s) => s.action === id)?.key
}

/** Appends the shortcut hint to a tooltip: "Take a backup now (n)". */
export function withKeyHint(id: ActionId, title: string): string {
  const k = shortcutKey(id)
  return k ? `${title} (${k})` : title
}

export interface GlobalAction {
  /** Why the action is unavailable, or '' when it can run. */
  disabled: ComputedRef<string>
  run: () => Promise<unknown>
}

/** Saved Connect popover fields; the quick actions share the ROM path. */
export const CONNECT_KEY = 'rtcvish.connect'
export const CONNECT_DEFAULTS = { address: '127.0.0.1:42069', rom: '' }

/** The first bundled or development emulator whose executable exists. */
function firstEmulator() {
  return useStatusStore().emulators.find((e) => e.present)
}

const NO_EMULATOR = 'No bundled or development emulator found'

/**
 * Loads a ROM picked from the top bar: into the connected emulator, or by
 * launching the first available emulator with it.
 */
export async function loadOrLaunch(path: string): Promise<unknown> {
  const st = useStatusStore()
  save(CONNECT_KEY, { ...loadJSON(CONNECT_KEY, CONNECT_DEFAULTS), rom: path })
  if (st.connected) return act(() => st.loadRom(path), `loaded ${path}`)
  const e = firstEmulator()
  if (!e) return act(() => Promise.reject(new Error(NO_EMULATOR)))
  return act(() => st.launch(e.name, path), `launched ${e.name}`)
}

/** The top bar's global actions, shared by its buttons and the shortcuts. */
export function useGlobalActions(): Record<GlobalActionId, GlobalAction> {
  const st = useStatusStore()
  const settings = useSettingsStore()
  const units = useUnitsStore()
  const ui = useUiStore()

  const noSettings = computed(() => (settings.settings ? '' : 'Settings not loaded'))
  const needBackup = computed(
    () => st.needRom || (st.status?.protectionBackups ? '' : 'No backups yet'),
  )
  const needRom = computed(() => st.needRom)
  const launchReason = computed(() => {
    if (st.connected) return 'Already connected'
    return firstEmulator() ? '' : NO_EMULATOR
  })
  const loadRomReason = computed(() => {
    if (st.connected) return st.needEmu
    return firstEmulator() ? '' : `Emulator not connected and ${NO_EMULATOR.toLowerCase()}`
  })

  return {
    blast: { disabled: needRom, run: () => act(() => units.blast()) },
    autoCorrupt: {
      disabled: noSettings,
      run: () => act(() => settings.patch({ autoCorrupt: !settings.settings?.autoCorrupt })),
    },
    protection: {
      disabled: noSettings,
      run: () =>
        act(() =>
          settings.patch({
            gameProtection: { enabled: !settings.settings?.gameProtection.enabled },
          }),
        ),
    },
    protectionBack: {
      disabled: needBackup,
      run: () => act(() => st.protectionBack(), 'game protection: back'),
    },
    protectionLast: {
      disabled: needBackup,
      run: () => act(() => st.protectionLast(), 'game protection: loaded last backup'),
    },
    protectionNow: {
      disabled: needRom,
      run: () => act(() => st.protectionNow(), 'game protection: backup taken'),
    },
    launch: {
      disabled: launchReason,
      run: async () => {
        const e = firstEmulator()
        if (e) await act(() => st.launch(e.name), `launched ${e.name}`)
      },
    },
    loadRom: {
      disabled: loadRomReason,
      run: async () => {
        ui.romPicker = true
      },
    },
    pause: {
      disabled: needRom,
      run: () =>
        st.game?.state === 'paused'
          ? act(() => st.control('resume'), 'resumed')
          : act(() => st.control('pause'), 'paused'),
    },
    reset: { disabled: needRom, run: () => act(() => st.control('reset'), 'reset') },
  }
}

const tabActions = new Map<TabActionId, GlobalAction>()

/**
 * Registers a tab-scoped action for the calling component's lifetime. The
 * shortcut fires only while the shortcut's tab is active and this is mounted.
 */
export function useTabAction(id: TabActionId, action: GlobalAction) {
  onMounted(() => tabActions.set(id, action))
  onBeforeUnmount(() => {
    if (tabActions.get(id) === action) tabActions.delete(id)
  })
}

function isTyping(el: Element | null): boolean {
  if (!(el instanceof HTMLElement)) return false
  if (el.isContentEditable) return true
  return !!el.closest('input, textarea, select, [contenteditable]:not([contenteditable="false"])')
}

/** Registers the shortcuts on window for the component's lifetime. */
export function useShortcuts() {
  const actions = useGlobalActions()
  const dlg = useDialogStore()
  const ui = useUiStore()

  function onKey(e: KeyboardEvent) {
    if (e.defaultPrevented || e.repeat) return
    if (e.ctrlKey || e.metaKey || e.altKey || e.shiftKey) return
    const s = SHORTCUTS.find((x) => x.key === e.key && (!x.tab || x.tab === ui.panel))
    if (!s) return
    if (isTyping(e.target as Element | null) || isTyping(document.activeElement)) return
    if (dlg.prompt || dlg.menu || document.querySelector('[role="dialog"]')) return
    const a = s.tab ? tabActions.get(s.action as TabActionId) : actions[s.action as GlobalActionId]
    if (!a || a.disabled.value) return
    e.preventDefault()
    void a.run()
  }

  onMounted(() => window.addEventListener('keydown', onKey))
  onBeforeUnmount(() => window.removeEventListener('keydown', onKey))
}
