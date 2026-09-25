import { act } from '@/stores/log'
import { useSettingsStore } from '@/stores/settings'
import type { SettingsPatch } from '@/api/types'

/** Returns a function that PATCHes settings, reporting errors to the log. */
export function useSettingsPatch() {
  const store = useSettingsStore()
  return (p: SettingsPatch) => act(() => store.patch(p))
}

type RangesPatch = NonNullable<NonNullable<SettingsPatch['nightmare']>['ranges']>

/** `{ "<precision>": {min, max} }` for a ranges PATCH. */
export function rangesPatch(precision: number, v: { min: string; max: string }): RangesPatch {
  return { [String(precision)]: v } as RangesPatch
}
