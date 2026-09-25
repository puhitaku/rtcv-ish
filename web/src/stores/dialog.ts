import { defineStore } from 'pinia'
import { ref } from 'vue'

export interface PromptState {
  title: string
  value: string
  /** Confirmation only: no text input. */
  confirm: boolean
  resolve: (v: string | null) => void
}

export interface MenuItem {
  label: string
  testid: string
  disabled?: boolean
  action: () => void
}

export interface MenuState {
  x: number
  y: number
  items: MenuItem[]
}

/** In-page prompt/confirm dialogs and context menus. */
export const useDialogStore = defineStore('dialog', () => {
  const prompt = ref<PromptState | null>(null)
  const menu = ref<MenuState | null>(null)

  function close(v: string | null) {
    const p = prompt.value
    prompt.value = null
    p?.resolve(v)
  }

  function ask(title: string, value = ''): Promise<string | null> {
    if (prompt.value) close(null)
    return new Promise((resolve) => {
      prompt.value = { title, value, confirm: false, resolve }
    })
  }

  async function confirm(title: string): Promise<boolean> {
    if (prompt.value) close(null)
    const r = await new Promise<string | null>((resolve) => {
      prompt.value = { title, value: '', confirm: true, resolve }
    })
    return r !== null
  }

  function openMenu(ev: MouseEvent, items: MenuItem[]) {
    ev.preventDefault()
    menu.value = { x: ev.clientX, y: ev.clientY, items }
  }

  function closeMenu() {
    menu.value = null
  }

  return { prompt, menu, ask, confirm, close, openMenu, closeMenu }
})
