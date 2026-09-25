import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { defineComponent } from 'vue'
import { ApiError, errorMessage } from '@/api/client'
import type { Status } from '@/api/types'
import { errorResponse, mockFetch } from '@/test/fetch'
import { statusFixture } from '@/test/fixtures'
import { act, useLogStore } from './log'
import { BUSY_POLL_MS, BUSY_TICK_MS, useBusyPoll, useStatusStore } from './status'

const BusyPoll = defineComponent({ setup: () => useBusyPoll(), template: '<div />' })

beforeEach(() => setActivePinia(createPinia()))
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('status store: busy / unresponsive', () => {
  it('treats missing unresponsive as false and missing busy as null', () => {
    const st = useStatusStore()
    const legacy = statusFixture() as Partial<Status>
    delete legacy.unresponsive
    delete legacy.busy
    st.set(legacy as Status)
    expect(st.status?.unresponsive).toBe(false)
    expect(st.status?.busy).toBeNull()
    expect(st.unresponsive).toBe(false)
    expect(st.busy).toBeNull()
    expect(st.needIdle).toBe('')
    expect(st.needRom).toBe('')
  })

  it('tracks the busy operation and its elapsed time', () => {
    vi.useFakeTimers()
    const st = useStatusStore()
    st.set(statusFixture({ busy: { operation: 'loadRom', sinceMs: 2500 } }))
    expect(st.busy?.operation).toBe('loadRom')
    expect(st.busyMs).toBe(2500)
    expect(st.stuck).toBe(false)
    expect(st.needEmu).toBe('Emulator is busy: loadRom')
    expect(st.needRom).toBe('Emulator is busy: loadRom')

    vi.advanceTimersByTime(3000)
    st.tick()
    expect(st.busyMs).toBe(5500)
    expect(st.stuck).toBe(true)

    st.set(statusFixture())
    expect(st.busy).toBeNull()
    expect(st.busyMs).toBe(0)
    expect(st.stuck).toBe(false)
    expect(st.needRom).toBe('')
  })

  it('blocks operations while unresponsive', () => {
    const st = useStatusStore()
    st.set(statusFixture({ unresponsive: true }))
    expect(st.unresponsive).toBe(true)
    expect(st.stuck).toBe(true)
    expect(st.needEmu).toBe('Emulator is unresponsive; disconnect or quit it')
    expect(st.needRom).toBe('Emulator is unresponsive; disconnect or quit it')
  })

  it('ignores unresponsive while disconnected', () => {
    const st = useStatusStore()
    st.set(statusFixture({ connected: false, unresponsive: true }))
    expect(st.unresponsive).toBe(false)
    expect(st.needEmu).toBe('Emulator not connected')
  })

  it('ticks while a call is pending and polls only as a fallback while busy', async () => {
    vi.useFakeTimers()
    let busy: Status['busy'] = { operation: 'loadRom', sinceMs: 100 }
    let resolveRom!: (r: Response) => void
    const { calls } = mockFetch({
      'GET /api/status': () => statusFixture({ busy }),
      'POST /api/emulator/rom': () =>
        new Promise<Response>((r) => {
          resolveRom = r
        }),
    })
    const st = useStatusStore()
    st.set(statusFixture())
    const w = mount(BusyPoll)
    const polls = () => calls.filter((c) => c.path === '/api/status').length

    await vi.advanceTimersByTimeAsync(BUSY_POLL_MS * 2)
    expect(polls()).toBe(0)

    // A pending call alone only ticks the clock; busy arrives over SSE.
    const p = st.loadRom('/r/game.nds')
    await vi.advanceTimersByTimeAsync(BUSY_TICK_MS)
    expect(st.now).toBe(Date.now())
    expect(polls()).toBe(0)

    // The `status` event reports busy: the elapsed time ticks locally.
    st.set(statusFixture({ busy }))
    expect(st.busyMs).toBe(100)
    await vi.advanceTimersByTimeAsync(BUSY_TICK_MS * 2)
    expect(st.busyMs).toBe(100 + BUSY_TICK_MS * 2)
    expect(polls()).toBe(0)

    resolveRom(new Response(JSON.stringify(statusFixture().game), { status: 200 }))
    await p
    // Still busy (e.g. a missed idle event): fall back to a slow poll.
    await vi.advanceTimersByTimeAsync(BUSY_POLL_MS - BUSY_TICK_MS * 2)
    expect(polls()).toBe(1)
    expect(st.busy?.operation).toBe('loadRom')
    await vi.advanceTimersByTimeAsync(BUSY_POLL_MS - BUSY_TICK_MS)
    expect(polls()).toBe(1)

    busy = undefined
    await vi.advanceTimersByTimeAsync(BUSY_TICK_MS)
    expect(polls()).toBe(2)
    expect(st.busy).toBeNull()
    expect(st.busyMs).toBe(0)
    await vi.advanceTimersByTimeAsync(BUSY_POLL_MS * 3)
    expect(polls()).toBe(2)
    w.unmount()
  })

  it('stops polling once a status event reports idle', async () => {
    vi.useFakeTimers()
    const { calls } = mockFetch({
      'GET /api/status': () => statusFixture({ busy: { operation: 'loadRom', sinceMs: 0 } }),
    })
    const st = useStatusStore()
    st.set(statusFixture({ busy: { operation: 'loadRom', sinceMs: 0 } }))
    const w = mount(BusyPoll)
    await vi.advanceTimersByTimeAsync(BUSY_POLL_MS - BUSY_TICK_MS)
    st.set(statusFixture())
    await vi.advanceTimersByTimeAsync(BUSY_POLL_MS * 3)
    expect(calls.filter((c) => c.path === '/api/status').length).toBe(0)
    w.unmount()
  })
})

describe('stuck emulator errors', () => {
  it('maps BUSY, EMULATOR_TIMEOUT and EMULATOR_UNRESPONSIVE to clear messages', () => {
    expect(errorMessage(new ApiError('another operation is running: loadRom', 'BUSY', 409))).toBe(
      'Emulator is busy: loadRom',
    )
    expect(errorMessage(new ApiError('busy', 'BUSY', 409))).toBe('Emulator is busy: busy')
    expect(errorMessage(new ApiError('read timed out', 'EMULATOR_TIMEOUT', 504))).toBe(
      'Emulator did not answer in time',
    )
    expect(errorMessage(new ApiError('not responding', 'EMULATOR_UNRESPONSIVE', 503))).toBe(
      'Emulator is unresponsive; disconnect or quit it',
    )
  })

  it('logs the message and refreshes status', async () => {
    const { calls } = mockFetch({
      'POST /api/blast': () =>
        errorResponse(409, 'BUSY', 'another operation is running: protectionBackup'),
      'GET /api/status': () =>
        statusFixture({ busy: { operation: 'protectionBackup', sinceMs: 5000 } }),
    })
    const { useUnitsStore } = await import('./units')
    await act(() => useUnitsStore().blast())
    await flushPromises()
    const log = useLogStore()
    expect(log.entries.at(-1)?.msg).toBe('Emulator is busy: protectionBackup')
    expect(log.toasts.at(-1)?.msg).toBe('Emulator is busy: protectionBackup')
    expect(calls.map((c) => `${c.method} ${c.path}`)).toContain('GET /api/status')
    expect(useStatusStore().busy?.operation).toBe('protectionBackup')
  })
})
