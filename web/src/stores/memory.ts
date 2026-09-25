import { defineStore } from 'pinia'
import { computed, ref, watch } from 'vue'
import type { EmuUnit, FreezeMode, Layer } from '@/api/types'
import { readMemory, writeMemory } from '@/api/memory'
import { hexToBytes } from '@/lib/hex'
import { newUnit } from '@/lib/layer'
import { loadJSON, save } from '@/lib/storage'
import { useDomainsStore } from './domains'
import { useUnitsStore } from './units'
import { effectiveFreezeMode } from '@/lib/freeze'
import { useSettingsStore } from './settings'
import { useStatusStore } from './status'

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

/**
 * Unit that rewrites `data` forever (lifetime 0); the core enforces it
 * with the `freezeMode` setting.
 */
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

/** A scheduled unit that holds its bytes: an infinite value unit. */
export function isFreezeUnit(u: EmuUnit): boolean {
  return u.lifetime === 0 && u.value !== undefined && !u.store
}

/** Memory panel state. Frozen cells come from the server's scheduled units. */
export const useMemoryStore = defineStore('memory', () => {
  const domain = ref('')
  const address = ref(0)
  const data = ref<Uint8Array>(new Uint8Array())
  const units = useUnitsStore()
  /** Scheduled infinite value units, newest last. */
  const freezes = computed(() => units.units.filter(isFreezeUnit))
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

  /** Freeze units covering byte `addr` of domain `d`, newest last. */
  function frozenUnits(d: string, addr: number): EmuUnit[] {
    return freezes.value.filter(
      (u) => u.domain === d && addr >= u.address && addr < u.address + u.size,
    )
  }

  /** The newest freeze unit covering the byte, if any. */
  function frozenAt(d: string, addr: number): EmuUnit | undefined {
    return frozenUnits(d, addr).at(-1)
  }

  /** The mode a new freeze gets: the setting (default hard) as the emulator supports it. */
  const freezeMode = computed<FreezeMode>(() =>
    effectiveFreezeMode(
      useSettingsStore().settings?.freezeMode ?? 'hard',
      useStatusStore().status?.emulator?.capabilities,
    ),
  )

  async function freeze(addr: number, size: number) {
    const f: Freeze = {
      domain: domain.value,
      address: addr,
      size,
      data: await readMemory(domain.value, addr, size),
    }
    await units.apply(freezeLayer([f]), false)
    await units.refresh()
  }

  /** Removes the freeze units covering the byte, and nothing else. */
  async function unfreeze(addr: number) {
    const us = frozenUnits(domain.value, addr)
    if (!us.length) return
    try {
      for (const u of us) await units.remove(u.id)
    } finally {
      await units.refresh()
    }
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
    frozenUnits,
    freezeMode,
    freeze,
    unfreeze,
  }
})
