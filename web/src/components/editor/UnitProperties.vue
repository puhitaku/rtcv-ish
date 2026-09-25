<script setup lang="ts">
import { computed } from 'vue'
import ListSelect from '@/components/ui/ListSelect.vue'
import type { Unit } from '@/api/types'
import { isSignedDecimal } from '@/lib/decimal'
import { hex, parseHex, valueDisplay, valueFromDisplay } from '@/lib/hex'
import { useDomainsStore } from '@/stores/domains'
import { useEditorStore } from '@/stores/editor'
import { useLogStore } from '@/stores/log'

/** Side property editor: edits apply to every selected unit. */
const ed = useEditorStore()
const domains = useDomainsStore()
const log = useLogStore()

const units = computed(() => ed.selectedUnits)

function common<K extends keyof Unit>(k: K): Unit[K] | undefined {
  const us = units.value
  if (!us.length) return undefined
  const v = us[0]![k]
  return us.every((u) => u[k] === v) ? v : undefined
}

function display(k: keyof Unit): string {
  const v = common(k)
  if (v === undefined) return ''
  if (k === 'address' || k === 'sourceAddress') return hex(v as number)
  if (k === 'value') return valueDisplay(v as string)
  return v == null ? '' : String(v)
}

const domainNames = computed(() => {
  const s = new Set(domains.domains.map((d) => d.name))
  for (const u of ed.layer.units) {
    if (u.domain) s.add(u.domain)
    if (u.sourceDomain) s.add(u.sourceDomain)
  }
  return [...s]
})

function bad(msg: string) {
  log.add('warn', `Blast Editor: ${msg}`)
}

function commit(k: keyof Unit, raw: string) {
  const t = raw.trim()
  if (t === '' && k !== 'note' && k !== 'loopTiming' && k !== 'limiterList') return
  switch (k) {
    case 'address':
    case 'sourceAddress': {
      const n = parseHex(t)
      if (n == null) return bad(`invalid hex address "${t}"`)
      return ed.applyToSelection({ [k]: n })
    }
    case 'precision':
    case 'executeFrame':
    case 'lifetime': {
      const n = Number(t)
      if (!/^\d+$/.test(t) || (k === 'precision' && n < 1)) return bad(`invalid ${k} "${t}"`)
      return ed.applyToSelection({ [k]: n })
    }
    case 'loopTiming':
      if (t === '') return ed.applyToSelection({ loopTiming: null })
      if (!/^\d+$/.test(t)) return bad(`invalid loop timing "${t}"`)
      return ed.applyToSelection({ loopTiming: Number(t) })
    case 'tilt':
      if (!isSignedDecimal(t)) return bad(`invalid tilt "${t}"`)
      return ed.applyToSelection({ tilt: BigInt(t).toString() })
    case 'value': {
      // Per unit: the value must fit its precision.
      const next = ed.layer.units.map((u, i) => {
        if (!ed.selection.includes(i)) return u
        const v = valueFromDisplay(t, u.precision)
        return v == null ? u : { ...u, value: v }
      })
      if (next.some((u, i) => ed.selection.includes(i) && u === ed.layer.units[i]))
        bad(`value "${t}" does not fit some units`)
      return ed.replace({ ...ed.layer, units: next })
    }
    default:
      return ed.applyToSelection({ [k]: t } as Partial<Unit>)
  }
}

function ph(k: keyof Unit): string {
  return common(k) === undefined ? '(mixed)' : ''
}

function val(e: Event) {
  return (e.target as HTMLInputElement | HTMLSelectElement).value
}

const flags = [
  { k: 'enabled', label: 'Enabled' },
  { k: 'locked', label: 'Locked' },
  { k: 'bigEndian', label: 'Big Endian' },
  { k: 'loop', label: 'Loop' },
  { k: 'invertLimiter', label: 'Invert Limiter' },
] as const
</script>

<template>
  <aside class="box flex w-64 shrink-0 flex-col gap-1 p-2" data-testid="be-properties">
    <div class="box-title -mx-2 -mt-2 mb-1">
      Properties <span class="font-mono normal-case">({{ units.length }} selected)</span>
    </div>
    <div v-if="!units.length" class="text-dim">Select rows to edit them.</div>
    <template v-else>
      <label class="grid grid-cols-[6.5rem_1fr] items-center gap-1">
        <span class="lbl">Domain</span>
        <select
          class="input"
          :value="display('domain')"
          data-testid="be-prop-domain"
          @change="commit('domain', val($event))"
        >
          <option value="" disabled>(mixed)</option>
          <option v-for="d in domainNames" :key="d" :value="d">{{ d }}</option>
        </select>
        <span class="lbl">Address</span>
        <input
          class="input font-mono"
          :value="display('address')"
          :placeholder="ph('address')"
          data-testid="be-prop-address"
          @change="commit('address', val($event))"
        />
        <span class="lbl">Precision</span>
        <input
          class="input font-mono"
          :value="display('precision')"
          :placeholder="ph('precision')"
          data-testid="be-prop-precision"
          @change="commit('precision', val($event))"
        />
        <span class="lbl">Source</span>
        <select
          class="input"
          :value="display('source')"
          data-testid="be-prop-source"
          @change="commit('source', val($event))"
        >
          <option value="" disabled>(mixed)</option>
          <option value="value">VALUE</option>
          <option value="store">STORE</option>
        </select>
        <span class="lbl">Value (hex)</span>
        <input
          class="input font-mono"
          :value="display('value')"
          :placeholder="ph('value')"
          data-testid="be-prop-value"
          @change="commit('value', val($event))"
        />
        <span class="lbl">Tilt</span>
        <input
          class="input font-mono"
          :value="display('tilt')"
          :placeholder="ph('tilt')"
          data-testid="be-prop-tilt"
          @change="commit('tilt', val($event))"
        />
        <span class="lbl">Execute Frame</span>
        <input
          class="input font-mono"
          :value="display('executeFrame')"
          :placeholder="ph('executeFrame')"
          data-testid="be-prop-executeFrame"
          @change="commit('executeFrame', val($event))"
        />
        <span class="lbl">Lifetime</span>
        <input
          class="input font-mono"
          :value="display('lifetime')"
          :placeholder="ph('lifetime')"
          data-testid="be-prop-lifetime"
          @change="commit('lifetime', val($event))"
        />
        <span class="lbl">Loop Timing</span>
        <input
          class="input font-mono"
          :value="display('loopTiming')"
          placeholder="(default)"
          data-testid="be-prop-loopTiming"
          @change="commit('loopTiming', val($event))"
        />
        <span class="lbl">Store Time</span>
        <select
          class="input"
          :value="display('storeTime')"
          data-testid="be-prop-storeTime"
          @change="commit('storeTime', val($event))"
        >
          <option value="" disabled>(mixed)</option>
          <option value="immediate">IMMEDIATE</option>
          <option value="preexecute">PREEXECUTE</option>
        </select>
        <span class="lbl">Store Type</span>
        <select
          class="input"
          :value="display('storeType')"
          data-testid="be-prop-storeType"
          @change="commit('storeType', val($event))"
        >
          <option value="" disabled>(mixed)</option>
          <option value="once">ONCE</option>
          <option value="continuous">CONTINUOUS</option>
        </select>
        <span class="lbl">Source Domain</span>
        <select
          class="input"
          :value="display('sourceDomain')"
          data-testid="be-prop-sourceDomain"
          @change="commit('sourceDomain', val($event))"
        >
          <option value="">(none / mixed)</option>
          <option v-for="d in domainNames" :key="d" :value="d">{{ d }}</option>
        </select>
        <span class="lbl">Source Address</span>
        <input
          class="input font-mono"
          :value="display('sourceAddress')"
          :placeholder="ph('sourceAddress')"
          data-testid="be-prop-sourceAddress"
          @change="commit('sourceAddress', val($event))"
        />
        <span class="lbl">Limiter Time</span>
        <select
          class="input"
          :value="display('limiterTime')"
          data-testid="be-prop-limiterTime"
          @change="commit('limiterTime', val($event))"
        >
          <option value="" disabled>(mixed)</option>
          <option value="none">NONE</option>
          <option value="generate">GENERATE</option>
        </select>
        <span class="lbl">Limiter List</span>
        <ListSelect
          :model-value="display('limiterList')"
          testid="be-prop-limiterList"
          @update:model-value="commit('limiterList', $event)"
        />
        <span class="lbl">Note</span>
        <input
          class="input"
          :value="display('note')"
          :placeholder="ph('note')"
          data-testid="be-prop-note"
          @change="commit('note', val($event))"
        />
      </label>
      <div class="grid grid-cols-2 gap-x-2">
        <label v-for="f in flags" :key="f.k" class="flex items-center gap-1">
          <input
            type="checkbox"
            :checked="common(f.k) === true"
            :indeterminate="common(f.k) === undefined"
            :data-testid="`be-prop-${f.k}`"
            @change="ed.applyToSelection({ [f.k]: ($event.target as HTMLInputElement).checked })"
          />
          {{ f.label }}
        </label>
      </div>
    </template>
  </aside>
</template>
