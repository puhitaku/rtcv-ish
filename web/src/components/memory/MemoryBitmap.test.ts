import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import MemoryBitmap from './MemoryBitmap.vue'
import { defineComponent, h } from 'vue'
import MemoryPanel from '@/components/panels/MemoryPanel.vue'
import { useShortcuts } from '@/lib/shortcuts'
import { mockFetch, type Call } from '@/test/fetch'
import { statusFixture } from '@/test/fixtures'
import { useDomainsStore } from '@/stores/domains'
import { useMemoryStore } from '@/stores/memory'
import { useStatusStore } from '@/stores/status'
import { useUiStore } from '@/stores/ui'
import type { Domain } from '@/api/types'

const tid = (id: string) => `[data-testid="${id}"]`

const pal: Domain = {
  name: 'PAL',
  size: 2048,
  wordSize: 2,
  bigEndian: false,
  writable: true,
  hidden: false,
  selected: true,
}
const ram: Domain = { ...pal, name: 'RAM', size: 64 << 10, wordSize: 4 }

/** Words whose value is their index, like the endpoint would return. */
function wordsResponse(c: Call): Response {
  const n = Math.ceil(Math.floor(Number(c.query.size) / 2) / Number(c.query.stride))
  const b = new Uint8Array(n * 2)
  for (let i = 0; i < n; i++) {
    b[i * 2] = i & 0xff
    b[i * 2 + 1] = i >> 8
  }
  return new Response(b, { headers: { 'Content-Type': 'application/octet-stream' } })
}

const putImageData = vi.fn()

beforeEach(() => {
  setActivePinia(createPinia())
  localStorage.clear()
  useStatusStore().set(statusFixture())
  putImageData.mockClear()
  vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockImplementation(function (
    this: HTMLCanvasElement,
  ) {
    return {
      createImageData: (w: number, h: number) => ({
        width: w,
        height: h,
        data: new Uint8ClampedArray(w * h * 4),
      }),
      putImageData,
    } as unknown as CanvasRenderingContext2D
  } as never)
  // 64x16 native pixels scaled 10x.
  vi.spyOn(HTMLCanvasElement.prototype, 'getBoundingClientRect').mockReturnValue({
    left: 0,
    top: 0,
    width: 640,
    height: 160,
  } as DOMRect)
})
afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  document.body.innerHTML = ''
})

describe('MemoryBitmap', () => {
  it('fetches the domain, draws it, shows the address on hover and jumps on click', async () => {
    const { calls } = mockFetch({ 'GET /api/memory/PAL/words': wordsResponse })
    const w = mount(MemoryBitmap, { props: { domain: pal }, attachTo: document.body })
    await flushPromises()

    expect(calls).toHaveLength(1)
    expect(w.text()).toContain('2 KiB')
    expect(calls[0]!.query).toEqual({ address: '0', size: '2048', stride: '1' })
    const canvas = w.find(tid('bitmap-canvas-PAL'))
    expect(canvas.attributes('width')).toBe('64')
    expect(canvas.attributes('height')).toBe('16')
    expect(putImageData).toHaveBeenCalledTimes(1)
    const img = putImageData.mock.calls[0]![0] as ImageData
    // Word 0x0001 is blue 1 -> 8.
    expect(Array.from(img.data.slice(4, 8))).toEqual([0, 0, 8, 255])

    // Pixel (3, 2) = index 131 = address 0x106, value 0x0083.
    await canvas.trigger('mousemove', { clientX: 35, clientY: 25 })
    const tip = document.querySelector(tid('bitmap-tip'))
    expect(tip?.textContent).toContain('0106')
    expect(tip?.textContent).toContain('0083')
    await canvas.trigger('mousemove', { clientX: 5, clientY: 5 })
    expect(document.querySelector(tid('bitmap-tip'))?.textContent).toContain('0000')
    await canvas.trigger('mouseleave')
    expect(document.querySelector(tid('bitmap-tip'))).toBeNull()
    expect(calls).toHaveLength(1)

    await canvas.trigger('click', { clientX: 35, clientY: 25 })
    const mem = useMemoryStore()
    expect(mem.domain).toBe('PAL')
    expect(mem.address).toBe(0x100)
    expect(mem.focus?.address).toBe(0x106)
    expect(w.find(tid('bitmap-flash')).exists()).toBe(true)
    w.unmount()
  })

  it('refetches with a user stride and keeps the width', async () => {
    const { calls } = mockFetch({ 'GET /api/memory/PAL/words': wordsResponse })
    const w = mount(MemoryBitmap, { props: { domain: pal } })
    await flushPromises()
    await w.find(tid('bitmap-stride-PAL')).setValue(4)
    await flushPromises()
    expect(calls.at(-1)!.query.stride).toBe('4')
    const canvas = w.find(tid('bitmap-canvas-PAL'))
    expect(canvas.attributes('width')).toBe('64')
    expect(canvas.attributes('height')).toBe('4')
    expect(useMemoryStore().bitmaps.stride).toEqual({ PAL: 4 })

    // 64x4 pixels in the 640x160 box; each pixel stands for 8 bytes:
    // pixel (3, 2) is index 131, address 0x418.
    await canvas.trigger('click', { clientX: 35, clientY: 100 })
    expect(useMemoryStore().focus?.address).toBe(0x418)
  })

  it('does not fetch while collapsed and remembers the state', async () => {
    const { calls } = mockFetch({ 'GET /api/memory/PAL/words': wordsResponse })
    const w = mount(MemoryBitmap, { props: { domain: pal } })
    await flushPromises()
    await w.find(tid('bitmap-toggle-PAL')).trigger('click')
    await flushPromises()
    expect(w.find(tid('bitmap-canvas-PAL')).exists()).toBe(false)
    const n = calls.length
    await (w.vm as unknown as { refresh(): Promise<void> }).refresh()
    expect(calls).toHaveLength(n)
    await flushPromises()
    expect(JSON.parse(localStorage.getItem('rtcvish.memoryBitmaps')!).collapsed).toEqual({
      PAL: true,
    })
  })
})

describe('MemoryPanel bitmaps', () => {
  it('clicking a pixel moves the hex view to that domain and address', async () => {
    const { calls } = mockFetch({
      'GET /api/memory/PAL/words': wordsResponse,
      'GET /api/memory/RAM/words': wordsResponse,
      'GET /api/memory/RAM': (c) => ({
        domain: 'RAM',
        address: 0,
        data: '00'.repeat(Number(c.query.size)),
      }),
      'GET /api/memory/PAL': (c) => ({
        domain: 'PAL',
        address: Number(c.query.address),
        data: '00'.repeat(Number(c.query.size)),
      }),
    })
    useDomainsStore().domains = [ram, pal]
    const w = mount(MemoryPanel, { attachTo: document.body })
    await flushPromises()
    expect(w.findAll('canvas')).toHaveLength(2)
    const mem = useMemoryStore()
    expect(mem.domain).toBe('RAM')

    await w.find(tid('bitmap-canvas-PAL')).trigger('click', { clientX: 35, clientY: 25 })
    await flushPromises()
    expect(mem.domain).toBe('PAL')
    expect(mem.address).toBe(0x100)
    expect((w.find(tid('mem-address')).element as HTMLInputElement).value).toBe('100')
    expect(calls.at(-1)).toMatchObject({ path: '/api/memory/PAL', query: { address: '256' } })
    // The cursor sits on the clicked byte.
    expect(w.find(tid('hex-cell-6')).classes()).toContain('bg-accent')
    w.unmount()
  })

  it('auto refresh runs at 2 Hz for the hex view and expanded bitmaps, without overlap', async () => {
    vi.useFakeTimers()
    let release: () => void = () => {}
    let slow = false
    const { calls } = mockFetch({
      'GET /api/memory/PAL/words': async (c) => {
        if (slow) await new Promise<void>((r) => (release = r))
        return wordsResponse(c)
      },
      'GET /api/memory/RAM/words': wordsResponse,
      'GET /api/memory/RAM': (c) => ({
        domain: 'RAM',
        address: 0,
        data: '00'.repeat(Number(c.query.size)),
      }),
    })
    useDomainsStore().domains = [ram, { ...pal, hidden: true }]
    const w = mount(MemoryPanel)
    await flushPromises()
    // The hidden domain starts collapsed.
    const count = (p: string) => calls.filter((c) => c.path === p).length
    expect(count('/api/memory/PAL/words')).toBe(0)
    await w.find(tid('bitmap-toggle-PAL')).trigger('click')
    await flushPromises()
    expect(count('/api/memory/PAL/words')).toBe(1)

    await w.find(tid('mem-auto')).setValue(true)
    const before = { hex: count('/api/memory/RAM'), ram: count('/api/memory/RAM/words') }
    await vi.advanceTimersByTimeAsync(500)
    expect(count('/api/memory/RAM')).toBe(before.hex + 1)
    expect(count('/api/memory/RAM/words')).toBe(before.ram + 1)
    expect(count('/api/memory/PAL/words')).toBe(2)

    // A tick is skipped while the previous refresh is running.
    slow = true
    await vi.advanceTimersByTimeAsync(500)
    await vi.advanceTimersByTimeAsync(1000)
    expect(count('/api/memory/PAL/words')).toBe(3)
    expect(count('/api/memory/RAM')).toBe(before.hex + 2)
    slow = false
    release()
    await vi.advanceTimersByTimeAsync(500)
    expect(count('/api/memory/PAL/words')).toBe(4)
    w.unmount()
    vi.useRealTimers()
  })
})

describe('MemoryPanel auto refresh persistence', () => {
  it('remembers the auto checkbox across mounts and resumes refreshing', async () => {
    vi.useFakeTimers()
    const { calls } = mockFetch({
      'GET /api/memory/RAM/words': wordsResponse,
      'GET /api/memory/RAM': (c) => ({
        domain: 'RAM',
        address: 0,
        data: '00'.repeat(Number(c.query.size)),
      }),
    })
    useDomainsStore().domains = [ram]
    const count = () => calls.filter((c) => c.path === '/api/memory/RAM').length

    const first = mount(MemoryPanel)
    await flushPromises()
    await first.find(tid('mem-auto')).setValue(true)
    expect(localStorage.getItem('rtcvish.memory.autoRefresh')).toBe('true')
    first.unmount()

    const second = mount(MemoryPanel)
    await flushPromises()
    expect((second.find(tid('mem-auto')).element as HTMLInputElement).checked).toBe(true)
    const before = count()
    await vi.advanceTimersByTimeAsync(500)
    expect(count()).toBe(before + 1)

    await second.find(tid('mem-auto')).setValue(false)
    expect(localStorage.getItem('rtcvish.memory.autoRefresh')).toBe('false')
    second.unmount()

    const third = mount(MemoryPanel)
    await flushPromises()
    expect((third.find(tid('mem-auto')).element as HTMLInputElement).checked).toBe(false)
    const after = count()
    await vi.advanceTimersByTimeAsync(1000)
    expect(count()).toBe(after)
    third.unmount()
    vi.useRealTimers()
  })
})

describe('MemoryPanel freezes', () => {
  it('shows frozen cells from server units and unfreezes one unit', async () => {
    let server = [
      {
        id: 5,
        domain: 'PAL',
        address: 0x10,
        size: 1,
        value: '00',
        tilt: 0,
        delay: 0,
        lifetime: 0,
        loop: false,
        loopDelay: 0,
        mode: 'hard' as const,
      },
    ]
    const { calls } = mockFetch({
      'GET /api/memory/PAL/words': wordsResponse,
      'GET /api/memory/PAL': (c) => ({
        domain: 'PAL',
        address: Number(c.query.address),
        data: '00'.repeat(Number(c.query.size)),
      }),
      'GET /api/blast/units': () => server,
      'DELETE /api/blast/units/5': () => {
        server = []
        return undefined
      },
    })
    useDomainsStore().domains = [pal]
    const w = mount(MemoryPanel, { attachTo: document.body })
    await flushPromises()

    expect(w.find(tid('hex-cell-16')).attributes('title')).toBe('frozen (hard)')
    expect(w.find(tid('hex-cell-0')).attributes('title')).toBeUndefined()

    await w.find(tid('hex-cell-0')).trigger('click')
    expect(w.find(tid('mem-freeze')).text()).toBe('Freeze')
    expect(w.find(tid('mem-freeze')).attributes('title')).toBe(
      'Freeze the value at the cursor (hard) (f)',
    )

    await w.find(tid('hex-cell-16')).trigger('click')
    const btn = w.find(tid('mem-freeze'))
    expect(btn.text()).toBe('Unfreeze')
    expect(btn.attributes('title')).toContain('hard')
    await btn.trigger('click')
    await flushPromises()
    expect(calls.some((c) => c.method === 'DELETE' && c.path === '/api/blast/units/5')).toBe(true)
    expect(calls.some((c) => c.method === 'DELETE' && c.path === '/api/blast/units')).toBe(false)
    expect(w.find(tid('hex-cell-16')).attributes('title')).toBeUndefined()
    expect(w.find(tid('mem-freeze')).text()).toBe('Freeze')
  })
})

/** Memory panel with the app's shortcut dispatcher, on the Memory tab. */
function mountMemoryApp() {
  useUiStore().panel = 'memory'
  const Host = defineComponent({
    setup() {
      useShortcuts()
      return () => h(MemoryPanel)
    },
  })
  return mount(Host, { attachTo: document.body })
}

function memRoutes(extra: Record<string, (c: Call) => unknown> = {}) {
  return mockFetch({
    'GET /api/memory/PAL/words': wordsResponse,
    'GET /api/memory/PAL': (c) => ({
      domain: 'PAL',
      address: Number(c.query.address),
      data: '00'.repeat(Number(c.query.size)),
    }),
    'PUT /api/memory/PAL': () => undefined,
    'POST /api/blast/apply': () => undefined,
    'GET /api/blast/units': () => [],
    ...extra,
  })
}

async function key(el: Element, k: string) {
  el.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true }))
  await flushPromises()
}

describe('MemoryPanel hex edit mode', () => {
  it('selects on click, edits only after Enter or double-click', async () => {
    const { calls } = memRoutes()
    useDomainsStore().domains = [pal]
    const w = mount(MemoryPanel, { attachTo: document.body })
    await flushPromises()
    const view = w.find(tid('hex-view')).element
    const puts = () => calls.filter((c) => c.method === 'PUT')

    await w.find(tid('hex-cell-0')).trigger('click')
    expect(w.find(tid('hex-cell-0')).attributes('data-editing')).toBeUndefined()
    // Not editing: hex digits are not consumed and write nothing.
    await key(view, 'a')
    await key(view, 'b')
    expect(puts()).toHaveLength(0)
    expect(w.find(tid('hex-cell-0')).text()).toBe('00')

    // Arrows move the cursor without editing.
    await key(view, 'ArrowRight')
    expect(w.find(tid('hex-cell-1')).classes()).toContain('bg-accent')

    // Enter starts editing; the last digit commits and moves on, still editing.
    await key(view, 'Enter')
    expect(w.find(tid('hex-cell-1')).attributes('data-editing')).toBe('true')
    expect(w.find(tid('hex-cell-1')).text()).toBe('__')
    await key(view, 'a')
    expect(w.find(tid('hex-cell-1')).text()).toBe('A_')
    await key(view, 'B')
    expect(puts().map((c) => c.body)).toEqual([{ address: 1, data: 'ab' }])
    expect(w.find(tid('hex-cell-2')).attributes('data-editing')).toBe('true')

    // Escape cancels the typed digits and leaves edit mode.
    await key(view, '7')
    await key(view, 'Escape')
    expect(w.find(tid('hex-cell-2')).attributes('data-editing')).toBeUndefined()
    expect(w.find(tid('hex-cell-2')).text()).toBe('00')
    expect(puts()).toHaveLength(1)

    // Double-click edits; Enter commits a partial value zero-padded.
    await w.find(tid('hex-cell-5')).trigger('dblclick')
    expect(w.find(tid('hex-cell-5')).attributes('data-editing')).toBe('true')
    await key(view, '5')
    await key(view, 'Enter')
    expect(puts().at(-1)!.body).toEqual({ address: 5, data: '05' })
    expect(w.find(tid('hex-cell-6')).classes()).toContain('bg-accent')
    expect(w.find(tid('hex-cell-6')).attributes('data-editing')).toBeUndefined()
    expect(w.text()).toContain(
      'Click to select, Enter or double-click to edit, f freezes, r refreshes',
    )
    w.unmount()
  })
})

describe('MemoryPanel navigation', () => {
  it('pages by 0x1000 clamped to the domain', async () => {
    memRoutes({
      'GET /api/memory/RAM/words': wordsResponse,
      'GET /api/memory/RAM': (c) => ({
        domain: 'RAM',
        address: Number(c.query.address),
        data: '00'.repeat(Number(c.query.size)),
      }),
    })
    useDomainsStore().domains = [ram]
    const w = mount(MemoryPanel)
    await flushPromises()
    const mem = useMemoryStore()
    expect(mem.address).toBe(0)

    await w.find(tid('mem-prev-big')).trigger('click')
    expect(mem.address).toBe(0)
    await w.find(tid('mem-next-big')).trigger('click')
    expect(mem.address).toBe(0x1000)
    await w.find(tid('mem-next')).trigger('click')
    expect(mem.address).toBe(0x1100)
    await w.find(tid('mem-prev-big')).trigger('click')
    expect(mem.address).toBe(0x100)
    await w.find(tid('mem-prev-big')).trigger('click')
    expect(mem.address).toBe(0)

    // The end clamps to the last full page of the 64 KiB domain.
    mem.address = 0xf800
    await w.find(tid('mem-next-big')).trigger('click')
    expect(mem.address).toBe(0xff00)
    await w.find(tid('mem-next-big')).trigger('click')
    expect(mem.address).toBe(0xff00)

    const word = w.find(tid('mem-group')).element.closest('label')!
    expect(word.textContent).toContain('Word')
    expect(word.getAttribute('title')).toBe('Bytes per cell')
    w.unmount()
  })
})

describe('MemoryPanel shortcuts', () => {
  it('f freezes at the cursor and r refreshes, only on the Memory tab', async () => {
    const { calls } = memRoutes()
    useDomainsStore().domains = [pal]
    const w = mountMemoryApp()
    await flushPromises()
    const view = w.find(tid('hex-view')).element as HTMLElement
    const n = (m: string, p: string) => calls.filter((c) => c.method === m && c.path === p).length

    expect(w.find(tid('mem-refresh')).attributes('title')).toBe(
      'Refresh the hex view and bitmaps (r)',
    )

    // No cursor: freeze is disabled.
    await key(view, 'f')
    expect(n('POST', '/api/blast/apply')).toBe(0)

    await w.find(tid('hex-cell-3')).trigger('click')
    view.focus()
    await key(view, 'f')
    expect(n('POST', '/api/blast/apply')).toBe(1)
    const applied = calls.find((c) => c.path === '/api/blast/apply')!.body as {
      layer: { units: { address: number }[] }
    }
    expect(applied.layer.units[0]!.address).toBe(3)

    const reads = n('GET', '/api/memory/PAL')
    const words = n('GET', '/api/memory/PAL/words')
    await key(view, 'r')
    expect(n('GET', '/api/memory/PAL')).toBe(reads + 1)
    expect(n('GET', '/api/memory/PAL/words')).toBe(words + 1)

    // While editing a cell, f is a hex digit and r is swallowed.
    await key(view, 'Enter')
    await key(view, 'f')
    await key(view, 'r')
    expect(n('POST', '/api/blast/apply')).toBe(1)
    expect(n('GET', '/api/memory/PAL')).toBe(reads + 1)
    expect(w.find(tid('hex-cell-3')).text()).toBe('F_')
    await key(view, 'Escape')

    // Another tab: ignored (the panel stays mounted here only because of the test host).
    useUiStore().panel = 'settings'
    await key(view, 'r')
    await key(view, 'f')
    expect(n('GET', '/api/memory/PAL')).toBe(reads + 1)
    expect(n('POST', '/api/blast/apply')).toBe(1)
    w.unmount()
  })
})
