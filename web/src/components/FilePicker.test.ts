import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import FilePicker from './ui/FilePicker.vue'
import ConnectPopover from './ConnectPopover.vue'
import { errorResponse, mockFetch } from '@/test/fetch'
import { statusFixture } from '@/test/fixtures'
import { useStatusStore } from '@/stores/status'
import type { DirListing } from '@/api/types'

const tid = (id: string) => `[data-testid="${id}"]`

const tree: Record<string, DirListing> = {
  '/home/me': {
    path: '/home/me',
    parent: '/home',
    entries: [
      { name: 'roms', path: '/home/me/roms', dir: true, size: 0 },
      { name: 'top.nds', path: '/home/me/top.nds', dir: false, size: 2048 },
    ],
  },
  '/home/me/roms': {
    path: '/home/me/roms',
    parent: '/home/me',
    entries: [{ name: 'game.nds', path: '/home/me/roms/game.nds', dir: false, size: 512 }],
  },
}

function mockBrowse() {
  return mockFetch({
    'GET /api/browse': (c) => {
      const l = tree[c.query.path ?? '/home/me']
      return l ?? errorResponse(404, 'NOT_FOUND', `${c.query.path}: no such file or directory`)
    },
  })
}

beforeEach(() => {
  setActivePinia(createPinia())
  localStorage.clear()
})
afterEach(() => vi.unstubAllGlobals())

describe('FilePicker', () => {
  it('navigates into a directory and selects a file', async () => {
    const { calls } = mockBrowse()
    const w = mount(FilePicker)
    await flushPromises()
    expect(calls[0]!.query).toEqual({})
    expect((w.find(tid('picker-path')).element as HTMLInputElement).value).toBe('/home/me')
    expect(w.findAll(tid('picker-dir')).map((b) => b.attributes('data-name'))).toEqual(['roms'])
    expect(w.find(tid('picker-parent')).exists()).toBe(true)

    await w.find(tid('picker-dir')).trigger('click')
    await flushPromises()
    expect(calls.at(-1)!.query).toEqual({ path: '/home/me/roms' })
    expect(localStorage.getItem('rtcvish.browse.dir')).toBe('/home/me/roms')

    await w.find('[data-name="game.nds"]').trigger('click')
    expect(w.emitted('select')).toEqual([['/home/me/roms/game.nds']])
  })

  it('goes up, goes to a typed path, shows errors and closes on Escape', async () => {
    localStorage.setItem('rtcvish.browse.dir', '/gone')
    const { calls } = mockBrowse()
    const w = mount(FilePicker)
    await flushPromises()
    // A remembered directory that vanished falls back to the default.
    expect(calls.map((c) => c.query.path)).toEqual(['/gone', undefined])
    expect(w.find(tid('picker-error')).exists()).toBe(false)

    await w.find(tid('picker-path')).setValue('/nope')
    await w.find(tid('picker-go')).trigger('submit')
    await flushPromises()
    expect(w.find(tid('picker-error')).text()).toContain('no such file')

    await w.find(tid('picker-path')).setValue('/home/me/roms')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(w.find(tid('picker-error')).exists()).toBe(false)
    await w.find(tid('picker-parent')).trigger('click')
    await flushPromises()
    expect(calls.at(-1)!.query).toEqual({ path: '/home/me' })

    await w.find(tid('file-picker')).trigger('keydown', { key: 'Escape' })
    expect(w.emitted('close')).toHaveLength(1)
  })
})

describe('ConnectPopover', () => {
  it('fills the ROM path from the picker and explains a disabled Load ROM', async () => {
    mockFetch({
      'GET /api/emulators': [],
      'GET /api/browse': (c) => tree[c.query.path ?? '/home/me'],
    })
    const st = useStatusStore()
    st.set(statusFixture({ connected: false }))
    const w = mount(ConnectPopover)
    await flushPromises()

    const load = () => w.find(tid('rom-load'))
    expect(load().attributes('disabled')).toBeDefined()
    expect(load().attributes('title')).toContain('Emulator not connected')

    await w.find(tid('rom-browse')).trigger('click')
    await flushPromises()
    await w.find('[data-name="top.nds"]').trigger('click')
    expect(w.find(tid('file-picker')).exists()).toBe(false)
    expect((w.find(tid('rom-path')).element as HTMLInputElement).value).toBe('/home/me/top.nds')

    st.set(statusFixture({ connected: true }))
    await flushPromises()
    expect(load().attributes('disabled')).toBeUndefined()

    await w.find(tid('rom-path')).setValue('')
    expect(load().attributes('title')).toContain('ROM path')
  })
})
