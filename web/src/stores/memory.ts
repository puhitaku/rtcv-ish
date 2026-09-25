import { defineStore } from 'pinia'
import { ref, watch } from 'vue'
import type { Layer } from '@/api/types'
import { readMemory, writeMemory } from '@/api/memory'
import { hexToBytes } from '@/lib/hex'
import { newUnit } from '@/lib/layer'
import { loadJSON, save } from '@/lib/storage'
import { useDomainsStore } from './domains'
import { useUnitsStore } from './units'

/** Hex view rows are this many bytes; jumps align to a row. */
export const ROW = 16

const BITMAPS_KEY = 'rtcvish.memoryBitmaps'

export interface BitmapPrefs {
  /** Domain name -> collapsed. Hidden domains start collapsed. */
  collapsed: Record<string, boolean>
  /** Domain name -> stride override; absent means auto. */
  stride: Record<string, number>
}

export interface Freeze {
  domain: string
  address: number
  size: number
  /** Frozen bytes in memory order. */
  data: string
}

/** Unit that rewrites `data` every frame forever (lifetime 0). */
export function freezeLayer(fs: Freeze[]): Layer {
  return {
    note: 'hex editor freeze',
    units: fs.map((f) => ({
      ...newUnit(f.domain, f.address),
      precision: f.size,
      // Value is little-endian and written as-is when bigEndian is false,
      // so memory-order bytes go in unchanged.
      value: f.data,
      lifetime: 0,
      note: 'freeze',
    })),
  }
}

/** Memory panel state: the view and the freezes it created. */
export const useMemoryStore = defineStore('memory', () => {
  const domain = ref('')
  const address = ref(0)
  const data = ref<Uint8Array>(new Uint8Array())
  const freezes = ref<Freeze[]>([])
  /** Exact byte of the last jump; `seq` changes on every jump. */
  const focus = ref<{ address: number; seq: number } | null>(null)

  const bitmaps = ref(loadJSON<BitmapPrefs>(BITMAPS_KEY, { collapsed: {}, stride: {} }))
  watch(bitmaps, (v) => save(BITMAPS_KEY, v), { deep: true })

  function setDomain(d: string) {
    domain.value = d
    address.value = 0
    data.value = new Uint8Array()
  }

  /** Shows `addr` of domain `d` in the hex view (row-aligned, clamped to the domain). */
  function jump(d: string, addr: number) {
    if (d !== domain.value) setDomain(d)
    const size = useDomainsStore().byName(d)?.size
    const a = size ? Math.min(Math.max(0, addr), size - 1) : Math.max(0, addr)
    address.value = a - (a % ROW)
    focus.value = { address: a, seq: (focus.value?.seq ?? 0) + 1 }
  }

  async function read(size: number) {
    if (!domain.value) return
    data.value = hexToBytes(await readMemory(domain.value, address.value, size))
  }

  async function write(addr: number, hex: string) {
    await writeMemory(domain.value, addr, hex)
  }

  function frozenAt(d: string, addr: number): Freeze | undefined {
    return freezes.value.find(
      (f) => f.domain === d && addr >= f.address && addr < f.address + f.size,
    )
  }

  async function freeze(addr: number, size: number) {
    const f: Freeze = {
      domain: domain.value,
      address: addr,
      size,
      data: await readMemory(domain.value, addr, size),
    }
    await useUnitsStore().apply(freezeLayer([f]), false)
    freezes.value = [...freezes.value.filter((x) => x !== frozenAt(f.domain, addr)), f]
  }

  /**
   * The API can only clear all scheduled units, so unfreezing clears them
   * and re-applies the remaining freezes.
   */
  async function unfreeze(addr: number) {
    const f = frozenAt(domain.value, addr)
    if (!f) return
    const rest = freezes.value.filter((x) => x !== f)
    const units = useUnitsStore()
    await units.clear()
    if (rest.length) await units.apply(freezeLayer(rest), false)
    freezes.value = rest
  }

  return {
    domain,
    address,
    data,
    freezes,
    focus,
    bitmaps,
    setDomain,
    jump,
    read,
    write,
    frozenAt,
    freeze,
    unfreeze,
  }
})
