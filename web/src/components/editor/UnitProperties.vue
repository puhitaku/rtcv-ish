<script setup lang="ts">
import { computed } from 'vue'
import BaseToggle from '@/components/ui/BaseToggle.vue'
import HelpTip from '@/components/ui/HelpTip.vue'
import ListSelect from '@/components/ui/ListSelect.vue'
import type { Unit } from '@/api/types'
import { FIELD_HELP } from '@/lib/fieldHelp'
import { valueDisplay, valueFromDisplay } from '@/lib/hex'
import { fieldBases, formatNum, parseNum, parseSafe, type NumField } from '@/lib/numBase'
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
  if (v === undefined || v == null) return ''
  if (k in fieldBases) return formatNum(v as number | string, fieldBases[k as NumField])
  if (k === 'value') return valueDisplay(v as string)
  return String(v)
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

function commitNum(k: NumField, t: string) {
  const base = fieldBases[k]
  switch (k) {
    case 'tilt': {
      const n = parseNum(t, base, true)
      if (n == null) return bad(`invalid tilt "${t}" (${base})`)
      return ed.applyToSelection({ tilt: n.toString() })
    }
    case 'loopTiming': {
      if (t === '') return ed.applyToSelection({ loopTiming: null })
      const n = parseSafe(t, base)
      if (n == null) return bad(`invalid loop timing "${t}" (${base})`)
      return ed.applyToSelection({ loopTiming: n })
    }
    default: {
      const n = parseSafe(t, base)
      if (n == null || (k === 'precision' && n < 1)) return bad(`invalid ${k} "${t}" (${base})`)
      return ed.applyToSelection({ [k]: n })
    }
  }
}

function commit(k: keyof Unit, raw: string) {
  const t = raw.trim()
  if (t === '' && k !== 'note' && k !== 'loopTiming' && k !== 'limiterList') return
  if (k in fieldBases) return commitNum(k as NumField, t)
  if (k === 'value') {
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
  return ed.applyToSelection({ [k]: t } as Partial<Unit>)
}

function ph(k: keyof Unit): string {
  if (k === 'loopTiming') return '(default)'
  return common(k) === undefined ? '(mixed)' : ''
}

function val(e: Event) {
  return (e.target as HTMLInputElement | HTMLSelectElement).value
}

/** Numeric fields between the domain select and the source select, and after it. */
const numsA = [
  { k: 'address', label: 'Address' },
  { k: 'precision', label: 'Precision' },
] as const
const numsB = [
  { k: 'tilt', label: 'Tilt' },
  { k: 'executeFrame', label: 'Execute Frame' },
  { k: 'lifetime', label: 'Lifetime' },
  { k: 'loopTiming', label: 'Loop Timing' },
] as const

const flags = [
  { k: 'enabled', label: 'Enabled' },
  { k: 'locked', label: 'Locked' },
  { k: 'bigEndian', label: 'Big Endian' },
  { k: 'loop', label: 'Loop' },
  { k: 'invertLimiter', label: 'Invert Limiter' },
  { k: 'generatedUsingValueList', label: 'From value list' },
] as const
</script>

<template>
  <aside class="box flex w-72 shrink-0 flex-col gap-1 p-2" data-testid="be-properties">
    <div class="box-title -mx-2 -mt-2 mb-1">
      Properties <span class="font-mono normal-case">({{ units.length }} selected)</span>
    </div>
    <div v-if="!units.length" class="text-dim">Select rows to edit them.</div>
    <template v-else>
      <div class="grid grid-cols-[6.5rem_1fr] items-center gap-1">
        <HelpTip class="lbl" :text="FIELD_HELP.domain">Domain</HelpTip>
        <select
          class="input"
          :value="display('domain')"
          aria-label="Domain"
          data-testid="be-prop-domain"
          @change="commit('domain', val($event))"
        >
          <option value="" disabled>(mixed)</option>
          <option v-for="d in domainNames" :key="d" :value="d">{{ d }}</option>
        </select>
        <template v-for="f in numsA" :key="f.k">
          <HelpTip class="lbl" :text="FIELD_HELP[f.k]">{{ f.label }}</HelpTip>
          <span class="flex min-w-0 gap-1">
            <input
              class="input min-w-0 flex-1 font-mono"
              :value="display(f.k)"
              :placeholder="ph(f.k)"
              :aria-label="f.label"
              :data-testid="`be-prop-${f.k}`"
              @change="commit(f.k, val($event))"
            />
            <BaseToggle :field="f.k" />
          </span>
        </template>
        <HelpTip class="lbl" :text="FIELD_HELP.source">Source</HelpTip>
        <select
          class="input"
          :value="display('source')"
          aria-label="Source"
          data-testid="be-prop-source"
          @change="commit('source', val($event))"
        >
          <option value="" disabled>(mixed)</option>
          <option value="value">VALUE</option>
          <option value="store">STORE</option>
        </select>
        <HelpTip class="lbl" :text="FIELD_HELP.value">Value (hex)</HelpTip>
        <input
          class="input font-mono"
          :value="display('value')"
          :placeholder="ph('value')"
          aria-label="Value"
          data-testid="be-prop-value"
          @change="commit('value', val($event))"
        />
        <template v-for="f in numsB" :key="f.k">
          <HelpTip class="lbl" :text="FIELD_HELP[f.k]">{{ f.label }}</HelpTip>
          <span class="flex min-w-0 gap-1">
            <input
              class="input min-w-0 flex-1 font-mono"
              :value="display(f.k)"
              :placeholder="ph(f.k)"
              :aria-label="f.label"
              :data-testid="`be-prop-${f.k}`"
              @change="commit(f.k, val($event))"
            />
            <BaseToggle :field="f.k" />
          </span>
        </template>
        <HelpTip class="lbl" :text="FIELD_HELP.storeTime">Store Time</HelpTip>
        <select
          class="input"
          :value="display('storeTime')"
          aria-label="Store Time"
          data-testid="be-prop-storeTime"
          @change="commit('storeTime', val($event))"
        >
          <option value="" disabled>(mixed)</option>
          <option value="immediate">IMMEDIATE</option>
          <option value="preexecute">PREEXECUTE</option>
        </select>
        <HelpTip class="lbl" :text="FIELD_HELP.storeType">Store Type</HelpTip>
        <select
          class="input"
          :value="display('storeType')"
          aria-label="Store Type"
          data-testid="be-prop-storeType"
          @change="commit('storeType', val($event))"
        >
          <option value="" disabled>(mixed)</option>
          <option value="once">ONCE</option>
          <option value="continuous">CONTINUOUS</option>
        </select>
        <HelpTip class="lbl" :text="FIELD_HELP.sourceDomain">Source Domain</HelpTip>
        <select
          class="input"
          :value="display('sourceDomain')"
          aria-label="Source Domain"
          data-testid="be-prop-sourceDomain"
          @change="commit('sourceDomain', val($event))"
        >
          <option value="">(none / mixed)</option>
          <option v-for="d in domainNames" :key="d" :value="d">{{ d }}</option>
        </select>
        <HelpTip class="lbl" :text="FIELD_HELP.sourceAddress">Source Address</HelpTip>
        <span class="flex min-w-0 gap-1">
          <input
            class="input min-w-0 flex-1 font-mono"
            :value="display('sourceAddress')"
            :placeholder="ph('sourceAddress')"
            aria-label="Source Address"
            data-testid="be-prop-sourceAddress"
            @change="commit('sourceAddress', val($event))"
          />
          <BaseToggle field="sourceAddress" />
        </span>
        <HelpTip class="lbl" :text="FIELD_HELP.limiterTime">Limiter Time</HelpTip>
        <select
          class="input"
          :value="display('limiterTime')"
          aria-label="Limiter Time"
          data-testid="be-prop-limiterTime"
          @change="commit('limiterTime', val($event))"
        >
          <option value="" disabled>(mixed)</option>
          <option value="none">NONE</option>
          <option value="generate">GENERATE</option>
        </select>
        <HelpTip class="lbl" :text="FIELD_HELP.limiterList">Limiter List</HelpTip>
        <ListSelect
          :model-value="display('limiterList')"
          testid="be-prop-limiterList"
          @update:model-value="commit('limiterList', $event)"
        />
        <HelpTip class="lbl" :text="FIELD_HELP.note">Note</HelpTip>
        <input
          class="input"
          :value="display('note')"
          :placeholder="ph('note')"
          aria-label="Note"
          data-testid="be-prop-note"
          @change="commit('note', val($event))"
        />
      </div>
      <div class="grid grid-cols-2 gap-x-2">
        <HelpTip
          v-for="f in flags"
          :key="f.k"
          tag="label"
          :focusable="false"
          :text="FIELD_HELP[f.k]"
          class="flex items-center gap-1"
        >
          <input
            type="checkbox"
            :checked="common(f.k) === true"
            :indeterminate="common(f.k) === undefined"
            :data-testid="`be-prop-${f.k}`"
            @change="ed.applyToSelection({ [f.k]: ($event.target as HTMLInputElement).checked })"
          />
          {{ f.label }}
        </HelpTip>
      </div>
    </template>
  </aside>
</template>
