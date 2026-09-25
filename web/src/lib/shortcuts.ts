import { computed, onBeforeUnmount, onMounted, type ComputedRef } from 'vue'
import { act } from '@/stores/log'
import { useDialogStore } from '@/stores/dialog'
import { useSettingsStore } from '@/stores/settings'
import { useStatusStore } from '@/stores/status'
import { useUnitsStore } from '@/stores/units'

export type GlobalActionId =
  'blast' | 'autoCorrupt' | 'protection' | 'protectionBack' | 'protectionLast' | 'protectionNow'

/** Global keyboard shortcuts: plain single keys, no modifiers. */
export const SHORTCUTS: readonly { key: string; action: GlobalActionId; label: string }[] = [
  { key: 'm', action: 'blast', label: 'Manual Blast' },
  { key: 'a', action: 'autoCorrupt', label: 'Toggle Auto-Corrupt' },
  { key: 'p', action: 'protection', label: 'Toggle Game Protection' },
  { key: 'b', action: 'protectionBack', label: 'Game Protection: Back' },
  { key: 'l', action: 'protectionLast', label: 'Game Protection: Last' },
  { key: 'n', action: 'protectionNow', label: 'Game Protection: Now' },
]

/** The shortcut key bound to an action, if any. */
export function shortcutKey(id: GlobalActionId): string | undefined {
  return SHORTCUTS.find((s) => s.action === id)?.key
}

/** Appends the shortcut hint to a tooltip: "Take a backup now (n)". */
export function withKeyHint(id: GlobalActionId, title: string): string {
  const k = shortcutKey(id)
  return k ? `${title} (${k})` : title
}

export interface GlobalAction {
  /** Why the action is unavailable, or '' when it can run. */
  disabled: ComputedRef<string>
  run: () => Promise<unknown>
}

/** The top bar's global actions, shared by its buttons and the shortcuts. */
export function useGlobalActions(): Record<GlobalActionId, GlobalAction> {
  const st = useStatusStore()
  const settings = useSettingsStore()
  const units = useUnitsStore()

  const noSettings = computed(() => (settings.settings ? '' : 'Settings not loaded'))
  const needBackup = computed(
    () => st.needRom || (st.status?.protectionBackups ? '' : 'No backups yet'),
  )
  const needRom = computed(() => st.needRom)

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
  }
}

function isTyping(el: Element | null): boolean {
  if (!(el instanceof HTMLElement)) return false
  if (el.isContentEditable) return true
  return !!el.closest('input, textarea, select, [contenteditable]:not([contenteditable="false"])')
}

/** Registers the global shortcuts on window for the component's lifetime. */
export function useShortcuts() {
  const actions = useGlobalActions()
  const dlg = useDialogStore()

  function onKey(e: KeyboardEvent) {
    if (e.defaultPrevented || e.repeat) return
    if (e.ctrlKey || e.metaKey || e.altKey || e.shiftKey) return
    const s = SHORTCUTS.find((x) => x.key === e.key)
    if (!s) return
    if (isTyping(e.target as Element | null) || isTyping(document.activeElement)) return
    if (dlg.prompt || dlg.menu || document.querySelector('[role="dialog"]')) return
    const a = actions[s.action]
    if (a.disabled.value) return
    e.preventDefault()
    void a.run()
  }

  onMounted(() => window.addEventListener('keydown', onKey))
  onBeforeUnmount(() => window.removeEventListener('keydown', onKey))
}
