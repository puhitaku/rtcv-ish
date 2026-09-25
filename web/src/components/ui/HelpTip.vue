<script setup lang="ts">
import { onBeforeUnmount, ref, useId } from 'vue'

/**
 * Hover/focus tooltip panel around a label. The wrapper is focusable
 * unless it contains its own focusable control (`focusable: false`).
 */
const props = withDefaults(
  defineProps<{ text: string; focusable?: boolean; delay?: number; tag?: string }>(),
  { focusable: true, delay: 350, tag: 'span' },
)

const id = `help-${useId()}`
const open = ref(false)
const pos = ref({ left: 0, top: 0 })
const el = ref<HTMLElement | null>(null)
let timer: ReturnType<typeof setTimeout> | undefined

function place() {
  const r = el.value?.getBoundingClientRect()
  if (!r) return
  const width = 256
  pos.value = {
    left: Math.max(4, Math.min(r.left, window.innerWidth - width - 4)),
    top: r.bottom + 4,
  }
}

function show(immediate = false) {
  clearTimeout(timer)
  const go = () => {
    place()
    open.value = true
  }
  if (immediate) go()
  else timer = setTimeout(go, props.delay)
}

function hide() {
  clearTimeout(timer)
  open.value = false
}

onBeforeUnmount(() => clearTimeout(timer))
</script>

<template>
  <component
    :is="tag"
    ref="el"
    :tabindex="focusable ? 0 : undefined"
    :aria-describedby="open ? id : undefined"
    class="help-tip"
    data-help-tip
    @mouseenter="show()"
    @mouseleave="hide"
    @focusin="show()"
    @focusout="hide"
    @keydown.esc="hide"
  >
    <slot />
    <Teleport to="body">
      <div
        v-if="open"
        :id="id"
        role="tooltip"
        class="help-panel"
        :style="{ left: `${pos.left}px`, top: `${pos.top}px` }"
        data-testid="help-tip"
      >
        {{ text }}
      </div>
    </Teleport>
  </component>
</template>

<style scoped>
.help-tip {
  cursor: help;
}
.help-tip:focus-visible {
  outline: 1px solid var(--c-accent);
  outline-offset: 1px;
}
.help-panel {
  position: fixed;
  z-index: 60;
  max-width: 16rem;
  padding: 0.35rem 0.5rem;
  border: 1px solid var(--c-line);
  background: var(--c-panel);
  color: var(--c-fg);
  font-size: 11px;
  font-weight: normal;
  line-height: 1.4;
  text-transform: none;
  letter-spacing: normal;
  white-space: normal;
  pointer-events: none;
  box-shadow: 0 2px 6px rgb(0 0 0 / 0.15);
}
</style>
