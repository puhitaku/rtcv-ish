import { computed } from 'vue'
import type { StashKey } from '@/api/types'
import { act, useLogStore } from '@/stores/log'
import { useSavestatesStore } from '@/stores/savestates'
import { useStashStore } from '@/stores/stash'
import { useStatusStore } from '@/stores/status'
import { useStockpileStore } from '@/stores/stockpile'
import { useUiStore } from '@/stores/ui'

/** Glitch Harvester actions shared by its four areas. */
export function useHarvester() {
  const ui = useUiStore()
  const st = useStatusStore()
  const stash = useStashStore()
  const stockpile = useStockpileStore()
  const savestates = useSavestatesStore()
  const log = useLogStore()

  const merging = computed(() => ui.stockpileSelection.length > 1)

  /** The single selected key of the list used last. */
  const selectedKey = computed<string | null>(() => {
    const sel = ui.ghSource === 'stash' ? ui.stashSelection : ui.stockpileSelection
    return sel.length === 1 ? sel[0]! : null
  })

  const slotKey = computed(() =>
    ui.selectedSlot == null ? undefined : savestates.get(ui.selectedSlot)?.key,
  )

  const mainLabel = computed(() => {
    if (merging.value) return 'Merge'
    return { corrupt: 'Corrupt', inject: 'Inject', original: 'Original' }[ui.gh.mode]
  })

  const mainReason = computed(() => {
    if (st.needRom) return st.needRom
    if (merging.value) return ''
    const needSlot =
      ui.selectedSlot == null
        ? 'Select a savestate slot'
        : !slotKey.value
          ? 'The selected slot is empty'
          : ''
    switch (ui.gh.mode) {
      case 'corrupt':
        return needSlot
      case 'inject':
        return needSlot || (selectedKey.value ? '' : 'Select a stash or stockpile item')
      case 'original':
        return selectedKey.value ? '' : 'Select a stash or stockpile item'
    }
    return ''
  })

  function selectNew(k: StashKey | undefined) {
    if (!k) return
    ui.ghSource = 'stash'
    ui.stashSelection = [k.key]
  }

  async function mainAction() {
    if (merging.value) {
      selectNew(await act(() => stash.merge([...ui.stockpileSelection]), 'merged'))
      return
    }
    const slot = ui.selectedSlot
    const key = selectedKey.value
    switch (ui.gh.mode) {
      case 'corrupt': {
        if (slot == null) return
        const k = await act(() => stash.corrupt(slot, ui.gh.autoLoadState))
        if (!k) return
        log.add('info', `corrupted: ${k.alias || k.key} (${k.unitCount} units)`)
        if (ui.gh.stashResults) selectNew(k)
        else await act(() => stash.remove(k.key))
        return
      }
      case 'inject':
        if (slot == null || !key) return
        selectNew(await act(() => stash.inject(key, slot), 'injected'))
        return
      case 'original':
        if (!key) return
        await act(() => stash.original(key), 'original state loaded')
        return
    }
  }

  const rerollReason = computed(
    () => st.needRom || (selectedKey.value ? '' : 'Select a stash or stockpile item'),
  )

  async function rerollSelected() {
    const key = selectedKey.value
    if (key) selectNew(await act(() => stash.reroll(key), 'rerolled'))
  }

  function runStash(key: string) {
    return act(() => stash.run(key))
  }

  function runStockpile(key: string) {
    return act(() => stockpile.run(key))
  }

  return {
    merging,
    selectedKey,
    mainLabel,
    mainReason,
    mainAction,
    rerollReason,
    rerollSelected,
    runStash,
    runStockpile,
  }
}

/** Selection update for click / ctrl-click / shift-click on a list of keys. */
export function clickSelect(
  current: string[],
  all: string[],
  key: string,
  ev: { ctrlKey: boolean; metaKey: boolean; shiftKey: boolean },
): string[] {
  if (ev.ctrlKey || ev.metaKey) {
    return current.includes(key) ? current.filter((k) => k !== key) : [...current, key]
  }
  if (ev.shiftKey && current.length) {
    const a = all.indexOf(current[current.length - 1]!)
    const b = all.indexOf(key)
    if (a >= 0 && b >= 0) return all.slice(Math.min(a, b), Math.max(a, b) + 1)
  }
  return [key]
}
