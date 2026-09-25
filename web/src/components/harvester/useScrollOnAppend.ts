import { nextTick, watch, type Ref } from 'vue'

/** True when `next` is `prev` with one or more keys added at the end. */
export function appended(prev: readonly string[], next: readonly string[]): boolean {
  return next.length > prev.length && prev.every((k, i) => next[i] === k)
}

/**
 * Scrolls the list to the bottom when keys are appended. Other changes
 * (removal, rename, reorder, replacing the list) leave the scroll position alone.
 */
export function useScrollOnAppend(el: Ref<HTMLElement | null>, keys: () => string[]) {
  watch(keys, async (next, prev) => {
    if (!appended(prev ?? [], next)) return
    await nextTick()
    const e = el.value
    if (e) e.scrollTop = e.scrollHeight
  })
}
