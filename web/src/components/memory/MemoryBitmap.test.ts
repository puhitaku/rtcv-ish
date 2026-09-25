import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import MemoryBitmap from './MemoryBitmap.vue'
import MemoryPanel from '@/components/panels/MemoryPanel.vue'
import { mockFetch, type Call } from '@/test/fetch'
import { statusFixture } from '@/test/fixtures'
import { useDomainsStore } from '@/stores/domains'
import { useMemoryStore } from '@/stores/memory'
import { useStatusStore } from '@/stores/status'
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
