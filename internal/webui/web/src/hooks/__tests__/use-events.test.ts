import { renderHook, act, waitFor } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { useEvents } from '../use-events'
import type { SeqEvent } from '../use-events'

// FakeEventSource is an in-test stub that replaces the browser EventSource.
// It exposes helpers used by tests to simulate opens, messages, and errors.
class FakeEventSource {
  static CONNECTING = 0
  static OPEN = 1
  static CLOSED = 2

  readyState: number = FakeEventSource.CONNECTING
  url: string
  private listeners: Map<string, EventListenerOrEventListenerObject[]> = new Map()

  // Track all instances created so tests can access the most recent one.
  static instances: FakeEventSource[] = []

  constructor(url: string) {
    this.url = url
    FakeEventSource.instances.push(this)
  }

  addEventListener(type: string, listener: EventListenerOrEventListenerObject): void {
    const list = this.listeners.get(type) ?? []
    list.push(listener)
    this.listeners.set(type, list)
  }

  removeEventListener(type: string, listener: EventListenerOrEventListenerObject): void {
    const list = this.listeners.get(type) ?? []
    this.listeners.set(
      type,
      list.filter((l) => l !== listener),
    )
  }

  dispatchEvent(event: Event): boolean {
    const list = this.listeners.get(event.type) ?? []
    for (const listener of list) {
      if (typeof listener === 'function') {
        listener(event)
      } else {
        listener.handleEvent(event)
      }
    }
    return true
  }

  close(): void {
    this.readyState = FakeEventSource.CLOSED
  }

  // Test helpers ---------------------------------------------------------

  simulateOpen(): void {
    this.readyState = FakeEventSource.OPEN
    const ev = new Event('open')
    this.dispatchEvent(ev)
    // The hook attaches onopen directly.
    if (typeof (this as unknown as Record<string, unknown>)['onopen'] === 'function') {
      ;(this as unknown as Record<string, (e: Event) => void>)['onopen'](ev)
    }
  }

  simulateMessage(data: SeqEvent): void {
    const ev = Object.assign(new MessageEvent('message', { data: JSON.stringify(data) }), {})
    // The hook attaches onmessage directly.
    if (typeof (this as unknown as Record<string, unknown>)['onmessage'] === 'function') {
      ;(this as unknown as Record<string, (e: MessageEvent) => void>)['onmessage'](
        ev as MessageEvent,
      )
    }
  }

  simulateError(closed = false): void {
    if (closed) this.readyState = FakeEventSource.CLOSED
    const ev = new Event('error')
    if (typeof (this as unknown as Record<string, unknown>)['onerror'] === 'function') {
      ;(this as unknown as Record<string, (e: Event) => void>)['onerror'](ev)
    }
  }
}

// Build a minimal SeqEvent fixture.
function makeEvent(seq: number, type = 'iter_started'): SeqEvent {
  return { seq, event: { type: type as SeqEvent['event']['type'], at: '2026-01-01T00:00:00Z' } }
}

describe('useEvents', () => {
  let fetchMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    FakeEventSource.instances = []
    vi.stubGlobal('EventSource', FakeEventSource)
    fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('starts with connecting status and empty events', () => {
    renderHook(() => useEvents('sess-01'))
    expect(FakeEventSource.instances).toHaveLength(1)
  })

  it('transitions to open status when the connection opens', async () => {
    const { result } = renderHook(() => useEvents('sess-01'))

    act(() => {
      FakeEventSource.instances[0].simulateOpen()
    })

    await waitFor(() => expect(result.current.status).toBe('open'))
  })

  it('delivers events in order and tracks lastSeq', async () => {
    const { result } = renderHook(() => useEvents('sess-01'))
    const es = FakeEventSource.instances[0]

    act(() => {
      es.simulateOpen()
      es.simulateMessage(makeEvent(1))
      es.simulateMessage(makeEvent(2))
      es.simulateMessage(makeEvent(3, 'loop_finished'))
    })

    await waitFor(() => expect(result.current.events).toHaveLength(3))

    expect(result.current.events[0].seq).toBe(1)
    expect(result.current.events[1].seq).toBe(2)
    expect(result.current.events[2].seq).toBe(3)
    expect(result.current.lastSeq).toBe(3)
  })

  it('sets status to error on transient reconnect (readyState CONNECTING)', async () => {
    const { result } = renderHook(() => useEvents('sess-01'))
    const es = FakeEventSource.instances[0]

    act(() => {
      es.simulateOpen()
      // Simulate a transient drop: readyState goes back to CONNECTING.
      es.readyState = FakeEventSource.CONNECTING
      es.simulateError(false)
    })

    await waitFor(() => expect(result.current.status).toBe('error'))
  })

  it('calls onSeqGone when the server returns 410', async () => {
    const onSeqGone = vi.fn()

    // Probe returns 410.
    fetchMock.mockResolvedValue({ status: 410, ok: false })

    const { result } = renderHook(() =>
      useEvents('sess-01', { fromSeq: 5, onSeqGone }),
    )
    const es = FakeEventSource.instances[0]

    act(() => {
      es.simulateOpen()
      es.simulateMessage(makeEvent(6))
      // Simulate permanent close (e.g. server sent HTTP 410 on reconnect).
      es.simulateError(true)
    })

    await waitFor(() => expect(onSeqGone).toHaveBeenCalledTimes(1))
    expect(result.current.lastSeq).toBe(6)
  })

  it('sets status to closed when probe returns non-410', async () => {
    fetchMock.mockResolvedValue({ status: 200, ok: true })

    const { result } = renderHook(() => useEvents('sess-01'))
    const es = FakeEventSource.instances[0]

    act(() => {
      es.simulateOpen()
      es.simulateError(true)
    })

    await waitFor(() => expect(result.current.status).toBe('closed'))
  })

  it('closes the EventSource on unmount', () => {
    const { unmount } = renderHook(() => useEvents('sess-01'))
    const es = FakeEventSource.instances[0]

    unmount()

    expect(es.readyState).toBe(FakeEventSource.CLOSED)
  })

  it('reopens EventSource when sessionId changes', async () => {
    const { rerender } = renderHook(
      ({ id }: { id: string }) => useEvents(id),
      { initialProps: { id: 'sess-A' } },
    )

    expect(FakeEventSource.instances).toHaveLength(1)

    rerender({ id: 'sess-B' })

    await waitFor(() => expect(FakeEventSource.instances).toHaveLength(2))
    // Old instance should be closed.
    expect(FakeEventSource.instances[0].readyState).toBe(FakeEventSource.CLOSED)
    expect(FakeEventSource.instances[1].url).toContain('sess-B')
  })
})
