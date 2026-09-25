import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import type { Layer, Unit } from '@/api/types'
import { readMemory } from '@/api/memory'
import * as L from '@/lib/layer'
import type { Rand } from '@/lib/prng'
import { useStashStore } from './stash'
import { useStockpileStore } from './stockpile'
import type { EditorTarget } from './ui'

/** Blast Editor working copy. Edits are local until saved with PUT .../layer. */
export const useEditorStore = defineStore('editor', () => {
  const target = ref<EditorTarget | null>(null)
  const layer = ref<Layer>(L.emptyLayer())
  const dirty = ref(false)
  const selection = ref<number[]>([])
  let rand: Rand = Math.random

  const stash = useStashStore()
  const stockpile = useStockpileStore()

  const selectedUnits = computed(() =>
    selection.value.map((i) => layer.value.units[i]).filter((u): u is Unit => !!u),
  )

  function setRand(r: Rand) {
    rand = r
  }

  async function open(t: EditorTarget) {
    let l: Layer
    if (t.kind === 'stash') l = await stash.getLayer(t.key)
    else if (t.kind === 'stockpile') l = await stockpile.getLayer(t.key)
    else l = L.emptyLayer()
    target.value = t
    layer.value = l
    dirty.value = false
    selection.value = []
  }

  function replace(l: Layer, keepSelection = true) {
    layer.value = l
    dirty.value = true
    if (keepSelection) selection.value = selection.value.filter((i) => i < l.units.length)
    else selection.value = []
  }

  /** Selected indices, or every index when nothing is selected. */
  function scope(): number[] {
    return selection.value.length ? [...selection.value] : []
  }

  async function save() {
    const t = target.value
    if (!t || t.kind === 'new') throw new Error('this layer has no stash or stockpile key')
    const l = {
      ...layer.value,
      units: layer.value.units.map((u) => ({ ...u, tilt: u.tilt || '0' })),
    }
    layer.value =
      t.kind === 'stash' ? await stash.putLayer(t.key, l) : await stockpile.putLayer(t.key, l)
    dirty.value = false
  }

  async function bake() {
    replace(await L.bake(layer.value, scope(), readMemory))
  }

  return {
    target,
    layer,
    dirty,
    selection,
    selectedUnits,
    setRand,
    open,
    replace,
    save,
    bake,
    disable50: () => replace(L.disable50(layer.value, rand)),
    invertDisabled: () => replace(L.invertDisabled(layer.value)),
    removeDisabled: () => replace(L.removeDisabled(layer.value), false),
    enableAll: () => replace(L.enableAll(layer.value)),
    disableAll: () => replace(L.disableAll(layer.value)),
    removeSelected: () => replace(L.removeIndices(layer.value, selection.value), false),
    duplicate: () => replace(L.duplicate(layer.value, selection.value)),
    addRow: (domain: string) => {
      replace(L.addUnit(layer.value, L.newUnit(domain)))
      selection.value = [layer.value.units.length - 1]
    },
    shift: (field: L.ShiftField, amount: number) =>
      replace(L.shift(layer.value, field, amount, scope())),
    breakDown: () => replace(L.breakDown(layer.value, scope()), false),
    sanitize: () => replace(L.sanitizeDuplicates(layer.value), false),
    applyToSelection: (patch: Partial<Unit>) =>
      replace(L.applyToIndices(layer.value, selection.value, patch)),
  }
})
