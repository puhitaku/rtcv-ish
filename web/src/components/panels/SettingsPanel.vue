<script setup lang="ts">
import { computed, ref } from 'vue'
import BoxPanel from '@/components/ui/BoxPanel.vue'
import NumField from '@/components/ui/NumField.vue'
import { useSettingsPatch } from '@/components/engine/useSettingsPatch'
import { useListsStore } from '@/stores/lists'
import { act } from '@/stores/log'
import { useSettingsStore } from '@/stores/settings'
import { useStatusStore } from '@/stores/status'
import type { Settings } from '@/api/types'

const store = useSettingsStore()
const lists = useListsStore()
const st = useStatusStore()
const patch = useSettingsPatch()

const rerollFlags: { k: keyof Settings['reroll']; label: string }[] = [
  { k: 'address', label: 'Reroll address' },
  { k: 'sourceAddress', label: 'Reroll source address' },
  { k: 'domain', label: 'Reroll domain' },
  { k: 'sourceDomain', label: 'Reroll source domain' },
  { k: 'followCustomEngine', label: 'Reroll follows Custom Engine' },
]

const fileInput = ref<HTMLInputElement | null>(null)
const listName = ref('')

async function onFile(e: Event) {
  const input = e.target as HTMLInputElement
  const f = input.files?.[0]
  input.value = ''
  if (!f) return
  const name = listName.value.trim() || undefined
  await act(() => lists.upload(f, f.name, name), `list uploaded: ${name ?? f.name}`)
  listName.value = ''
}

const emu = computed(() => st.status?.emulator)
function chk(e: Event) {
  return (e.target as HTMLInputElement).checked
}
</script>

<template>
  <div class="grid grid-cols-1 gap-2 md:grid-cols-2 xl:grid-cols-3" data-testid="settings-panel">
    <BoxPanel title="Reroll Settings">
      <template v-if="store.settings">
        <label v-for="f in rerollFlags" :key="f.k" class="flex items-center gap-2">
          <input
            type="checkbox"
            :checked="store.settings.reroll[f.k]"
            :data-testid="`reroll-${f.k}`"
            @change="patch({ reroll: { [f.k]: chk($event) } })"
          />
          {{ f.label }}
        </label>
      </template>
    </BoxPanel>

    <BoxPanel title="Step Settings">
      <template v-if="store.settings">
        <label class="flex items-center justify-between gap-2">
          <span>Max infinite units</span>
          <NumField
            :model-value="store.settings.maxInfiniteUnits"
            :min="1"
            testid="settings-max-infinite-units"
            @update:model-value="patch({ maxInfiniteUnits: $event })"
          />
        </label>
        <label class="flex items-center gap-2">
          <input
            type="checkbox"
            :checked="store.settings.lockUnits"
            data-testid="settings-lock-units"
            @change="patch({ lockUnits: chk($event) })"
          />
          Lock units
        </label>
      </template>
    </BoxPanel>

    <BoxPanel title="Game Protection">
      <template v-if="store.settings">
        <label class="flex items-center gap-2">
          <input
            type="checkbox"
            :checked="store.settings.gameProtection.enabled"
            data-testid="settings-protection-enabled"
            @change="patch({ gameProtection: { enabled: chk($event) } })"
          />
          Enabled
        </label>
        <label class="flex items-center justify-between gap-2">
          <span>Backup every (seconds)</span>
          <NumField
            :model-value="store.settings.gameProtection.intervalSeconds"
            :min="1"
            testid="settings-protection-interval"
            @update:model-value="patch({ gameProtection: { intervalSeconds: $event } })"
          />
        </label>
        <label class="flex items-center justify-between gap-2">
          <span>Backups kept</span>
          <NumField
            :model-value="store.settings.gameProtection.keep"
            :min="1"
            testid="settings-protection-keep"
            @update:model-value="patch({ gameProtection: { keep: $event } })"
          />
        </label>
        <div class="text-dim">held now: {{ st.status?.protectionBackups ?? 0 }}</div>
      </template>
    </BoxPanel>

    <BoxPanel title="Lists" class="md:col-span-2">
      <div class="flex flex-wrap items-center gap-1">
        <input
          v-model="listName"
          class="input w-40"
          placeholder="name (optional)"
          data-testid="list-name"
        />
        <button class="btn" data-testid="list-upload" @click="fileInput?.click()">
          Upload .txt
        </button>
        <input
          ref="fileInput"
          type="file"
          accept=".txt,text/plain"
          class="hidden"
          data-testid="list-upload-file"
          @change="onFile"
        />
        <span class="text-dim">One hex value per line.</span>
      </div>
      <table class="w-full border-collapse text-left" data-testid="list-table">
        <thead class="text-dim">
          <tr>
            <th class="px-1 font-normal">Name</th>
            <th class="px-1 font-normal">Precision</th>
            <th class="px-1 font-normal">Entries</th>
            <th />
          </tr>
        </thead>
        <tbody>
          <tr v-if="!lists.lists.length">
            <td colspan="4" class="px-1 text-dim">no lists</td>
          </tr>
          <tr v-for="l in lists.lists" :key="l.name" data-testid="list-row">
            <td class="px-1 font-mono">{{ l.name }}</td>
            <td class="px-1 font-mono">{{ l.precision * 8 }}-bit</td>
            <td class="px-1 font-mono">{{ l.entries }}</td>
            <td class="px-1 text-right">
              <button
                class="btn"
                :data-testid="`list-delete-${l.name}`"
                @click="act(() => lists.remove(l.name), `list deleted: ${l.name}`)"
              >
                Delete
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </BoxPanel>

    <BoxPanel title="About">
      <dl class="grid grid-cols-[7rem_1fr] gap-x-2 gap-y-0.5">
        <dt class="lbl">Core version</dt>
        <dd class="font-mono" data-testid="about-version">{{ st.status?.version ?? '?' }}</dd>
        <dt class="lbl">Data directory</dt>
        <dd class="font-mono break-all" data-testid="about-data-dir">
          {{ st.status?.dataDir ?? '?' }}
        </dd>
        <dt class="lbl">Emulator</dt>
        <dd class="font-mono" data-testid="about-emulator">
          <template v-if="emu">
            {{ emu.name }} {{ emu.version }} ({{ emu.system }}, protocol {{ emu.protocolVersion }})
          </template>
          <template v-else>not connected</template>
        </dd>
        <dt v-if="emu" class="lbl">Capabilities</dt>
        <dd v-if="emu" class="font-mono">
          {{
            Object.entries(emu.capabilities)
              .filter(([k, v]) => v === true && k !== 'maxPayload')
              .map(([k]) => k)
              .join(', ')
          }}
        </dd>
      </dl>
      <p class="text-dim">
        rtcv-ish is an OS-agnostic reimplementation of RTCV (Real-Time Corruptor Vanguard), released
        under the MIT License. RTCV is © Phil Girard &amp; Daniel Barreiro (MIT); melonDS is
        GPL-3.0-or-later. See LICENSE in the repository for all third-party notices.
      </p>
    </BoxPanel>
  </div>
</template>
